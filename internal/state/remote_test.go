package state

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRemoteStoreNotImplemented(t *testing.T) {
	store := NewRemoteStore()
	_, err := store.Load(context.Background(), "conv-1")
	require.ErrorIs(t, err, ErrRemoteNotImplemented)

	err = store.Save(context.Background(), Thread{ID: "conv-1"})
	require.ErrorIs(t, err, ErrRemoteNotImplemented)
}
