package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
}

type ToolResult struct {
	Content string `json:"content"`
}

type Client struct {
	reader  *bufio.Scanner
	writer  io.Writer
	closer  io.Closer
	pending map[string]chan response
	mu      sync.Mutex
	nextID  int64
	done    chan struct{}
}

type request struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int64       `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func NewClient(reader io.Reader, writer io.Writer, closer io.Closer) *Client {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	client := &Client{
		reader:  scanner,
		writer:  writer,
		closer:  closer,
		pending: make(map[string]chan response),
		done:    make(chan struct{}),
	}
	go client.readLoop()
	return client
}

func NewProcessClient(ctx context.Context, command string, args ...string) (*Client, error) {
	if strings.TrimSpace(command) == "" {
		return nil, errors.New("command is required")
	}
	cmd := exec.CommandContext(ctx, command, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open stdout: %w", err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start mcp process: %w", err)
	}
	return NewClient(stdout, stdin, stdin), nil
}

func NewProcessClientWithEnv(ctx context.Context, command string, args []string, env []string) (*Client, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Env = env
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start mcp process: %w", err)
	}
	return NewClient(stdout, stdin, stdin), nil
}

func (c *Client) Close() error {
	close(c.done)
	if c.closer != nil {
		return c.closer.Close()
	}
	return nil
}

func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	resp, err := c.sendRequest(ctx, "tools/list", map[string]interface{}{})
	if err != nil {
		return nil, err
	}
	var payload struct {
		Tools []Tool `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &payload); err != nil {
		return nil, fmt.Errorf("parse tools list: %w", err)
	}
	return payload.Tools, nil
}

func (c *Client) CallTool(ctx context.Context, call ToolCall) (ToolResult, error) {
	params := map[string]interface{}{
		"name":      call.Name,
		"arguments": json.RawMessage(call.Arguments),
	}
	resp, err := c.sendRequest(ctx, "tools/call", params)
	if err != nil {
		return ToolResult{}, err
	}
	var payload ToolResult
	if err := json.Unmarshal(resp.Result, &payload); err != nil {
		return ToolResult{}, fmt.Errorf("parse tool result: %w", err)
	}
	if strings.TrimSpace(payload.Content) == "" {
		return ToolResult{}, errors.New("tool result content is empty")
	}
	return payload, nil
}

type ToolCall struct {
	ID        string
	Name      string
	Arguments json.RawMessage
}

func (c *Client) sendRequest(ctx context.Context, method string, params interface{}) (response, error) {
	requestID := atomic.AddInt64(&c.nextID, 1)
	idKey := strconv.FormatInt(requestID, 10)
	respCh := make(chan response, 1)
	defer close(respCh)

	c.mu.Lock()
	c.pending[idKey] = respCh
	c.mu.Unlock()

	req := request{JSONRPC: "2.0", ID: requestID, Method: method, Params: params}
	data, err := json.Marshal(req)
	if err != nil {
		return response{}, fmt.Errorf("marshal request: %w", err)
	}
	data = append(data, '\n')
	if _, err := c.writer.Write(data); err != nil {
		return response{}, fmt.Errorf("write request: %w", err)
	}

	select {
	case <-ctx.Done():
		return response{}, ctx.Err()
	case resp := <-respCh:
		if resp.Error != nil {
			return response{}, fmt.Errorf("mcp error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp, nil
	}
}

func (c *Client) readLoop() {
	for c.reader.Scan() {
		select {
		case <-c.done:
			return
		default:
		}
		line := strings.TrimSpace(c.reader.Text())
		if line == "" {
			continue
		}
		var resp response
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			fmt.Fprintf(os.Stderr, "mcp: failed to parse response: %v\n", err)
			continue
		}
		idKey := strings.TrimSpace(string(resp.ID))
		if idKey == "" {
			continue
		}
		c.mu.Lock()
		ch := c.pending[idKey]
		delete(c.pending, idKey)
		c.mu.Unlock()
		if ch == nil {
			continue
		}
		ch <- resp
	}
}
