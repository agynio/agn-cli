package tokencounting

import (
	"context"
	"encoding/json"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/agynio/agn-cli/internal/message"
	tokencountingv1 "github.com/agynio/agn-cli/internal/tokencounting/token_countingv1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

const bufSize = 1024 * 1024

const tokenCountingGatewayServiceName = "agynio.api.gateway.v1.TokenCountingGateway"

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

type captureServer struct {
	mu   sync.Mutex
	req  *tokencountingv1.CountTokensRequest
	resp *tokencountingv1.CountTokensResponse
	err  error
}

func (s *captureServer) CountTokens(ctx context.Context, req *tokencountingv1.CountTokensRequest) (*tokencountingv1.CountTokensResponse, error) {
	s.mu.Lock()
	s.req = req
	resp := s.resp
	err := s.err
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (s *captureServer) LastRequest() *tokencountingv1.CountTokensRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.req
}

func newBufconnClient(t *testing.T, server *captureServer) (*Client, func()) {
	t.Helper()
	listener := bufconn.Listen(bufSize)
	grpcServer := grpc.NewServer()
	registerTokenCountingGatewayServer(grpcServer, server)
	go func() {
		_ = grpcServer.Serve(listener)
	}()
	dialer := func(ctx context.Context, address string) (net.Conn, error) {
		return listener.Dial()
	}
	conn, err := grpc.DialContext(context.Background(), "bufnet", grpc.WithContextDialer(dialer), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	client := &Client{
		conn:    conn,
		model:   DefaultModel,
		timeout: time.Second,
	}
	cleanup := func() {
		_ = client.Close()
		grpcServer.Stop()
		_ = listener.Close()
	}
	return client, cleanup
}

func TestClientCountSumsTokens(t *testing.T) {
	server := &captureServer{resp: &tokencountingv1.CountTokensResponse{Tokens: []int32{3, 5}}}
	client, cleanup := newBufconnClient(t, server)
	defer cleanup()

	msg := message.NewToolCallMessage([]message.ToolCall{
		{ID: "call-1", Name: "lookup", Arguments: `{"q":"one"}`},
		{ID: "call-2", Name: "search", Arguments: `{"q":"two"}`},
	})
	count, err := client.CountWithContext(context.Background(), msg)
	require.NoError(t, err)
	require.Equal(t, 8, count)

	req := server.LastRequest()
	require.NotNil(t, req)
	require.Equal(t, DefaultModel, req.Model)
	require.Len(t, req.Messages, 2)

	for idx, expected := range []string{`{"q":"one"}`, `{"q":"two"}`} {
		var payload struct {
			Type      string `json:"type"`
			Arguments string `json:"arguments"`
		}
		require.NoError(t, json.Unmarshal(req.Messages[idx], &payload))
		require.Equal(t, "function_call", payload.Type)
		require.Equal(t, expected, payload.Arguments)
	}
}

func TestClientCountMessagePayload(t *testing.T) {
	server := &captureServer{resp: &tokencountingv1.CountTokensResponse{Tokens: []int32{1}}}
	client, cleanup := newBufconnClient(t, server)
	defer cleanup()

	_, err := client.CountWithContext(context.Background(), message.NewHumanMessage("hello"))
	require.NoError(t, err)

	req := server.LastRequest()
	require.NotNil(t, req)
	require.Len(t, req.Messages, 1)

	var payload struct {
		Type    string `json:"type"`
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	require.NoError(t, json.Unmarshal(req.Messages[0], &payload))
	require.Equal(t, "message", payload.Type)
	require.Equal(t, "user", payload.Role)
	require.Len(t, payload.Content, 1)
	require.Equal(t, "input_text", payload.Content[0].Type)
	require.Equal(t, "hello", payload.Content[0].Text)
}

func TestClientCountLengthMismatch(t *testing.T) {
	server := &captureServer{resp: &tokencountingv1.CountTokensResponse{Tokens: []int32{3}}}
	client, cleanup := newBufconnClient(t, server)
	defer cleanup()

	msg := message.NewToolCallMessage([]message.ToolCall{
		{ID: "call-1", Name: "lookup", Arguments: `{"q":"one"}`},
		{ID: "call-2", Name: "search", Arguments: `{"q":"two"}`},
	})
	_, err := client.CountWithContext(context.Background(), msg)
	require.Error(t, err)
	require.ErrorContains(t, err, "token counting returned 1 tokens for 2 items")
}
