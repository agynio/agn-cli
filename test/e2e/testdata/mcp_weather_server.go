package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
}

type toolListResult struct {
	Tools []tool `json:"tools"`
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

const weatherContent = "{\"temperature\": \"18\u00b0C\", \"condition\": \"partly cloudy\"}"

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	writer := bufio.NewWriter(os.Stdout)
	defer func() {
		_ = writer.Flush()
	}()

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var req request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			fmt.Fprintf(os.Stderr, "mcp weather server: parse request: %v\n", err)
			continue
		}
		resp := response{JSONRPC: "2.0", ID: req.ID}
		switch req.Method {
		case "tools/list":
			resp.Result = toolListResult{Tools: []tool{weatherTool()}}
		case "tools/call":
			result, err := handleToolCall(req.Params)
			if err != nil {
				resp.Error = &rpcError{Code: -32602, Message: err.Error()}
				break
			}
			resp.Result = result
		default:
			resp.Error = &rpcError{Code: -32601, Message: "method not found"}
		}
		if err := writeResponse(writer, resp); err != nil {
			fmt.Fprintf(os.Stderr, "mcp weather server: write response: %v\n", err)
			return
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "mcp weather server: read input: %v\n", err)
	}
}

func weatherTool() tool {
	schema := json.RawMessage(`{"type":"object","properties":{"location":{"type":"string"}},"required":["location"],"additionalProperties":false}`)
	return tool{Name: "get_weather", InputSchema: schema}
}

func handleToolCall(raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("missing params")
	}
	var params toolCallParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, fmt.Errorf("invalid params")
	}
	if strings.TrimSpace(params.Name) != "get_weather" {
		return nil, fmt.Errorf("unknown tool")
	}
	return map[string]any{
		"content": []map[string]string{{"type": "text", "text": weatherContent}},
	}, nil
}

func writeResponse(writer *bufio.Writer, resp response) error {
	payload, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	if _, err := writer.Write(payload); err != nil {
		return err
	}
	return writer.Flush()
}
