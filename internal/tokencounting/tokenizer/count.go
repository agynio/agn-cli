package tokenizer

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/pkoukk/tiktoken-go"
)

const (
	gpt5MessageOverheadTokens = 3
	gpt5SystemOverheadTokens  = 6
	gpt5ImageBaseTokens       = 70
	gpt5ImageTileTokens       = 140
)

type counter struct {
	model      Model
	encoding   *tiktoken.Tiktoken
	httpClient *http.Client
}

func newCounter(model Model) (*counter, error) {
	if model != ModelGPT5 {
		return nil, fmt.Errorf("unsupported model: %s", model)
	}
	enc, err := encodingForModel(model)
	if err != nil {
		return nil, err
	}
	return &counter{
		model:    model,
		encoding: enc,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}, nil
}

func (c *counter) countMessage(ctx context.Context, message Message) (int, error) {
	switch item := message.Item.(type) {
	case MessageItem:
		return c.countMessageItem(ctx, item)
	case *MessageItem:
		if item == nil {
			return 0, errors.New("message item is nil")
		}
		return c.countMessageItem(ctx, *item)
	case FunctionCallItem:
		return c.countFunctionCall(ctx, item)
	case *FunctionCallItem:
		if item == nil {
			return 0, errors.New("function call item is nil")
		}
		return c.countFunctionCall(ctx, *item)
	case FunctionCallOutputItem:
		return c.countFunctionCallOutput(ctx, item)
	case *FunctionCallOutputItem:
		if item == nil {
			return 0, errors.New("function call output item is nil")
		}
		return c.countFunctionCallOutput(ctx, *item)
	default:
		return 0, fmt.Errorf("unsupported message item %T", message.Item)
	}
}

func (c *counter) countMessageItem(ctx context.Context, item MessageItem) (int, error) {
	count := c.messageOverhead(item.Role)
	for _, part := range item.Content {
		partTokens, err := c.countContentPart(ctx, part)
		if err != nil {
			return 0, err
		}
		count += partTokens
	}
	return count, nil
}

func (c *counter) countFunctionCall(ctx context.Context, item FunctionCallItem) (int, error) {
	count := c.functionCallOverhead()
	count += c.countText(item.Arguments)
	return count, nil
}

func (c *counter) countFunctionCallOutput(ctx context.Context, item FunctionCallOutputItem) (int, error) {
	count := c.functionCallOverhead()
	if item.Output.IsText {
		count += c.countText(item.Output.Text)
		return count, nil
	}
	for _, part := range item.Output.Content {
		partTokens, err := c.countContentPart(ctx, part)
		if err != nil {
			return 0, err
		}
		count += partTokens
	}
	return count, nil
}

func (c *counter) countContentPart(ctx context.Context, part ContentPart) (int, error) {
	switch part.Type {
	case ContentTypeInputText, ContentTypeOutputText, ContentTypeRefusal:
		return c.countText(part.Text), nil
	case ContentTypeInputImage:
		return c.countImageTokens(ctx, part.Image)
	case ContentTypeInputFile:
		return c.countFileTokens(ctx, part.File)
	case ContentTypeInputAudio:
		return 0, errors.New("audio content is not supported")
	default:
		return 0, fmt.Errorf("unsupported content type %q", part.Type)
	}
}

func (c *counter) countText(text string) int {
	if text == "" {
		return 0
	}
	return len(c.encoding.Encode(text, nil, nil))
}

func (c *counter) messageOverhead(role MessageRole) int {
	if role == RoleSystem {
		return gpt5SystemOverheadTokens
	}
	return gpt5MessageOverheadTokens
}

func (c *counter) functionCallOverhead() int {
	return gpt5MessageOverheadTokens
}

var (
	encodingOnce sync.Once
	encodingInst *tiktoken.Tiktoken
	encodingErr  error
)

func encodingForModel(model Model) (*tiktoken.Tiktoken, error) {
	if model != ModelGPT5 {
		return nil, errors.New("unsupported model")
	}
	configureBpeLoader()
	encodingOnce.Do(func() {
		encodingInst, encodingErr = tiktoken.GetEncoding("o200k_base")
	})
	if encodingErr != nil {
		return nil, fmt.Errorf("load tokenizer: %w", encodingErr)
	}
	return encodingInst, nil
}
