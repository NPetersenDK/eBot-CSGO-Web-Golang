package store

import (
	"context"
	"errors"
)

func (s *Store) ListMatches(ctx context.Context, f MatchFilter) ([]Match, Page, error) {
	return nil, Page{}, errors.ErrUnsupported
}
func (s *Store) GetMatch(ctx context.Context, id int64) (*Match, error) {
	return nil, errors.ErrUnsupported
}
func (s *Store) CreateMatch(ctx context.Context, in MatchInput) (int64, error) {
	return 0, errors.ErrUnsupported
}
func (s *Store) UpdateMatch(ctx context.Context, id int64, in MatchInput) error {
	return errors.ErrUnsupported
}
func (s *Store) DeleteMatch(ctx context.Context, id int64) error { return errors.ErrUnsupported }

// StartMatch assigns the server and enables the match: ip, server_id, enable=1,
// status=1, scores zeroed, config_authkey generated when empty.
func (s *Store) StartMatch(ctx context.Context, id, serverID int64) error {
	return errors.ErrUnsupported
}

// AssignServer sets the server without starting the match.
func (s *Store) AssignServer(ctx context.Context, id, serverID int64) error {
	return errors.ErrUnsupported
}
func (s *Store) RetryMatch(ctx context.Context, id int64) error   { return errors.ErrUnsupported }
func (s *Store) ResetMatch(ctx context.Context, id int64) error   { return errors.ErrUnsupported }
func (s *Store) ArchiveMatch(ctx context.Context, id int64) error { return errors.ErrUnsupported }
func (s *Store) DuplicateMatch(ctx context.Context, id int64) (int64, error) {
	return 0, errors.ErrUnsupported
}
