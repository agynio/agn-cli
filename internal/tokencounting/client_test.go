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

type captureServer struct {
	tokencountingv1.UnimplementedTokenCountingServiceServer

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
	tokencountingv1.RegisterTokenCountingServiceServer(grpcServer, server)
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
		client:  tokencountingv1.NewTokenCountingServiceClient(conn),
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

	for idx, expected := range []struct {
		callID string
		name   string
	}{
		{callID: "call-1", name: "lookup"},
		{callID: "call-2", name: "search"},
	} {
		var payload map[string]any
		require.NoError(t, json.Unmarshal(req.Messages[idx], &payload))
		require.Equal(t, "function_call", payload["type"])
		require.Equal(t, expected.callID, payload["call_id"])
		require.Equal(t, expected.name, payload["name"])
	}
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
