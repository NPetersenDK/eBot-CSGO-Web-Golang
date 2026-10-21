package store

import (
	"context"
	"database/sql"
)

const adSelect = `SELECT a.id, a.season_id, COALESCE(s.name, ''), a.message, a.active, a.created_at, a.updated_at FROM advertising a LEFT JOIN seasons s ON s.id = a.season_id`

func scanAd(r rowScanner) (Ad, error) {
	var ad Ad
	var sid sql.NullInt64
	var msg sql.NullString
	var c, u entityTime
	if err := r.Scan(&ad.ID, &sid, &ad.Season, &msg, &ad.Active, &c, &u); err != nil {
		return ad, err
	}
	ad.SeasonID, ad.Message, ad.CreatedAt, ad.UpdatedAt = sid.Int64, msg.String, c.Time, u.Time
	return ad, nil
}

func (s *Store) ListAds(ctx context.Context) ([]Ad, error) {
	rows, err := s.db.QueryContext(ctx, adSelect+` ORDER BY a.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Ad
	for rows.Next() {
		ad, err := scanAd(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ad)
	}
	return out, rows.Err()
}

func (s *Store) GetAd(ctx context.Context, id int64) (*Ad, error) {
	ad, err := scanAd(s.db.QueryRowContext(ctx, adSelect+` WHERE a.id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &ad, nil
}

func (s *Store) CreateAd(ctx context.Context, in AdInput) (int64, error) {
	now := nowUTC()
	res, err := s.db.ExecContext(ctx, `INSERT INTO advertising (season_id, message, active, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		in.SeasonID, in.Message, in.Active, now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateAd(ctx context.Context, id int64, in AdInput) error {
	res, err := s.db.ExecContext(ctx, `UPDATE advertising SET season_id = ?, message = ?, active = ?, updated_at = ? WHERE id = ?`,
		in.SeasonID, in.Message, in.Active, nowUTC(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return s.existsID(ctx, "advertising", id)
	}
	return nil
}

func (s *Store) DeleteAd(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM advertising WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeactivateAd(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE advertising SET active = 0, updated_at = ? WHERE id = ?`, nowUTC(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return s.existsID(ctx, "advertising", id)
	}
	return nil
}
