package store

import (
	"context"
	"errors"
)

func (s *Store) ListSeasons(ctx context.Context) ([]Season, error) {
	return nil, errors.ErrUnsupported
}
func (s *Store) GetSeason(ctx context.Context, id int64) (*Season, error) {
	return nil, errors.ErrUnsupported
}
func (s *Store) CreateSeason(ctx context.Context, in SeasonInput) (int64, error) {
	return 0, errors.ErrUnsupported
}
func (s *Store) UpdateSeason(ctx context.Context, id int64, in SeasonInput) error {
	return errors.ErrUnsupported
}
func (s *Store) DeleteSeason(ctx context.Context, id int64) error { return errors.ErrUnsupported }
func (s *Store) DeactivateSeason(ctx context.Context, id int64) error {
	return errors.ErrUnsupported
}
