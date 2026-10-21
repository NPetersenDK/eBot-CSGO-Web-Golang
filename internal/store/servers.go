package store

import (
	"context"
	"errors"
)

func (s *Store) ListServers(ctx context.Context) ([]Server, error) {
	return nil, errors.ErrUnsupported
}

// FreeServers lists servers without an enabled match.
func (s *Store) FreeServers(ctx context.Context) ([]Server, error) {
	return nil, errors.ErrUnsupported
}
func (s *Store) GetServer(ctx context.Context, id int64) (*Server, error) {
	return nil, errors.ErrUnsupported
}

// CreateServers returns the number of servers created.
func (s *Store) CreateServers(ctx context.Context, in ServerCreate) (int, error) {
	return 0, errors.ErrUnsupported
}
func (s *Store) UpdateServer(ctx context.Context, id int64, in ServerInput) error {
	return errors.ErrUnsupported
}
func (s *Store) DeleteServer(ctx context.Context, id int64) error { return errors.ErrUnsupported }
