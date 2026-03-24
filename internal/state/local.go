package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/agynio/agn-cli/internal/message"
)

const previewMaxRunes = 120

type LocalStore struct {
	basePath   string
	legacyPath string
}

type persistedThread struct {
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
	return filepath.Join(home, ".agyn", "agn", "threads"), nil
}

func legacyLocalPath() (string, error) {
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
	legacyPath, err := legacyLocalPath()
	if err != nil {
		return nil, err
	}
	store, err := NewLocalStore(path)
	if err != nil {
		return nil, err
	}
	store.legacyPath = legacyPath
	return store, nil
}

func (s *LocalStore) Load(ctx context.Context, threadID string) (Thread, error) {
	if threadID == "" {
		return Thread{}, errors.New("thread ID is required")
	}
	select {
	case <-ctx.Done():
		return Thread{}, ctx.Err()
	default:
	}

	data, err := readThreadFile(s.basePath, threadID)
	if err != nil {
		return Thread{}, err
	}
	if data == nil && s.legacyPath != "" {
		data, err = readThreadFile(s.legacyPath, threadID)
		if err != nil {
			return Thread{}, err
		}
	}
	if data == nil {
		return Thread{ID: threadID, Messages: []MessageRecord{}}, nil
	}

	thread, err := decodeThread(data, threadID)
	if err != nil {
		return Thread{}, err
	}
	return thread, nil
}

func (s *LocalStore) Save(ctx context.Context, thread Thread) error {
	if thread.ID == "" {
		return errors.New("thread ID is required")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if err := os.MkdirAll(s.basePath, 0o755); err != nil {
		return fmt.Errorf("create threads directory: %w", err)
	}

	persisted := persistedThread{
		ID:        thread.ID,
		UpdatedAt: time.Now().UTC(),
		Messages:  make([]persistedMessage, 0, len(thread.Messages)),
	}

	for _, record := range thread.Messages {
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
		return fmt.Errorf("marshal thread: %w", err)
	}

	path := filepath.Join(s.basePath, fmt.Sprintf("%s.json", thread.ID))
	tmp, err := os.CreateTemp(s.basePath, fmt.Sprintf("%s.*.json", thread.ID))
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
		return fmt.Errorf("commit thread file: %w", err)
	}

	return nil
}

func (s *LocalStore) List(ctx context.Context) ([]ThreadSummary, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	canonical, err := listThreads(ctx, s.basePath)
	if err != nil {
		return nil, err
	}
	combined := make(map[string]ThreadSummary, len(canonical))
	for _, summary := range canonical {
		combined[summary.ID] = summary
	}
	if s.legacyPath != "" {
		legacy, err := listThreads(ctx, s.legacyPath)
		if err != nil {
			return nil, err
		}
		for _, summary := range legacy {
			if _, ok := combined[summary.ID]; ok {
				continue
			}
			combined[summary.ID] = summary
		}
	}

	result := make([]ThreadSummary, 0, len(combined))
	for _, summary := range combined {
		result = append(result, summary)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].UpdatedAt.Equal(result[j].UpdatedAt) {
			return result[i].ID < result[j].ID
		}
		return result[i].UpdatedAt.After(result[j].UpdatedAt)
	})
	return result, nil
}

func readThreadFile(basePath, threadID string) ([]byte, error) {
	path := filepath.Join(basePath, fmt.Sprintf("%s.json", threadID))
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read thread: %w", err)
	}
	return data, nil
}

func decodeThread(data []byte, threadID string) (Thread, error) {
	var persisted persistedThread
	if err := json.Unmarshal(data, &persisted); err != nil {
		return Thread{}, fmt.Errorf("parse thread: %w", err)
	}

	if persisted.ID == "" {
		persisted.ID = threadID
	}
	if persisted.ID != threadID {
		return Thread{}, fmt.Errorf("thread ID mismatch: %s", persisted.ID)
	}

	messages := make([]MessageRecord, 0, len(persisted.Messages))
	for _, item := range persisted.Messages {
		msg, err := message.Decode(item.Envelope)
		if err != nil {
			return Thread{}, fmt.Errorf("decode message: %w", err)
		}
		if item.TokenCount <= 0 {
			return Thread{}, errors.New("message token count missing")
		}
		messages = append(messages, MessageRecord{
			ID:         item.ID,
			CreatedAt:  item.CreatedAt,
			TokenCount: item.TokenCount,
			Message:    msg,
		})
	}

	return Thread{
		ID:        persisted.ID,
		Messages:  messages,
		UpdatedAt: persisted.UpdatedAt,
	}, nil
}

func listThreads(ctx context.Context, basePath string) ([]ThreadSummary, error) {
	entries, err := os.ReadDir(basePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read threads directory: %w", err)
	}
	result := make([]ThreadSummary, 0, len(entries))
	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(basePath, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read thread: %w", err)
		}
		var persisted persistedThread
		if err := json.Unmarshal(data, &persisted); err != nil {
			return nil, fmt.Errorf("parse thread: %w", err)
		}
		id := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		if persisted.ID == "" {
			persisted.ID = id
		}
		if persisted.ID != id {
			return nil, fmt.Errorf("thread ID mismatch: %s", persisted.ID)
		}
		summary, err := summarizeThread(persisted)
		if err != nil {
			return nil, err
		}
		result = append(result, summary)
	}
	return result, nil
}

func summarizeThread(persisted persistedThread) (ThreadSummary, error) {
	preview := ""
	for _, item := range persisted.Messages {
		msg, err := message.Decode(item.Envelope)
		if err != nil {
			return ThreadSummary{}, fmt.Errorf("decode message: %w", err)
		}
		human, ok := msg.(message.HumanMessage)
		if ok {
			preview = strings.TrimSpace(human.Text)
			break
		}
	}
	preview = truncatePreview(preview, previewMaxRunes)
	return ThreadSummary{ID: persisted.ID, Preview: preview, UpdatedAt: persisted.UpdatedAt}, nil
}

func truncatePreview(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}
