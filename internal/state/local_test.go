package state

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/agynio/agn-cli/internal/message"
)

func TestNewLocalStoreValidation(t *testing.T) {
	store, err := NewLocalStore("")
	require.Error(t, err)
	require.Nil(t, store)
}

func TestLocalStoreSaveLoadRoundTrip(t *testing.T) {
	store := newLocalStore(t)
	ctx := context.Background()
	createdAt := time.Date(2024, 10, 12, 8, 0, 0, 0, time.UTC)
	conversation := Conversation{
		ID: "conv-1",
		Messages: []MessageRecord{
			{
				ID:         "msg-1",
				CreatedAt:  createdAt,
				TokenCount: 4,
				Message:    message.NewHumanMessage("hello"),
			},
			{
				ID:         "msg-2",
				CreatedAt:  createdAt.Add(time.Minute),
				TokenCount: 3,
				Message:    message.NewAIMessage("world"),
			},
		},
	}
	before := time.Now().UTC()
	require.NoError(t, store.Save(ctx, conversation))

	loaded, err := store.Load(ctx, conversation.ID)
	require.NoError(t, err)
	require.Equal(t, conversation.ID, loaded.ID)
	require.Equal(t, conversation.Messages, loaded.Messages)
	require.False(t, loaded.UpdatedAt.IsZero())
	require.True(t, loaded.UpdatedAt.Equal(before) || loaded.UpdatedAt.After(before))
}

func TestLocalStoreLoadMissing(t *testing.T) {
	store := newLocalStore(t)
	ctx := context.Background()
	conv, err := store.Load(ctx, "missing")
	require.NoError(t, err)
	require.Equal(t, "missing", conv.ID)
	require.Empty(t, conv.Messages)
}

func TestLocalStoreLoadSaveEmptyID(t *testing.T) {
	store := newLocalStore(t)
	ctx := context.Background()
	_, err := store.Load(ctx, "")
	require.Error(t, err)
	returnErr := store.Save(ctx, Conversation{})
	require.Error(t, returnErr)
}

func TestLocalStoreLoadMismatchedID(t *testing.T) {
	store := newLocalStore(t)
	ctx := context.Background()
	writePersistedConversation(t, store.basePath, "expected", persistedConversation{
		ID:        "other",
		UpdatedAt: time.Now().UTC(),
		Messages:  []persistedMessage{persistedMessageFixture(t)},
	})

	_, err := store.Load(ctx, "expected")
	require.Error(t, err)
}

func TestLocalStoreLoadZeroTokenCount(t *testing.T) {
	store := newLocalStore(t)
	ctx := context.Background()
	msg := persistedMessageFixture(t)
	msg.TokenCount = 0
	writePersistedConversation(t, store.basePath, "conv-1", persistedConversation{
		ID:        "conv-1",
		UpdatedAt: time.Now().UTC(),
		Messages:  []persistedMessage{msg},
	})

	_, err := store.Load(ctx, "conv-1")
	require.Error(t, err)
}

func TestLocalStoreLoadCorruptJSON(t *testing.T) {
	store := newLocalStore(t)
	path := filepath.Join(store.basePath, "conv-1.json")
	require.NoError(t, os.MkdirAll(store.basePath, 0o755))
	require.NoError(t, os.WriteFile(path, []byte("not-json"), 0o644))

	_, err := store.Load(context.Background(), "conv-1")
	require.Error(t, err)
}

func TestLocalStoreMultipleSaveLoadCycles(t *testing.T) {
	store := newLocalStore(t)
	ctx := context.Background()
	conversation := Conversation{
		ID: "conv-1",
		Messages: []MessageRecord{
			{
				ID:         "msg-1",
				CreatedAt:  time.Date(2024, 10, 12, 8, 0, 0, 0, time.UTC),
				TokenCount: 2,
				Message:    message.NewHumanMessage("first"),
			},
		},
	}

	require.NoError(t, store.Save(ctx, conversation))
	loaded, err := store.Load(ctx, conversation.ID)
	require.NoError(t, err)
	require.Equal(t, conversation.Messages, loaded.Messages)

	require.NoError(t, store.Save(ctx, loaded))
	loadedAgain, err := store.Load(ctx, conversation.ID)
	require.NoError(t, err)
	require.Equal(t, loaded.Messages, loadedAgain.Messages)
}

func newLocalStore(t *testing.T) *LocalStore {
	t.Helper()
	store, err := NewLocalStore(t.TempDir())
	require.NoError(t, err)
	return store
}

func writePersistedConversation(t *testing.T, basePath, conversationID string, conversation persistedConversation) {
	t.Helper()
	require.NoError(t, os.MkdirAll(basePath, 0o755))
	data, err := json.Marshal(conversation)
	require.NoError(t, err)
	path := filepath.Join(basePath, conversationID+".json")
	require.NoError(t, os.WriteFile(path, data, 0o644))
}

func persistedMessageFixture(t *testing.T) persistedMessage {
	t.Helper()
	env, err := message.Encode(message.NewHumanMessage("hello"))
	require.NoError(t, err)
	return persistedMessage{
		ID:         "msg-1",
		CreatedAt:  time.Date(2024, 10, 12, 8, 0, 0, 0, time.UTC),
		TokenCount: 1,
		Envelope:   env,
	}
}
