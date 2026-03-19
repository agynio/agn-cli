package server

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/agynio/agn-cli/internal/loop"
)

func TestServeErrorResponses(t *testing.T) {
	cases := []struct {
		name       string
		input      string
		expectedID string
		code       int
		message    string
	}{
		{
			name:       "parse error",
			input:      "not-json",
			expectedID: "null",
			code:       -32700,
			message:    "parse error",
		},
		{
			name:       "invalid request missing method",
			input:      `{"jsonrpc":"2.0","id":1,"method":""}`,
			expectedID: "1",
			code:       -32600,
			message:    "invalid request",
		},
		{
			name:       "invalid request missing id",
			input:      `{"jsonrpc":"2.0","method":"agent.turn","params":{}}`,
			expectedID: "null",
			code:       -32600,
			message:    "invalid request",
		},
		{
			name:       "method not found",
			input:      `{"jsonrpc":"2.0","id":1,"method":"agent.unknown","params":{}}`,
			expectedID: "1",
			code:       -32601,
			message:    "method not found",
		},
		{
			name:       "invalid params",
			input:      `{"jsonrpc":"2.0","id":1,"method":"agent.turn","params":"nope"}`,
			expectedID: "1",
			code:       -32602,
			message:    "invalid params",
		},
		{
			name:       "prompt required",
			input:      `{"jsonrpc":"2.0","id":1,"method":"agent.turn","params":{"prompt":" "}}`,
			expectedID: "1",
			code:       -32602,
			message:    "prompt is required",
		},
		{
			name:       "request id required",
			input:      `{"jsonrpc":"2.0","id":1,"method":"agent.cancel","params":{"request_id":" "}}`,
			expectedID: "1",
			code:       -32602,
			message:    "request_id is required",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := runServerRequest(t, tc.input)
			require.Equal(t, jsonRPCVersion, resp.JSONRPC)
			require.NotNil(t, resp.Error)
			require.Equal(t, tc.code, resp.Error.Code)
			require.Equal(t, tc.message, resp.Error.Message)
			require.Equal(t, tc.expectedID, string(resp.ID))
		})
	}
}

func runServerRequest(t *testing.T, input string) response {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	server := New(&loop.Agent{})
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(ctx, inR, outW)
	}()

	_, err := io.WriteString(inW, input+"\n")
	require.NoError(t, err)

	scanner := bufio.NewScanner(outR)
	require.True(t, scanner.Scan())
	var resp response
	require.NoError(t, json.Unmarshal(scanner.Bytes(), &resp))

	cancel()
	_ = inW.Close()
	require.ErrorIs(t, <-done, context.Canceled)
	_ = outR.Close()
	_ = outW.Close()
	return resp
}
