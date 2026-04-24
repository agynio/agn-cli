//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

const (
	tokenCountingGatewayServiceName       = "agynio.api.gateway.v1.TokenCountingGateway"
	tokenCountingGatewayCountTokensMethod = "/agynio.api.gateway.v1.TokenCountingGateway/CountTokens"
)

type tokenCountingGatewayServer interface {
	CountTokens(context.Context, *tokencountingv1.CountTokensRequest) (*tokencountingv1.CountTokensResponse, error)
}

var tokenCountingGatewayServiceDesc = grpc.ServiceDesc{
	ServiceName: tokenCountingGatewayServiceName,
	HandlerType: (*tokenCountingGatewayServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "CountTokens",
			Handler:    tokenCountingGatewayCountTokensHandler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "agynio/api/gateway/v1/token_counting.proto",
}

func registerTokenCountingGatewayServer(s grpc.ServiceRegistrar, srv tokenCountingGatewayServer) {
	s.RegisterService(&tokenCountingGatewayServiceDesc, srv)
}

func tokenCountingGatewayCountTokensHandler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(tokencountingv1.CountTokensRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(tokenCountingGatewayServer).CountTokens(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: tokenCountingGatewayCountTokensMethod,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(tokenCountingGatewayServer).CountTokens(ctx, req.(*tokencountingv1.CountTokensRequest))
	}
	return interceptor(ctx, in, info, handler)
}

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
	registerTokenCountingGatewayServer(server, tokenCountingServer{})
	go func() {
		_ = server.Serve(listener)
	}()
	return listener.Addr().String(), nil
}

type tokenCountingServer struct {
}

func (tokenCountingServer) CountTokens(_ context.Context, req *tokencountingv1.CountTokensRequest) (*tokencountingv1.CountTokensResponse, error) {
	if req == nil {
		return nil, errors.New("token counting request is required")
	}
	if len(req.Messages) == 0 {
		return nil, errors.New("token counting messages are required")
	}
	tokens := make([]int32, len(req.Messages))
	for i, payload := range req.Messages {
		text, err := parseTokenCountingPayload(payload)
		if err != nil {
			return nil, fmt.Errorf("message[%d]: %w", i, err)
		}
		tokens[i] = tokenCountForText(text)
	}
	return &tokencountingv1.CountTokensResponse{Tokens: tokens}, nil
}

const (
	itemTypeMessage            = "message"
	itemTypeFunctionCall       = "function_call"
	itemTypeFunctionCallOutput = "function_call_output"

	contentTypeInputText  = "input_text"
	contentTypeOutputText = "output_text"
	contentTypeRefusal    = "refusal"
	contentTypeInputImage = "input_image"
	contentTypeInputFile  = "input_file"
	contentTypeInputAudio = "input_audio"
)

func parseTokenCountingPayload(payload []byte) (string, error) {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 {
		return "", errors.New("message is empty")
	}
	var envelope struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(trimmed, &envelope); err != nil {
		return "", fmt.Errorf("parse message json: %w", err)
	}
	switch envelope.Type {
	case itemTypeMessage:
		return parseMessageItem(trimmed)
	case itemTypeFunctionCall:
		return parseFunctionCallItem(trimmed)
	case itemTypeFunctionCallOutput:
		return parseFunctionCallOutputItem(trimmed)
	case "":
		return "", errors.New("message type is required")
	default:
		return "", fmt.Errorf("unsupported message type %q", envelope.Type)
	}
}

func parseMessageItem(payload []byte) (string, error) {
	var raw struct {
		Role    *string           `json:"role"`
		Content []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return "", fmt.Errorf("parse message item: %w", err)
	}
	if raw.Role == nil {
		return "", errors.New("message role is required")
	}
	role := strings.TrimSpace(*raw.Role)
	if role == "" {
		return "", errors.New("message role is required")
	}
	if !isValidRole(role) {
		return "", fmt.Errorf("unsupported message role %q", role)
	}
	if raw.Content == nil {
		return "", errors.New("message content is required")
	}
	texts := make([]string, 0, len(raw.Content))
	for i, part := range raw.Content {
		text, err := parseContentPart(part)
		if err != nil {
			return "", fmt.Errorf("content[%d]: %w", i, err)
		}
		if trimmed := strings.TrimSpace(text); trimmed != "" {
			texts = append(texts, trimmed)
		}
	}
	return strings.Join(texts, " "), nil
}

func parseFunctionCallItem(payload []byte) (string, error) {
	var raw struct {
		Arguments *string `json:"arguments"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return "", fmt.Errorf("parse function call item: %w", err)
	}
	if raw.Arguments == nil {
		return "", errors.New("function call arguments are required")
	}
	return *raw.Arguments, nil
}

func parseFunctionCallOutputItem(payload []byte) (string, error) {
	var raw struct {
		Output json.RawMessage `json:"output"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return "", fmt.Errorf("parse function call output item: %w", err)
	}
	if len(raw.Output) == 0 {
		return "", errors.New("function call output is required")
	}
	return parseFunctionCallOutput(raw.Output)
}

func parseFunctionCallOutput(raw json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return "", errors.New("function call output is required")
	}
	if trimmed[0] == '"' {
		var text string
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return "", fmt.Errorf("parse function call output string: %w", err)
		}
		return text, nil
	}
	if trimmed[0] != '[' {
		return "", errors.New("function call output must be string or array")
	}
	var parts []json.RawMessage
	if err := json.Unmarshal(trimmed, &parts); err != nil {
		return "", fmt.Errorf("parse function call output array: %w", err)
	}
	texts := make([]string, 0, len(parts))
	for i, part := range parts {
		text, err := parseContentPart(part)
		if err != nil {
			return "", fmt.Errorf("output[%d]: %w", i, err)
		}
		if trimmed := strings.TrimSpace(text); trimmed != "" {
			texts = append(texts, trimmed)
		}
	}
	return strings.Join(texts, " "), nil
}

func parseContentPart(raw json.RawMessage) (string, error) {
	var envelope struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return "", fmt.Errorf("parse content type: %w", err)
	}
	if strings.TrimSpace(envelope.Type) == "" {
		return "", errors.New("content type is required")
	}
	switch envelope.Type {
	case contentTypeInputText, contentTypeOutputText:
		var content struct {
			Text *string `json:"text"`
		}
		if err := json.Unmarshal(raw, &content); err != nil {
			return "", fmt.Errorf("parse text content: %w", err)
		}
		if content.Text == nil {
			return "", errors.New("text content is required")
		}
		return *content.Text, nil
	case contentTypeRefusal:
		var content struct {
			Refusal *string `json:"refusal"`
		}
		if err := json.Unmarshal(raw, &content); err != nil {
			return "", fmt.Errorf("parse refusal content: %w", err)
		}
		if content.Refusal == nil {
			return "", errors.New("refusal content is required")
		}
		return *content.Refusal, nil
	case contentTypeInputImage:
		var content struct {
			ImageURL *string `json:"image_url"`
			FileID   *string `json:"file_id"`
			Detail   string  `json:"detail"`
		}
		if err := json.Unmarshal(raw, &content); err != nil {
			return "", fmt.Errorf("parse image content: %w", err)
		}
		imageURL := strings.TrimSpace(optString(content.ImageURL))
		fileID := strings.TrimSpace(optString(content.FileID))
		if imageURL == "" && fileID == "" {
			return "", errors.New("image content requires image_url or file_id")
		}
		detailValue := strings.ToLower(strings.TrimSpace(content.Detail))
		switch detailValue {
		case "":
		case "high":
		case "low":
		default:
			return "", fmt.Errorf("unsupported image detail %q", content.Detail)
		}
		return "", nil
	case contentTypeInputFile:
		var content struct {
			FileData *string `json:"file_data"`
			Filename *string `json:"filename"`
		}
		if err := json.Unmarshal(raw, &content); err != nil {
			return "", fmt.Errorf("parse file content: %w", err)
		}
		if content.FileData == nil || strings.TrimSpace(*content.FileData) == "" {
			return "", errors.New("file_data is required")
		}
		if content.Filename == nil || strings.TrimSpace(*content.Filename) == "" {
			return "", errors.New("filename is required")
		}
		return "", nil
	case contentTypeInputAudio:
		return "", nil
	default:
		return "", fmt.Errorf("unsupported content type %q", envelope.Type)
	}
}

func optString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func isValidRole(role string) bool {
	switch role {
	case "system", "user", "assistant", "tool":
		return true
	default:
		return false
	}
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
	} else if length >= 40 {
		divisor = 3
	}
	return int32(length/divisor + 1)
}
