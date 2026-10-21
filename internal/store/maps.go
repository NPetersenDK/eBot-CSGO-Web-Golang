package store

import (
	"context"
	"errors"
)

func (s *Store) ListMaps(ctx context.Context, matchID int64) ([]Map, error) {
	return nil, errors.ErrUnsupported
}
func (s *Store) GetMap(ctx context.Context, id int64) (*Map, error) {
	return nil, errors.ErrUnsupported
}
func (s *Store) ListMapScores(ctx context.Context, mapID int64) ([]MapScore, error) {
	return nil, errors.ErrUnsupported
}
func (s *Store) GetMapScore(ctx context.Context, id int64) (*MapScore, error) {
	return nil, errors.ErrUnsupported
}

// UpdateMapScore stores the per-side scores and recomputes the map and match scores.
func (s *Store) UpdateMapScore(ctx context.Context, id int64, in MapScoreInput) error {
	return errors.ErrUnsupported
}

// ListPlayers returns the players of a match. A zero mapID means all maps.
func (s *Store) ListPlayers(ctx context.Context, matchID, mapID int64) ([]Player, error) {
	return nil, errors.ErrUnsupported
}
func (s *Store) ListRoundSummaries(ctx context.Context, matchID, mapID int64) ([]RoundSummary, error) {
	return nil, errors.ErrUnsupported
}
