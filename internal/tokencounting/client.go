package tokencounting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/agynio/agn-cli/internal/llm"
	"github.com/agynio/agn-cli/internal/message"
	tokencountingv1 "github.com/agynio/agn-cli/internal/tokencounting/token_countingv1"
)

const (
	DefaultAddress = "token-counting:50051"
	DefaultTimeout = 5 * time.Second
)

var DefaultModel = tokencountingv1.TokenCountingModel_TOKEN_COUNTING_MODEL_GPT_5

type Client struct {
	conn    *grpc.ClientConn
	client  tokencountingv1.TokenCountingServiceClient
	model   tokencountingv1.TokenCountingModel
	timeout time.Duration
}

func New(address string, model tokencountingv1.TokenCountingModel) (*Client, error) {
	trimmed := strings.TrimSpace(address)
	if trimmed == "" {
		return nil, errors.New("token counting address is required")
	}
	conn, err := grpc.NewClient(trimmed, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("connect token counting: %w", err)
	}
	return &Client{
		conn:    conn,
		client:  tokencountingv1.NewTokenCountingServiceClient(conn),
		model:   model,
		timeout: DefaultTimeout,
	}, nil
}

func (c *Client) Close() error {
	return c.conn.Close()
}

func (c *Client) Count(msg message.Message) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	return c.CountWithContext(ctx, msg)
}

func (c *Client) CountWithContext(ctx context.Context, msg message.Message) (int, error) {
	items, err := llm.MessagesToInput([]message.Message{msg})
	if err != nil {
		return 0, err
	}
	if len(items) == 0 {
		return 0, errors.New("no input items for token counting")
	}
	payloads := make([][]byte, len(items))
	for i, item := range items {
		payload, err := json.Marshal(item)
		if err != nil {
			return 0, fmt.Errorf("marshal token counting item: %w", err)
		}
		payloads[i] = payload
	}
	resp, err := c.client.CountTokens(ctx, &tokencountingv1.CountTokensRequest{
		Model:    c.model,
		Messages: payloads,
	})
	if err != nil {
		return 0, err
	}
	if len(resp.Tokens) != len(payloads) {
		return 0, fmt.Errorf("token counting returned %d tokens for %d items", len(resp.Tokens), len(payloads))
	}
	count := 0
	for _, token := range resp.Tokens {
		count += int(token)
	}
	return count, nil
}
