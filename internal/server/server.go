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

	"github.com/agynio/agn-cli/internal/loop"
	"github.com/agynio/agn-cli/internal/message"
)

const jsonRPCVersion = "2.0"

type Server struct {
	agent    *loop.Agent
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
	ConversationID string `json:"conversation_id,omitempty"`
	RestrictOutput bool   `json:"restrict_output,omitempty"`
	Stream         bool   `json:"stream,omitempty"`
}

type TurnResult struct {
	ConversationID string `json:"conversation_id"`
	Response       string `json:"response"`
}

type CancelParams struct {
	RequestID string `json:"request_id"`
}

type EventNotification struct {
	RequestID  string `json:"request_id"`
	Type       string `json:"type"`
	Delta      string `json:"delta,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
}

func New(agent *loop.Agent) *Server {
	return &Server{
		agent:    agent,
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
		case "agent.turn":
			go s.handleTurn(ctx, req, writer)
		case "agent.cancel":
			go s.handleCancel(ctx, req, writer)
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
	ctx, cancel := context.WithCancel(ctx)
	idKey := string(req.ID)
	s.storeCancel(idKey, cancel)
	defer s.removeCancel(idKey)

	input := loop.Input{
		ConversationID: params.ConversationID,
		Prompt:         message.NewHumanMessage(prompt),
		RestrictOutput: params.RestrictOutput,
		Stream:         params.Stream,
	}
	if params.Stream {
		input.EventSink = s.eventSink(writer, idKey)
	}
	result, err := s.agent.Run(ctx, input)
	if err != nil {
		s.writeResponse(writer, response{JSONRPC: jsonRPCVersion, ID: req.ID, Error: &rpcError{Code: -32603, Message: err.Error()}})
		return
	}

	s.writeResponse(writer, response{JSONRPC: jsonRPCVersion, ID: req.ID, Result: TurnResult{ConversationID: result.ConversationID, Response: result.Response}})
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

func (s *Server) eventSink(writer io.Writer, requestID string) loop.EventSink {
	return func(event loop.Event) {
		notification := map[string]interface{}{
			"jsonrpc": jsonRPCVersion,
			"method":  "agent.event",
			"params": EventNotification{
				RequestID:  requestID,
				Type:       string(event.Type),
				Delta:      event.Delta,
				ToolName:   event.ToolName,
				ToolCallID: event.ToolCallID,
			},
		}
		payload, err := json.Marshal(notification)
		if err != nil {
			s.signalShutdown(fmt.Errorf("marshal event: %w", err))
			return
		}
		payload = append(payload, '\n')
		s.writeMu.Lock()
		_, err = writer.Write(payload)
		s.writeMu.Unlock()
		if err != nil {
			s.signalShutdown(fmt.Errorf("write event: %w", err))
		}
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
