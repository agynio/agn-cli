package llm

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
)

type Client struct {
	model string
	sdk   openai.Client
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
	sdk := openai.NewClient(
		option.WithAPIKey(apiKey),
		option.WithBaseURL(endpoint),
		option.WithMaxRetries(2),
	)
	return &Client{model: model, sdk: sdk}, nil
}

func (c *Client) CreateResponse(
	ctx context.Context,
	instructions string,
	input []responses.ResponseInputItemUnionParam,
	tools []responses.ToolUnionParam,
	toolChoice responses.ResponseNewParamsToolChoiceUnion,
	stream bool,
	onDelta func(string),
) (*responses.Response, error) {
	if len(input) == 0 {
		return nil, errors.New("input messages are required")
	}
	params := responses.ResponseNewParams{
		Model:      c.model,
		Input:      responses.ResponseNewParamsInputUnion{OfInputItemList: responses.ResponseInputParam(input)},
		Store:      openai.Bool(false),
		ToolChoice: toolChoice,
	}
	if len(tools) > 0 {
		params.Tools = tools
	}
	if strings.TrimSpace(instructions) != "" {
		params.Instructions = openai.String(strings.TrimSpace(instructions))
	}

	if stream {
		streamResp := c.sdk.Responses.NewStreaming(ctx, params)
		var final *responses.Response
		for streamResp.Next() {
			event := streamResp.Current()
			if event.Delta != "" && onDelta != nil {
				onDelta(event.Delta)
			}
			if event.Type == "response.completed" {
				response := event.Response
				final = &response
			}
		}
		if err := streamResp.Err(); err != nil {
			return nil, fmt.Errorf("read stream: %w", err)
		}
		if final == nil {
			return nil, errors.New("stream completed without response")
		}
		return final, nil
	}

	response, err := c.sdk.Responses.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("call llm: %w", err)
	}
	if onDelta != nil {
		text := strings.TrimSpace(response.OutputText())
		if text != "" {
			onDelta(text)
		}
	}
	return response, nil
}
