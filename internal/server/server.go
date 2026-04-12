package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/agynio/agn-cli/internal/loop"
	"github.com/agynio/agn-cli/internal/message"
	"github.com/agynio/agn-cli/internal/state"
	"github.com/agynio/agn-cli/internal/telemetry"
)

const (
	jsonRPCVersion         = "2.0"
	defaultThreadListLimit = 50
	maxThreadListLimit     = 200
)

type Server struct {
	agent    *loop.Agent
	store    state.Store
	flush    func(context.Context) error
	writeMu  sync.Mutex
	inFlight map[string]context.CancelFunc
	mu       sync.Mutex
	shutdown context.CancelFunc
	once     sync.Once
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type TurnParams struct {
	Prompt         string `json:"prompt"`
	ThreadID       string `json:"thread_id,omitempty"`
	RestrictOutput bool   `json:"restrict_output,omitempty"`
	Stream         bool   `json:"stream,omitempty"`
}

type TurnResult struct {
	ThreadID string `json:"thread_id"`
	Response string `json:"response"`
}

type CancelParams struct {
	RequestID string `json:"request_id"`
}

type AgentMessageDeltaNotification struct {
	RequestID string `json:"request_id"`
	ThreadID  string `json:"thread_id"`
	TurnID    string `json:"turn_id"`
	ItemID    string `json:"item_id"`
	Delta     string `json:"delta"`
}

type ItemLifecycleNotification struct {
	RequestID string `json:"request_id"`
	ThreadID  string `json:"thread_id"`
	TurnID    string `json:"turn_id"`
	ItemID    string `json:"item_id"`
	ToolName  string `json:"tool_name"`
}

type TurnLifecycleNotification struct {
	RequestID string `json:"request_id"`
	ThreadID  string `json:"thread_id"`
	TurnID    string `json:"turn_id"`
}

type ThreadListParams struct {
	Limit  int    `json:"limit,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}

type ThreadListResult struct {
	Data       []state.ThreadSummary `json:"data"`
	NextCursor *string               `json:"next_cursor"`
}

type ThreadReadParams struct {
	ThreadID string `json:"thread_id"`
}

type ThreadReadResult struct {
	ThreadID  string              `json:"thread_id"`
	UpdatedAt time.Time           `json:"updated_at"`
	Messages  []ThreadReadMessage `json:"messages"`
}

type ThreadReadMessage struct {
	ID         string          `json:"id"`
	CreatedAt  time.Time       `json:"created_at"`
	Role       string          `json:"role"`
	Kind       string          `json:"kind"`
	Text       string          `json:"text,omitempty"`
	ToolCalls  json.RawMessage `json:"tool_calls,omitempty"`
	ToolOutput json.RawMessage `json:"tool_output,omitempty"`
}

type ThreadResumeParams struct {
	ThreadID       string `json:"thread_id"`
	Prompt         string `json:"prompt"`
	RestrictOutput bool   `json:"restrict_output,omitempty"`
	Stream         bool   `json:"stream,omitempty"`
}

func New(agent *loop.Agent, store state.Store, flush func(context.Context) error) *Server {
	return &Server{
		agent:    agent,
		store:    store,
		flush:    flush,
		inFlight: make(map[string]context.CancelFunc),
	}
}

func (s *Server) Serve(ctx context.Context, reader io.Reader, writer io.Writer) error {
	ctx, cancel := context.WithCancel(ctx)
	s.shutdown = cancel
	defer cancel()
	if closer, ok := reader.(io.Closer); ok {
		go func() {
			<-ctx.Done()
			_ = closer.Close()
		}()
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var req request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			s.writeResponse(writer, response{JSONRPC: jsonRPCVersion, ID: json.RawMessage("null"), Error: &rpcError{Code: -32700, Message: "parse error"}})
			continue
		}
		if strings.TrimSpace(req.Method) == "" {
			s.writeResponse(writer, response{JSONRPC: jsonRPCVersion, ID: invalidRequestID(req.ID), Error: &rpcError{Code: -32600, Message: "invalid request"}})
			continue
		}
		if len(req.ID) == 0 {
			s.writeResponse(writer, response{JSONRPC: jsonRPCVersion, ID: json.RawMessage("null"), Error: &rpcError{Code: -32600, Message: "invalid request"}})
			continue
		}
		id := req.ID
		switch req.Method {
		case "turn/start":
			go s.handleTurn(ctx, req, writer)
		case "turn/interrupt":
			go s.handleCancel(ctx, req, writer)
		case "thread/list":
			go s.handleThreadList(ctx, req, writer)
		case "thread/read":
			go s.handleThreadRead(ctx, req, writer)
		case "thread/resume":
			go s.handleThreadResume(ctx, req, writer)
		default:
			s.writeResponse(writer, response{JSONRPC: jsonRPCVersion, ID: id, Error: &rpcError{Code: -32601, Message: "method not found"}})
		}
	}
	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	return ctx.Err()
}

func (s *Server) handleTurn(ctx context.Context, req request, writer io.Writer) {
	var params TurnParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.writeResponse(writer, response{JSONRPC: jsonRPCVersion, ID: req.ID, Error: &rpcError{Code: -32602, Message: "invalid params"}})
		return
	}
	prompt := strings.TrimSpace(params.Prompt)
	if prompt == "" {
		s.writeResponse(writer, response{JSONRPC: jsonRPCVersion, ID: req.ID, Error: &rpcError{Code: -32602, Message: "prompt is required"}})
		return
	}
	threadID := strings.TrimSpace(params.ThreadID)
	s.executeTurn(ctx, req, writer, threadID, prompt, params.RestrictOutput, params.Stream)
}

func (s *Server) handleCancel(ctx context.Context, req request, writer io.Writer) {
	var params CancelParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.writeResponse(writer, response{JSONRPC: jsonRPCVersion, ID: req.ID, Error: &rpcError{Code: -32602, Message: "invalid params"}})
		return
	}
	if strings.TrimSpace(params.RequestID) == "" {
		s.writeResponse(writer, response{JSONRPC: jsonRPCVersion, ID: req.ID, Error: &rpcError{Code: -32602, Message: "request_id is required"}})
		return
	}

	if !s.cancel(params.RequestID) {
		s.writeResponse(writer, response{JSONRPC: jsonRPCVersion, ID: req.ID, Error: &rpcError{Code: -32602, Message: "request not found"}})
		return
	}

	s.writeResponse(writer, response{JSONRPC: jsonRPCVersion, ID: req.ID, Result: map[string]bool{"cancelled": true}})
}

func (s *Server) handleThreadList(ctx context.Context, req request, writer io.Writer) {
	if s.store == nil {
		s.writeResponse(writer, response{JSONRPC: jsonRPCVersion, ID: req.ID, Error: &rpcError{Code: -32603, Message: "state store is not configured"}})
		return
	}
	var params ThreadListParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.writeResponse(writer, response{JSONRPC: jsonRPCVersion, ID: req.ID, Error: &rpcError{Code: -32602, Message: "invalid params"}})
		return
	}
	limit := params.Limit
	if limit <= 0 {
		limit = defaultThreadListLimit
	}
	if limit > maxThreadListLimit {
		limit = maxThreadListLimit
	}
	threads, err := s.store.List(ctx)
	if err != nil {
		s.writeResponse(writer, response{JSONRPC: jsonRPCVersion, ID: req.ID, Error: &rpcError{Code: -32603, Message: err.Error()}})
		return
	}
	start := 0
	cursor := strings.TrimSpace(params.Cursor)
	if cursor != "" {
		found := false
		for i, summary := range threads {
			if summary.ID == cursor {
				start = i + 1
				found = true
				break
			}
		}
		if !found {
			s.writeResponse(writer, response{JSONRPC: jsonRPCVersion, ID: req.ID, Error: &rpcError{Code: -32602, Message: "cursor not found"}})
			return
		}
	}
	if start > len(threads) {
		start = len(threads)
	}
	end := start + limit
	if end > len(threads) {
		end = len(threads)
	}
	data := threads[start:end]
	var nextCursor *string
	if end < len(threads) && len(data) > 0 {
		next := data[len(data)-1].ID
		nextCursor = &next
	}
	s.writeResponse(writer, response{JSONRPC: jsonRPCVersion, ID: req.ID, Result: ThreadListResult{Data: data, NextCursor: nextCursor}})
}

func (s *Server) handleThreadRead(ctx context.Context, req request, writer io.Writer) {
	if s.store == nil {
		s.writeResponse(writer, response{JSONRPC: jsonRPCVersion, ID: req.ID, Error: &rpcError{Code: -32603, Message: "state store is not configured"}})
		return
	}
	var params ThreadReadParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.writeResponse(writer, response{JSONRPC: jsonRPCVersion, ID: req.ID, Error: &rpcError{Code: -32602, Message: "invalid params"}})
		return
	}
	threadID := strings.TrimSpace(params.ThreadID)
	if threadID == "" {
		s.writeResponse(writer, response{JSONRPC: jsonRPCVersion, ID: req.ID, Error: &rpcError{Code: -32602, Message: "thread_id is required"}})
		return
	}
	thread, err := s.store.Load(ctx, threadID)
	if err != nil {
		s.writeResponse(writer, response{JSONRPC: jsonRPCVersion, ID: req.ID, Error: &rpcError{Code: -32603, Message: err.Error()}})
		return
	}
	messages, err := threadMessagesToWire(thread.Messages)
	if err != nil {
		s.writeResponse(writer, response{JSONRPC: jsonRPCVersion, ID: req.ID, Error: &rpcError{Code: -32603, Message: err.Error()}})
		return
	}
	s.writeResponse(writer, response{JSONRPC: jsonRPCVersion, ID: req.ID, Result: ThreadReadResult{ThreadID: thread.ID, UpdatedAt: thread.UpdatedAt, Messages: messages}})
}

func threadMessagesToWire(records []state.MessageRecord) ([]ThreadReadMessage, error) {
	messages := make([]ThreadReadMessage, 0, len(records))
	for _, record := range records {
		env, err := message.Encode(record.Message)
		if err != nil {
			return nil, err
		}
		entry := ThreadReadMessage{
			ID:        record.ID,
			CreatedAt: record.CreatedAt,
			Role:      string(env.Role),
			Kind:      string(env.Kind),
			Text:      env.Text,
		}
		if entry.Text == "" && env.Raw != "" {
			entry.Text = env.Raw
		}
		if len(env.ToolCalls) > 0 {
			payload, err := json.Marshal(env.ToolCalls)
			if err != nil {
				return nil, err
			}
			entry.ToolCalls = payload
		}
		if env.ToolOutput != nil {
			payload, err := json.Marshal(env.ToolOutput)
			if err != nil {
				return nil, err
			}
			entry.ToolOutput = payload
		}
		messages = append(messages, entry)
	}
	return messages, nil
}

func (s *Server) handleThreadResume(ctx context.Context, req request, writer io.Writer) {
	var params ThreadResumeParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.writeResponse(writer, response{JSONRPC: jsonRPCVersion, ID: req.ID, Error: &rpcError{Code: -32602, Message: "invalid params"}})
		return
	}
	threadID := strings.TrimSpace(params.ThreadID)
	if threadID == "" {
		s.writeResponse(writer, response{JSONRPC: jsonRPCVersion, ID: req.ID, Error: &rpcError{Code: -32602, Message: "thread_id is required"}})
		return
	}
	prompt := strings.TrimSpace(params.Prompt)
	if prompt == "" {
		s.writeResponse(writer, response{JSONRPC: jsonRPCVersion, ID: req.ID, Error: &rpcError{Code: -32602, Message: "prompt is required"}})
		return
	}
	s.executeTurn(ctx, req, writer, threadID, prompt, params.RestrictOutput, params.Stream)
}

func (s *Server) executeTurn(ctx context.Context, req request, writer io.Writer, threadID, prompt string, restrictOutput, stream bool) {
	ctx, cancel := context.WithCancel(ctx)
	idKey := string(req.ID)
	s.storeCancel(idKey, cancel)
	defer s.removeCancel(idKey)

	input := loop.Input{
		ThreadID:       threadID,
		Prompt:         message.NewHumanMessage(prompt),
		RestrictOutput: restrictOutput,
		Stream:         stream,
	}
	if stream {
		input.EventSink = s.eventSink(writer, idKey)
	}
	result, err := s.agent.Run(ctx, input)
	s.flushSpans()
	if err != nil {
		s.writeResponse(writer, response{JSONRPC: jsonRPCVersion, ID: req.ID, Error: &rpcError{Code: -32603, Message: err.Error()}})
		return
	}

	s.writeResponse(writer, response{JSONRPC: jsonRPCVersion, ID: req.ID, Result: TurnResult{ThreadID: result.ThreadID, Response: result.Response}})
}

func (s *Server) flushSpans() {
	flushCtx, cancel := context.WithTimeout(context.Background(), telemetry.FlushTimeout)
	defer cancel()
	if err := s.flush(flushCtx); err != nil {
		fmt.Fprintf(os.Stderr, "agn server trace flush error: %v\n", err)
	}
}

func (s *Server) eventSink(writer io.Writer, requestID string) loop.EventSink {
	return func(event loop.Event) {
		switch event.Type {
		case loop.EventModelDelta:
			s.writeNotification(writer, "item/agentMessage/delta", AgentMessageDeltaNotification{
				RequestID: requestID,
				ThreadID:  event.ThreadID,
				TurnID:    event.TurnID,
				ItemID:    event.ItemID,
				Delta:     event.Delta,
			})
		case loop.EventItemStarted:
			s.writeNotification(writer, "item/started", ItemLifecycleNotification{
				RequestID: requestID,
				ThreadID:  event.ThreadID,
				TurnID:    event.TurnID,
				ItemID:    event.ItemID,
				ToolName:  event.ToolName,
			})
		case loop.EventItemDone:
			s.writeNotification(writer, "item/completed", ItemLifecycleNotification{
				RequestID: requestID,
				ThreadID:  event.ThreadID,
				TurnID:    event.TurnID,
				ItemID:    event.ItemID,
				ToolName:  event.ToolName,
			})
		case loop.EventTurnStarted:
			s.writeNotification(writer, "turn/started", TurnLifecycleNotification{
				RequestID: requestID,
				ThreadID:  event.ThreadID,
				TurnID:    event.TurnID,
			})
		case loop.EventTurnDone:
			s.writeNotification(writer, "turn/completed", TurnLifecycleNotification{
				RequestID: requestID,
				ThreadID:  event.ThreadID,
				TurnID:    event.TurnID,
			})
		}
	}
}

func (s *Server) writeNotification(writer io.Writer, method string, params interface{}) {
	notification := map[string]interface{}{
		"jsonrpc": jsonRPCVersion,
		"method":  method,
		"params":  params,
	}
	payload, err := json.Marshal(notification)
	if err != nil {
		s.signalShutdown(fmt.Errorf("marshal notification: %w", err))
		return
	}
	payload = append(payload, '\n')
	s.writeMu.Lock()
	_, err = writer.Write(payload)
	s.writeMu.Unlock()
	if err != nil {
		s.signalShutdown(fmt.Errorf("write notification: %w", err))
	}
}

func (s *Server) writeResponse(writer io.Writer, resp response) {
	resp.JSONRPC = jsonRPCVersion
	payload, err := json.Marshal(resp)
	if err != nil {
		s.signalShutdown(fmt.Errorf("marshal response: %w", err))
		return
	}
	payload = append(payload, '\n')
	s.writeMu.Lock()
	_, err = writer.Write(payload)
	s.writeMu.Unlock()
	if err != nil {
		s.signalShutdown(fmt.Errorf("write response: %w", err))
	}
}

func (s *Server) signalShutdown(err error) {
	if err == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "agn server error: %v\n", err)
	s.once.Do(func() {
		if s.shutdown != nil {
			s.shutdown()
		}
	})
}

func invalidRequestID(id json.RawMessage) json.RawMessage {
	if len(id) == 0 {
		return json.RawMessage("null")
	}
	return id
}

func (s *Server) storeCancel(id string, cancel context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inFlight[id] = cancel
}

func (s *Server) removeCancel(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.inFlight, id)
}

func (s *Server) cancel(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	cancel, ok := s.inFlight[id]
	if !ok {
		return false
	}
	cancel()
	delete(s.inFlight, id)
	return true
}
