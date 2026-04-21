//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"sync"
	"testing"

	tokencountingv1 "github.com/agynio/agn-cli/internal/tokencounting/token_countingv1"
	"google.golang.org/grpc"
)

var (
	tokenCountingOnce sync.Once
	tokenCountingAddr string
	tokenCountingErr  error
)

func tokenCountingAddress(t *testing.T) string {
	t.Helper()
	tokenCountingOnce.Do(func() {
		tokenCountingAddr, tokenCountingErr = startTokenCountingServer()
	})
	if tokenCountingErr != nil {
		t.Fatalf("start token counting server: %v", tokenCountingErr)
	}
	return tokenCountingAddr
}

func startTokenCountingServer() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	server := grpc.NewServer()
	tokencountingv1.RegisterTokenCountingServiceServer(server, tokenCountingServer{})
	go func() {
		_ = server.Serve(listener)
	}()
	return listener.Addr().String(), nil
}

type tokenCountingServer struct {
	tokencountingv1.UnimplementedTokenCountingServiceServer
}

func (tokenCountingServer) CountTokens(_ context.Context, req *tokencountingv1.CountTokensRequest) (*tokencountingv1.CountTokensResponse, error) {
	if req == nil {
		return &tokencountingv1.CountTokensResponse{}, nil
	}
	tokens := make([]int32, len(req.Messages))
	for i, payload := range req.Messages {
		text := extractTokenText(payload)
		if strings.TrimSpace(text) == "" {
			tokens[i] = tokenCountForText(string(payload))
			continue
		}
		tokens[i] = tokenCountForText(text)
	}
	return &tokencountingv1.CountTokensResponse{Tokens: tokens}, nil
}

func extractTokenText(payload []byte) string {
	var envelope struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return ""
	}
	switch envelope.Type {
	case "message":
		var msg struct {
			Content []json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(payload, &msg); err != nil {
			return ""
		}
		return joinContentParts(msg.Content)
	case "function_call":
		var call struct {
			Arguments string `json:"arguments"`
		}
		if err := json.Unmarshal(payload, &call); err != nil {
			return ""
		}
		return call.Arguments
	case "function_call_output":
		var output struct {
			Output json.RawMessage `json:"output"`
		}
		if err := json.Unmarshal(payload, &output); err != nil {
			return ""
		}
		return extractOutputText(output.Output)
	default:
		return ""
	}
}

func extractOutputText(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return ""
	}
	if trimmed[0] == '"' {
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return ""
		}
		return text
	}
	if trimmed[0] == '[' {
		var parts []json.RawMessage
		if err := json.Unmarshal(raw, &parts); err != nil {
			return ""
		}
		return joinContentParts(parts)
	}
	return ""
}

func joinContentParts(parts []json.RawMessage) string {
	texts := make([]string, 0, len(parts))
	for _, part := range parts {
		var envelope struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(part, &envelope); err != nil {
			continue
		}
		switch envelope.Type {
		case "input_text", "output_text":
			var content struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal(part, &content); err != nil {
				continue
			}
			if text := strings.TrimSpace(content.Text); text != "" {
				texts = append(texts, text)
			}
		case "refusal":
			var content struct {
				Refusal string `json:"refusal"`
			}
			if err := json.Unmarshal(part, &content); err != nil {
				continue
			}
			if text := strings.TrimSpace(content.Refusal); text != "" {
				texts = append(texts, text)
			}
		}
	}
	return strings.Join(texts, " ")
}

func tokenCountForText(text string) int32 {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return 1
	}
	length := len([]rune(trimmed))
	divisor := 4
	if length >= 120 {
		divisor = 2
	}
	return int32(length/divisor + 1)
}
