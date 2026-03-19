package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultTimeout    = 60 * time.Second
	defaultToolChoice = "auto"
)

type Client struct {
	endpoint   string
	apiKey     string
	model      string
	httpClient *http.Client
}

func NewClient(endpoint, apiKey, model string) (*Client, error) {
	if strings.TrimSpace(endpoint) == "" {
		return nil, errors.New("endpoint is required")
	}
	if strings.TrimSpace(apiKey) == "" {
		return nil, errors.New("api key is required")
	}
	if strings.TrimSpace(model) == "" {
		return nil, errors.New("model is required")
	}
	if _, err := url.Parse(endpoint); err != nil {
		return nil, fmt.Errorf("invalid endpoint: %w", err)
	}
	return &Client{
		endpoint:   endpoint,
		apiKey:     apiKey,
		model:      model,
		httpClient: &http.Client{Timeout: defaultTimeout},
	}, nil
}

func (c *Client) CreateResponse(ctx context.Context, input []InputMessage, tools []ToolDefinition, stream bool, onDelta func(string)) (Response, error) {
	if len(input) == 0 {
		return Response{}, errors.New("input messages are required")
	}
	req := Request{
		Model:      c.model,
		Input:      input,
		Tools:      tools,
		ToolChoice: defaultToolChoice,
		Stream:     stream,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return Response{}, fmt.Errorf("marshal request: %w", err)
	}

	endpoint, err := url.JoinPath(c.endpoint, "responses")
	if err != nil {
		return Response{}, fmt.Errorf("build endpoint: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("call llm: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		payload, _ := io.ReadAll(resp.Body)
		return Response{}, fmt.Errorf("llm error: status %d: %s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}

	if stream {
		return decodeStream(resp.Body, onDelta)
	}

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return Response{}, fmt.Errorf("read response: %w", err)
	}

	var result Response
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return Response{}, fmt.Errorf("parse response: %w", err)
	}

	if onDelta != nil {
		text := strings.TrimSpace(result.Text())
		if text != "" {
			onDelta(text)
		}
	}

	return result, nil
}

type streamEnvelope struct {
	Type     string    `json:"type"`
	Delta    string    `json:"delta,omitempty"`
	Response *Response `json:"response,omitempty"`
}

func decodeStream(reader io.Reader, onDelta func(string)) (Response, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	var final *Response
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}
		var envelope streamEnvelope
		if err := json.Unmarshal([]byte(payload), &envelope); err != nil {
			return Response{}, fmt.Errorf("parse stream event: %w", err)
		}
		if envelope.Delta != "" && onDelta != nil {
			onDelta(envelope.Delta)
		}
		if envelope.Response != nil {
			final = envelope.Response
		}
	}
	if err := scanner.Err(); err != nil {
		return Response{}, fmt.Errorf("read stream: %w", err)
	}
	if final == nil {
		return Response{}, errors.New("stream completed without response")
	}
	return *final, nil
}
