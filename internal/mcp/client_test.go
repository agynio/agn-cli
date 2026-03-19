package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClientSequentialRequests(t *testing.T) {
	client, reqReader, respWriter, cleanup := setupClient(t)
	defer cleanup()

	ctx := context.Background()
	var tools []Tool
	var listErr error
	listDone := make(chan struct{})
	go func() {
		tools, listErr = client.ListTools(ctx)
		close(listDone)
	}()

	listReq := readRequest(t, reqReader)
	require.Equal(t, "2.0", listReq.JSONRPC)
	require.Equal(t, int64(1), listReq.ID)
	require.Equal(t, "tools/list", listReq.Method)

	expectedTools := []Tool{{
		Name:        "read",
		Description: "Read",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}}
	writeResponse(t, respWriter, listReq.ID, map[string]any{"tools": expectedTools}, nil)
	<-listDone
	require.NoError(t, listErr)
	require.Equal(t, expectedTools, tools)

	var result ToolResult
	var callErr error
	callDone := make(chan struct{})
	go func() {
		result, callErr = client.CallTool(ctx, ToolCall{
			ID:        "call-1",
			Name:      "read",
			Arguments: json.RawMessage(`{"path":"/tmp"}`),
		})
		close(callDone)
	}()

	callReq := readRequest(t, reqReader)
	require.Equal(t, "2.0", callReq.JSONRPC)
	require.Equal(t, int64(2), callReq.ID)
	require.Equal(t, "tools/call", callReq.Method)

	params, ok := callReq.Params.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "read", params["name"])
	args, ok := params["arguments"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "/tmp", args["path"])

	writeResponse(t, respWriter, callReq.ID, ToolResult{Content: "ok"}, nil)
	<-callDone
	require.NoError(t, callErr)
	require.Equal(t, ToolResult{Content: "ok"}, result)
}

func TestClientListToolsError(t *testing.T) {
	client, reqReader, respWriter, cleanup := setupClient(t)
	defer cleanup()

	ctx := context.Background()
	var err error
	done := make(chan struct{})
	go func() {
		_, err = client.ListTools(ctx)
		close(done)
	}()

	listReq := readRequest(t, reqReader)
	writeResponse(t, respWriter, listReq.ID, nil, &rpcError{Code: -32001, Message: "boom"})
	<-done
	require.Error(t, err)
}

func TestClientCallToolEmptyContent(t *testing.T) {
	client, reqReader, respWriter, cleanup := setupClient(t)
	defer cleanup()

	ctx := context.Background()
	var err error
	done := make(chan struct{})
	go func() {
		_, err = client.CallTool(ctx, ToolCall{Name: "read", Arguments: json.RawMessage(`{"path":"/tmp"}`)})
		close(done)
	}()

	callReq := readRequest(t, reqReader)
	writeResponse(t, respWriter, callReq.ID, ToolResult{Content: ""}, nil)
	<-done
	require.Error(t, err)
}

func TestNewProcessClientValidation(t *testing.T) {
	client, err := NewProcessClient(context.Background(), "  ")
	require.Error(t, err)
	require.Nil(t, client)
}

func setupClient(t *testing.T) (*Client, *bufio.Reader, io.WriteCloser, func()) {
	t.Helper()
	serverToClientR, serverToClientW := io.Pipe()
	clientToServerR, clientToServerW := io.Pipe()
	client := NewClient(serverToClientR, clientToServerW, clientToServerW)
	reader := bufio.NewReader(clientToServerR)
	cleanup := func() {
		_ = serverToClientW.Close()
		_ = clientToServerW.Close()
		_ = clientToServerR.Close()
		_ = serverToClientR.Close()
		_ = client.Close()
	}
	return client, reader, serverToClientW, cleanup
}

func readRequest(t *testing.T, reader *bufio.Reader) request {
	t.Helper()
	line, err := reader.ReadString('\n')
	require.NoError(t, err)
	line = strings.TrimSpace(line)
	var req request
	require.NoError(t, json.Unmarshal([]byte(line), &req))
	return req
}

func writeResponse(t *testing.T, writer io.Writer, id int64, result any, errObj *rpcError) {
	t.Helper()
	resp := response{JSONRPC: "2.0", ID: json.RawMessage(strconv.FormatInt(id, 10))}
	if errObj != nil {
		resp.Error = errObj
	} else {
		data, err := json.Marshal(result)
		require.NoError(t, err)
		resp.Result = data
	}
	payload, err := json.Marshal(resp)
	require.NoError(t, err)
	payload = append(payload, '\n')
	_, err = writer.Write(payload)
	require.NoError(t, err)
}
