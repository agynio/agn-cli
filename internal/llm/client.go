package llm

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
)

type Client struct {
	model    string
	endpoint string
	sdk      openai.Client
}

func NewClient(endpoint, apiKey, model string) (*Client, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
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
	return &Client{model: model, endpoint: endpoint, sdk: sdk}, nil
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
		params.ParallelToolCalls = openai.Bool(true)
	}
	if strings.TrimSpace(instructions) != "" {
		params.Instructions = openai.String(strings.TrimSpace(instructions))
	}

	log.Printf(
		"agn: llm request endpoint=%s model=%s input_items=%d tools=%d stream=%t",
		c.endpoint,
		c.model,
		len(input),
		len(tools),
		stream,
	)

	if stream {
		streamResp := c.sdk.Responses.NewStreaming(ctx, params)
		var final *responses.Response
		for streamResp.Next() {
			event := streamResp.Current()
			if event.Type == "response.output_text.delta" && event.Delta != "" && onDelta != nil {
				onDelta(event.Delta)
			}
			if event.Type == "response.completed" {
				response := event.Response
				final = &response
			}
		}
		if err := streamResp.Err(); err != nil {
			wrapped := fmt.Errorf("read stream: %w", err)
			logLLMError(wrapped)
			return nil, wrapped
		}
		if final == nil {
			err := errors.New("stream completed without response")
			logLLMError(err)
			return nil, err
		}
		logLLMSuccess(final)
		return final, nil
	}

	response, err := c.sdk.Responses.New(ctx, params)
	if err != nil {
		wrapped := fmt.Errorf("call llm: %w", err)
		logLLMError(wrapped)
		return nil, wrapped
	}
	logLLMSuccess(response)
	if onDelta != nil {
		text := strings.TrimSpace(response.OutputText())
		if text != "" {
			onDelta(text)
		}
	}
	return response, nil
}

func logLLMError(err error) {
	var apiErr *openai.Error
	if errors.As(err, &apiErr) && apiErr.StatusCode != 0 {
		log.Printf("agn: llm error status=%d err=%v", apiErr.StatusCode, err)
		return
	}
	log.Printf("agn: llm error err=%v", err)
}

func logLLMSuccess(response *responses.Response) {
	if response.JSON.Usage.Valid() {
		log.Printf(
			"agn: llm response id=%s model=%s usage_input=%d usage_output=%d usage_total=%d",
			response.ID,
			response.Model,
			response.Usage.InputTokens,
			response.Usage.OutputTokens,
			response.Usage.TotalTokens,
		)
		return
	}
	log.Printf("agn: llm response id=%s model=%s", response.ID, response.Model)
}
