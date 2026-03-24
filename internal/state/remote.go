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

func (s *RemoteStore) Load(ctx context.Context, threadID string) (Thread, error) {
	return Thread{}, ErrRemoteNotImplemented
}

func (s *RemoteStore) Save(ctx context.Context, thread Thread) error {
	return ErrRemoteNotImplemented
}

func (s *RemoteStore) List(ctx context.Context) ([]ThreadSummary, error) {
	return nil, ErrRemoteNotImplemented
}
