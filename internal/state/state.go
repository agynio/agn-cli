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

type Conversation struct {
	ID        string
	Messages  []MessageRecord
	UpdatedAt time.Time
}

type Store interface {
	Load(ctx context.Context, conversationID string) (Conversation, error)
	Save(ctx context.Context, conversation Conversation) error
}
