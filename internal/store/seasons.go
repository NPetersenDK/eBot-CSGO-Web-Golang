package store

import (
	"context"
	"database/sql"
)

const seasonCols = `id, name, event, DATE_FORMAT(start, '%Y-%m-%d %H:%i:%s'), DATE_FORMAT(end, '%Y-%m-%d %H:%i:%s'), link, logo, active, created_at, updated_at`

type rowScanner interface{ Scan(dest ...any) error }

func scanSeason(r rowScanner) (Season, error) {
	var se Season
	var start, end, link, logo sql.NullString
	var c, u entityTime
	if err := r.Scan(&se.ID, &se.Name, &se.Event, &start, &end, &link, &logo, &se.Active, &c, &u); err != nil {
		return se, err
	}
	se.Start, se.End, se.Link, se.Logo = start.String, end.String, link.String, logo.String
	se.CreatedAt, se.UpdatedAt = c.Time, u.Time
	return se, nil
}

func (s *Store) ListSeasons(ctx context.Context) ([]Season, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+seasonCols+` FROM seasons ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Season
	for rows.Next() {
		se, err := scanSeason(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, se)
	}
	return out, rows.Err()
}

func (s *Store) GetSeason(ctx context.Context, id int64) (*Season, error) {
	se, err := scanSeason(s.db.QueryRowContext(ctx, `SELECT `+seasonCols+` FROM seasons WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &se, nil
}

func (s *Store) CreateSeason(ctx context.Context, in SeasonInput) (int64, error) {
	now := nowUTC()
	res, err := s.db.ExecContext(ctx, `INSERT INTO seasons (name, event, start, end, link, logo, active, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.Name, in.Event, in.Start, in.End, nullStr(in.Link), nullStr(in.Logo), in.Active, now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateSeason(ctx context.Context, id int64, in SeasonInput) error {
	res, err := s.db.ExecContext(ctx, `UPDATE seasons SET name = ?, event = ?, start = ?, end = ?, link = ?, logo = ?, active = ?, updated_at = ? WHERE id = ?`,
		in.Name, in.Event, in.Start, in.End, nullStr(in.Link), nullStr(in.Logo), in.Active, nowUTC(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return s.existsID(ctx, "seasons", id)
	}
	return nil
}

func (s *Store) DeleteSeason(ctx context.Context, id int64) error {
	if err := s.existsID(ctx, "seasons", id); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`DELETE FROM teams_in_seasons WHERE season_id = ?`,
		`DELETE FROM advertising WHERE season_id = ?`,
		`DELETE FROM seasons WHERE id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, q, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) DeactivateSeason(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE seasons SET active = 0, updated_at = ? WHERE id = ?`, nowUTC(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return s.existsID(ctx, "seasons", id)
	}
	return nil
}
