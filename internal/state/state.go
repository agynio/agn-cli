package state

import (
	"context"
	"time"

	"github.com/agynio/agn-cli/internal/message"
)

type MessageRecord struct {
	ID         string
	CreatedAt  time.Time
	TokenCount int
	Message    message.Message
}

type Thread struct {
	ID        string
	Messages  []MessageRecord
	UpdatedAt time.Time
}

type ThreadSummary struct {
	ID        string    `json:"thread_id"`
	Preview   string    `json:"preview"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Store interface {
	Load(ctx context.Context, threadID string) (Thread, error)
	Save(ctx context.Context, thread Thread) error
	List(ctx context.Context) ([]ThreadSummary, error)
}
