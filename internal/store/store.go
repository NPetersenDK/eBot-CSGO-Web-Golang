package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

type Store struct {
	db *sql.DB
}

// Open connects to MySQL. Every statement that writes created_at or updated_at
// must set them itself: Doctrine did that in PHP and the columns have no
// database default.
func Open(dsn string) (*Store, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)
	return &Store{db: db}, nil
}

// New wraps an existing handle, for tests.
func New(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Ping(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("mysql: %w", err)
	}
	return nil
}
