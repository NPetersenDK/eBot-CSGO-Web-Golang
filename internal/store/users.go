package store

import (
	"context"
	"errors"

	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/auth"
)

func (s *Store) CountUsers(ctx context.Context) (int, error) { return 0, errors.ErrUnsupported }
func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	return nil, errors.ErrUnsupported
}
func (s *Store) GetUser(ctx context.Context, id int64) (*User, error) {
	return nil, errors.ErrUnsupported
}
func (s *Store) CreateUser(ctx context.Context, in UserInput) (int64, error) {
	return 0, errors.ErrUnsupported
}
func (s *Store) UpdateUser(ctx context.Context, id int64, in UserInput) error {
	return errors.ErrUnsupported
}
func (s *Store) DeleteUser(ctx context.Context, id int64) error { return errors.ErrUnsupported }

// VerifyLocal implements auth.LocalUsers against sf_guard_user.
func (s *Store) VerifyLocal(ctx context.Context, username, password string) (auth.Session, error) {
	return auth.Session{}, errors.ErrUnsupported
}

// IsAdmin implements auth.RoleChecker.
func (s *Store) IsAdmin(ctx context.Context, username string) (bool, error) {
	return false, errors.ErrUnsupported
}

func (s *Store) TouchLastLogin(ctx context.Context, username string) error {
	return errors.ErrUnsupported
}
