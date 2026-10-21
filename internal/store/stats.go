package store

import (
	"context"
	"errors"
)

func (s *Store) Counts(ctx context.Context) (Counts, error) { return Counts{}, errors.ErrUnsupported }
