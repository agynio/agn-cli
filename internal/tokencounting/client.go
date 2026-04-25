package tokencounting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/agynio/agn-cli/internal/message"
	"github.com/agynio/agn-cli/internal/tokencounting/tokenizer"
)

type Counter struct {
	model tokenizer.Model
}

func New() (*Counter, error) {
	return &Counter{model: tokenizer.ModelGPT5}, nil
}

func (c *Counter) Close() error {
	return nil
}

func (c *Counter) Count(msg message.Message) (int, error) {
	return c.CountWithContext(context.Background(), msg)
}

func (c *Counter) CountWithContext(ctx context.Context, msg message.Message) (int, error) {
	items, err := tokenCountingItems(msg)
	if err != nil {
		return 0, err
	}
	if len(items) == 0 {
		return 0, errors.New("no input items for token counting")
	}
	parsed := make([]tokenizer.Message, len(items))
	for i, item := range items {
		payload, err := json.Marshal(item)
		if err != nil {
			return 0, fmt.Errorf("marshal token counting item: %w", err)
		}
		message, err := tokenizer.ParseMessage(payload)
		if err != nil {
			return 0, fmt.Errorf("parse token counting item: %w", err)
		}
		parsed[i] = message
	}
	tokens, err := tokenizer.CountTokens(ctx, c.model, parsed)
	if err != nil {
		return 0, fmt.Errorf("count tokens: %w", err)
	}
	if len(tokens) != len(parsed) {
		return 0, fmt.Errorf("token counting returned %d tokens for %d items", len(tokens), len(parsed))
	}
	count := 0
	for _, token := range tokens {
		count += int(token)
	}
	return count, nil
}
