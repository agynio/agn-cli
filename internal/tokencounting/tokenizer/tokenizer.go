package tokenizer

import (
	"context"
	"errors"
)

type Model string

const ModelGPT5 Model = "gpt-5"

type Message struct {
	Item Item
}

func NewMessage(item Item) Message {
	return Message{Item: item}
}

func CountTokens(ctx context.Context, model Model, messages []Message) ([]int32, error) {
	if len(messages) == 0 {
		return nil, nil
	}

	counter, err := newCounter(model)
	if err != nil {
		return nil, err
	}

	tokens := make([]int32, len(messages))
	for i, message := range messages {
		if message.Item == nil {
			return nil, errors.New("message item is required")
		}
		count, err := counter.countMessage(ctx, message)
		if err != nil {
			return nil, err
		}
		tokens[i] = int32(count)
	}
	return tokens, nil
}
