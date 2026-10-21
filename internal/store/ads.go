package store

import (
	"context"
	"errors"
)

func (s *Store) ListAds(ctx context.Context) ([]Ad, error) { return nil, errors.ErrUnsupported }
func (s *Store) GetAd(ctx context.Context, id int64) (*Ad, error) {
	return nil, errors.ErrUnsupported
}
func (s *Store) CreateAd(ctx context.Context, in AdInput) (int64, error) {
	return 0, errors.ErrUnsupported
}
func (s *Store) UpdateAd(ctx context.Context, id int64, in AdInput) error {
	return errors.ErrUnsupported
}
func (s *Store) DeleteAd(ctx context.Context, id int64) error     { return errors.ErrUnsupported }
func (s *Store) DeactivateAd(ctx context.Context, id int64) error { return errors.ErrUnsupported }
