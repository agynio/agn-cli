package state

import (
	"context"
	"errors"
)

var ErrRemoteNotImplemented = errors.New("remote state store not implemented")

type RemoteStore struct{}

func NewRemoteStore() *RemoteStore {
	return &RemoteStore{}
}

func (s *RemoteStore) Load(ctx context.Context, conversationID string) (Conversation, error) {
	return Conversation{}, ErrRemoteNotImplemented
}

func (s *RemoteStore) Save(ctx context.Context, conversation Conversation) error {
	return ErrRemoteNotImplemented
}
