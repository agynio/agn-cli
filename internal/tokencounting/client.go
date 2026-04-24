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

	"github.com/agynio/agn-cli/internal/message"
	tokencountingv1 "github.com/agynio/agn-cli/internal/tokencounting/token_countingv1"
)

const (
	DefaultAddress = "gateway.ziti:443"
	DefaultTimeout = 30 * time.Second
)

var DefaultModel = tokencountingv1.TokenCountingModel_TOKEN_COUNTING_MODEL_GPT_5

type Client struct {
	conn    *grpc.ClientConn
	model   tokencountingv1.TokenCountingModel
	timeout time.Duration
}

func New(address string, model tokencountingv1.TokenCountingModel, timeout time.Duration) (*Client, error) {
	trimmed := strings.TrimSpace(address)
	if trimmed == "" {
		return nil, errors.New("token counting address is required")
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	conn, err := grpc.NewClient(trimmed, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("connect token counting: %w", err)
	}
	return &Client{
		conn:    conn,
		model:   model,
		timeout: timeout,
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
	items, err := tokenCountingItems(msg)
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
	resp := &tokencountingv1.CountTokensResponse{}
	err = c.conn.Invoke(ctx, TokenCountingGatewayCountTokensMethod, &tokencountingv1.CountTokensRequest{
		Model:    c.model,
		Messages: payloads,
	}, resp)
	if err != nil {
		return 0, fmt.Errorf("count tokens: %w", err)
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

func ModelFromConfig(model string) (tokencountingv1.TokenCountingModel, error) {
	trimmed := strings.TrimSpace(model)
	if trimmed == "" {
		return DefaultModel, nil
	}
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "gpt-5") {
		return DefaultModel, nil
	}
	return 0, fmt.Errorf("unsupported token counting model: %q", trimmed)
}
