// Package store keeps snapshots: one JSON document per id, nothing else.
package store

import (
	"context"
	"errors"
	"sync"
)

// ErrNotFound is what Get returns for an id the store doesn't hold.
var ErrNotFound = errors.New("snapshot not found")

// Store holds each snapshot's stored record, as JSON, under its id. Expiry is the
// server's to judge from the record; the Azure store's lifecycle rule is only the cleanup.
type Store interface {
	Put(ctx context.Context, id string, record []byte) error
	Get(ctx context.Context, id string) ([]byte, error)
}

// Memory is a Store in the process's memory, for tests, the image's self-test and
// running locally.
type Memory struct {
	mu      sync.Mutex
	records map[string][]byte
}

func NewMemory() *Memory { return &Memory{records: map[string][]byte{}} }

func (m *Memory) Put(_ context.Context, id string, record []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records[id] = append([]byte(nil), record...)
	return nil
}

func (m *Memory) Get(_ context.Context, id string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	record, ok := m.records[id]
	if !ok {
		return nil, ErrNotFound
	}
	return append([]byte(nil), record...), nil
}
