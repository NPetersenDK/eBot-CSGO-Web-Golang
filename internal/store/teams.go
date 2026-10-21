package store

import (
	"context"
	"errors"
)

func (s *Store) ListTeams(ctx context.Context) ([]Team, error) { return nil, errors.ErrUnsupported }

// ListTeamsBySeason lists the teams in a season (teams_in_seasons).
func (s *Store) ListTeamsBySeason(ctx context.Context, seasonID int64) ([]Team, error) {
	return nil, errors.ErrUnsupported
}
func (s *Store) GetTeam(ctx context.Context, id int64) (*Team, error) {
	return nil, errors.ErrUnsupported
}
func (s *Store) CreateTeam(ctx context.Context, in TeamInput) (int64, error) {
	return 0, errors.ErrUnsupported
}
func (s *Store) UpdateTeam(ctx context.Context, id int64, in TeamInput) error {
	return errors.ErrUnsupported
}
func (s *Store) DeleteTeam(ctx context.Context, id int64) error { return errors.ErrUnsupported }
