package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/agynio/agn-cli/internal/message"
)

type LocalStore struct {
	basePath string
}

type persistedConversation struct {
	ID        string             `json:"id"`
	UpdatedAt time.Time          `json:"updated_at"`
	Messages  []persistedMessage `json:"messages"`
}

type persistedMessage struct {
	ID         string           `json:"id"`
	CreatedAt  time.Time        `json:"created_at"`
	TokenCount int              `json:"token_count"`
	Envelope   message.Envelope `json:"message"`
}

func DefaultLocalPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".agyn", "agn", "state"), nil
}

func NewLocalStore(basePath string) (*LocalStore, error) {
	if basePath == "" {
		return nil, errors.New("base path is required")
	}
	return &LocalStore{basePath: basePath}, nil
}

func NewDefaultLocalStore() (*LocalStore, error) {
	path, err := DefaultLocalPath()
	if err != nil {
		return nil, err
	}
	return NewLocalStore(path)
}

func (s *LocalStore) Load(ctx context.Context, conversationID string) (Conversation, error) {
	if conversationID == "" {
		return Conversation{}, errors.New("conversation ID is required")
	}
	select {
	case <-ctx.Done():
		return Conversation{}, ctx.Err()
	default:
	}

	path := filepath.Join(s.basePath, fmt.Sprintf("%s.json", conversationID))
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Conversation{ID: conversationID, Messages: []MessageRecord{}}, nil
		}
		return Conversation{}, fmt.Errorf("read conversation: %w", err)
	}

	var persisted persistedConversation
	if err := json.Unmarshal(data, &persisted); err != nil {
		return Conversation{}, fmt.Errorf("parse conversation: %w", err)
	}

	if persisted.ID == "" {
		persisted.ID = conversationID
	}
	if persisted.ID != conversationID {
		return Conversation{}, fmt.Errorf("conversation ID mismatch: %s", persisted.ID)
	}

	messages := make([]MessageRecord, 0, len(persisted.Messages))
	for _, item := range persisted.Messages {
		msg, err := message.Decode(item.Envelope)
		if err != nil {
			return Conversation{}, fmt.Errorf("decode message: %w", err)
		}
		if item.TokenCount <= 0 {
			return Conversation{}, errors.New("message token count missing")
		}
		messages = append(messages, MessageRecord{
			ID:         item.ID,
			CreatedAt:  item.CreatedAt,
			TokenCount: item.TokenCount,
			Message:    msg,
		})
	}

	return Conversation{
		ID:        persisted.ID,
		Messages:  messages,
		UpdatedAt: persisted.UpdatedAt,
	}, nil
}

func (s *LocalStore) Save(ctx context.Context, conversation Conversation) error {
	if conversation.ID == "" {
		return errors.New("conversation ID is required")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if err := os.MkdirAll(s.basePath, 0o755); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}

	persisted := persistedConversation{
		ID:        conversation.ID,
		UpdatedAt: time.Now().UTC(),
		Messages:  make([]persistedMessage, 0, len(conversation.Messages)),
	}

	for _, record := range conversation.Messages {
		env, err := message.Encode(record.Message)
		if err != nil {
			return fmt.Errorf("encode message: %w", err)
		}
		persisted.Messages = append(persisted.Messages, persistedMessage{
			ID:         record.ID,
			CreatedAt:  record.CreatedAt,
			TokenCount: record.TokenCount,
			Envelope:   env,
		})
	}

	data, err := json.MarshalIndent(persisted, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal conversation: %w", err)
	}

	path := filepath.Join(s.basePath, fmt.Sprintf("%s.json", conversation.ID))
	tmp, err := os.CreateTemp(s.basePath, fmt.Sprintf("%s.*.json", conversation.ID))
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("commit state file: %w", err)
	}

	return nil
}
