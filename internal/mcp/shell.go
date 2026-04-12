package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const ShellToolName = "shell"

const shellToolDescription = "Execute a shell command."

var shellToolSchema = []byte(`{"type":"object","properties":{"command":{"type":"string"},"cwd":{"type":"string"},"timeout":{"type":"integer","minimum":0},"idle_timeout":{"type":"integer","minimum":0}},"required":["command"]}`)

type ShellToolConfig struct {
	Timeout        int
	IdleTimeout    int
	MaxTimeout     int
	MaxIdleTimeout int
	MaxOutput      int
}

type ShellToolProvider struct {
	config    ShellToolConfig
	shellPath string
}

func NewShellToolProvider(config ShellToolConfig) *ShellToolProvider {
	return &ShellToolProvider{config: config, shellPath: resolveShellPath()}
}

func (s *ShellToolProvider) ListTools(ctx context.Context) ([]Tool, error) {
	return []Tool{{
		Name:        ShellToolName,
		Description: shellToolDescription,
		InputSchema: shellToolSchema,
	}}, nil
}

func (s *ShellToolProvider) CallTool(ctx context.Context, call ToolCall) (ToolResult, error) {
	if call.Name != ShellToolName {
		return ToolResult{}, fmt.Errorf("unknown tool name %q", call.Name)
	}
	input, err := parseShellArguments(call.Arguments, s.config)
	if err != nil {
		return ToolResult{}, err
	}
	result, err := executeShellCommand(ctx, s.shellPath, input)
	if err != nil {
		return ToolResult{}, err
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return ToolResult{}, fmt.Errorf("marshal shell output: %w", err)
	}
	return ToolResult{Content: []ContentItem{{Type: ContentTypeText, Text: string(payload)}}}, nil
}

func (s *ShellToolProvider) Close() error {
	return nil
}

type shellToolArguments struct {
	Command     string  `json:"command"`
	Cwd         *string `json:"cwd"`
	Timeout     *int    `json:"timeout"`
	IdleTimeout *int    `json:"idle_timeout"`
}

type shellExecutionConfig struct {
	Command     string
	Cwd         string
	Timeout     time.Duration
	IdleTimeout time.Duration
	MaxOutput   int
}

type shellExecutionResult struct {
	ExitCode        int    `json:"exit_code"`
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	OutputTruncated bool   `json:"output_truncated,omitempty"`
	OutputBytes     int    `json:"output_bytes,omitempty"`
	OutputFile      string `json:"output_file,omitempty"`
}

func parseShellArguments(raw json.RawMessage, cfg ShellToolConfig) (shellExecutionConfig, error) {
	if len(raw) == 0 {
		return shellExecutionConfig{}, errors.New("shell tool arguments are required")
	}
	var args shellToolArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return shellExecutionConfig{}, fmt.Errorf("parse shell tool arguments: %w", err)
	}
	command := strings.TrimSpace(args.Command)
	if command == "" {
		return shellExecutionConfig{}, errors.New("shell command is required")
	}
	cwd := ""
	if args.Cwd != nil {
		cwd = strings.TrimSpace(*args.Cwd)
	}
	if cfg.MaxOutput < 0 {
		return shellExecutionConfig{}, errors.New("shell max_output must be >= 0")
	}
	timeoutSeconds, err := resolveTimeoutSeconds(args.Timeout, cfg.Timeout, cfg.MaxTimeout, "timeout")
	if err != nil {
		return shellExecutionConfig{}, err
	}
	idleSeconds, err := resolveTimeoutSeconds(args.IdleTimeout, cfg.IdleTimeout, cfg.MaxIdleTimeout, "idle_timeout")
	if err != nil {
		return shellExecutionConfig{}, err
	}
	return shellExecutionConfig{
		Command:     command,
		Cwd:         cwd,
		Timeout:     time.Duration(timeoutSeconds) * time.Second,
		IdleTimeout: time.Duration(idleSeconds) * time.Second,
		MaxOutput:   cfg.MaxOutput,
	}, nil
}

func resolveTimeoutSeconds(value *int, defaultValue int, maxValue int, field string) (int, error) {
	resolved := defaultValue
	if value != nil {
		resolved = *value
	}
	if resolved < 0 {
		return 0, fmt.Errorf("shell %s must be >= 0", field)
	}
	if maxValue < 0 {
		return 0, fmt.Errorf("shell max_%s must be >= 0", field)
	}
	if maxValue > 0 {
		if resolved == 0 || resolved > maxValue {
			resolved = maxValue
		}
	}
	return resolved, nil
}

func executeShellCommand(ctx context.Context, shellPath string, input shellExecutionConfig) (shellExecutionResult, error) {
	var cancel context.CancelFunc
	execCtx := ctx
	if input.Timeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, input.Timeout)
	} else {
		execCtx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	cmd := exec.CommandContext(execCtx, shellPath, "-c", input.Command)
	if input.Cwd != "" {
		cmd.Dir = input.Cwd
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return shellExecutionResult{}, fmt.Errorf("open stdout: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return shellExecutionResult{}, fmt.Errorf("open stderr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return shellExecutionResult{}, fmt.Errorf("start shell command: %w", err)
	}

	collector := newOutputCollector(input.MaxOutput)
	activityCh := make(chan struct{}, 1)
	idleTriggered := atomic.Bool{}
	stopIdle := startIdleTimer(execCtx, input.IdleTimeout, activityCh, func() {
		idleTriggered.Store(true)
		cancel()
	})
	defer stopIdle()

	activity := func() {
		if input.IdleTimeout <= 0 {
			return
		}
		select {
		case activityCh <- struct{}{}:
		default:
		}
	}

	errCh := make(chan error, 2)
	go func() {
		_, copyErr := io.Copy(&streamWriter{collector: collector, stdout: true, activity: activity}, stdoutPipe)
		errCh <- copyErr
	}()
	go func() {
		_, copyErr := io.Copy(&streamWriter{collector: collector, stdout: false, activity: activity}, stderrPipe)
		errCh <- copyErr
	}()

	waitErr := cmd.Wait()
	stdoutErr := <-errCh
	stderrErr := <-errCh
	closeErr := collector.Close()
	stdoutErr = normalizePipeError(stdoutErr)
	stderrErr = normalizePipeError(stderrErr)
	if stdoutErr != nil {
		return shellExecutionResult{}, fmt.Errorf("read stdout: %w", stdoutErr)
	}
	if stderrErr != nil {
		return shellExecutionResult{}, fmt.Errorf("read stderr: %w", stderrErr)
	}
	if closeErr != nil {
		return shellExecutionResult{}, fmt.Errorf("close output file: %w", closeErr)
	}
	if execCtx.Err() != nil {
		if idleTriggered.Load() {
			return shellExecutionResult{}, errors.New("shell command idle timeout exceeded")
		}
		if errors.Is(execCtx.Err(), context.DeadlineExceeded) {
			return shellExecutionResult{}, errors.New("shell command timeout exceeded")
		}
		return shellExecutionResult{}, execCtx.Err()
	}
	if waitErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(waitErr, &exitErr) {
			return shellExecutionResult{}, fmt.Errorf("run shell command: %w", waitErr)
		}
	}
	exitCode := cmd.ProcessState.ExitCode()

	result := shellExecutionResult{
		ExitCode: exitCode,
		Stdout:   collector.Stdout(),
		Stderr:   collector.Stderr(),
	}
	if collector.Truncated() {
		result.OutputTruncated = true
		result.OutputBytes = collector.TotalBytes()
		result.OutputFile = collector.OutputFile()
	}
	return result, nil
}

func resolveShellPath() string {
	value := strings.TrimSpace(os.Getenv("SHELL"))
	if value != "" {
		return value
	}
	return "/bin/sh"
}

func startIdleTimer(ctx context.Context, idleTimeout time.Duration, activity <-chan struct{}, onTimeout func()) func() {
	if idleTimeout <= 0 {
		return func() {}
	}
	timer := time.NewTimer(idleTimeout)
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		defer timer.Stop()
		for {
			select {
			case <-activity:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(idleTimeout)
			case <-timer.C:
				onTimeout()
				return
			case <-ctx.Done():
				return
			case <-done:
				return
			}
		}
	}()
	return func() {
		close(done)
		<-stopped
	}
}

func normalizePipeError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, os.ErrClosed) {
		return nil
	}
	return err
}

type streamWriter struct {
	collector *outputCollector
	stdout    bool
	activity  func()
}

func (w *streamWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	w.activity()
	if w.stdout {
		return w.collector.WriteStdout(p)
	}
	return w.collector.WriteStderr(p)
}

type outputCollector struct {
	maxOutput  int
	stdout     bytes.Buffer
	stderr     bytes.Buffer
	combined   bytes.Buffer
	output     *os.File
	outputPath string
	totalBytes int
	truncated  bool
	mu         sync.Mutex
}

func newOutputCollector(maxOutput int) *outputCollector {
	return &outputCollector{maxOutput: maxOutput}
}

func (o *outputCollector) WriteStdout(p []byte) (int, error) {
	return o.write(p, &o.stdout)
}

func (o *outputCollector) WriteStderr(p []byte) (int, error) {
	return o.write(p, &o.stderr)
}

func (o *outputCollector) write(p []byte, buffer *bytes.Buffer) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.truncated {
		o.totalBytes += len(p)
		if _, err := o.output.Write(p); err != nil {
			return 0, err
		}
		return len(p), nil
	}

	if o.maxOutput <= 0 {
		o.totalBytes += len(p)
		_, _ = buffer.Write(p)
		return len(p), nil
	}

	newTotal := o.totalBytes + len(p)
	if newTotal > o.maxOutput {
		allowed := o.maxOutput - o.totalBytes
		file, err := os.CreateTemp("", "agn-shell-output-")
		if err != nil {
			return 0, err
		}
		o.output = file
		o.outputPath = file.Name()
		if o.combined.Len() > 0 {
			if _, err := o.output.Write(o.combined.Bytes()); err != nil {
				return 0, err
			}
		}
		if _, err := o.output.Write(p); err != nil {
			return 0, err
		}
		if allowed > 0 {
			_, _ = buffer.Write(p[:allowed])
		}
		o.totalBytes = newTotal
		o.truncated = true
		o.combined.Reset()
		return len(p), nil
	}

	o.totalBytes = newTotal
	_, _ = buffer.Write(p)
	_, _ = o.combined.Write(p)
	return len(p), nil
}

func (o *outputCollector) Stdout() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.stdout.String()
}

func (o *outputCollector) Stderr() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.stderr.String()
}

func (o *outputCollector) OutputFile() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.outputPath
}

func (o *outputCollector) TotalBytes() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.totalBytes
}

func (o *outputCollector) Truncated() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.truncated
}

func (o *outputCollector) Close() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.output != nil {
		err := o.output.Close()
		o.output = nil
		return err
	}
	return nil
}
