package store

import (
	"context"
	"database/sql"
)

const teamCols = `t.id, t.name, t.shorthandle, t.flag, t.link, t.created_at, t.updated_at`

func scanTeam(r rowScanner) (Team, error) {
	var t Team
	var link sql.NullString
	var c, u entityTime
	if err := r.Scan(&t.ID, &t.Name, &t.ShortName, &t.Flag, &link, &c, &u); err != nil {
		return t, err
	}
	t.Link, t.CreatedAt, t.UpdatedAt = link.String, c.Time, u.Time
	return t, nil
}

func (s *Store) queryTeams(ctx context.Context, q string, args ...any) ([]Team, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Team
	for rows.Next() {
		t, err := scanTeam(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) ListTeams(ctx context.Context) ([]Team, error) {
	return s.queryTeams(ctx, `SELECT `+teamCols+` FROM teams t ORDER BY t.name, t.id`)
}

// ListTeamsBySeason lists the teams in a season (teams_in_seasons).
func (s *Store) ListTeamsBySeason(ctx context.Context, seasonID int64) ([]Team, error) {
	return s.queryTeams(ctx, `SELECT DISTINCT `+teamCols+` FROM teams t JOIN teams_in_seasons tis ON tis.team_id = t.id WHERE tis.season_id = ? ORDER BY t.name, t.id`, seasonID)
}

func (s *Store) GetTeam(ctx context.Context, id int64) (*Team, error) {
	t, err := scanTeam(s.db.QueryRowContext(ctx, `SELECT `+teamCols+` FROM teams t WHERE t.id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT season_id FROM teams_in_seasons WHERE team_id = ? ORDER BY season_id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var sid int64
		if err := rows.Scan(&sid); err != nil {
			return nil, err
		}
		t.SeasonIDs = append(t.SeasonIDs, sid)
	}
	return &t, rows.Err()
}

func setTeamSeasons(ctx context.Context, tx *sql.Tx, teamID int64, seasonIDs []int64, now any) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM teams_in_seasons WHERE team_id = ?`, teamID); err != nil {
		return err
	}
	seen := map[int64]bool{}
	for _, sid := range seasonIDs {
		if seen[sid] {
			continue
		}
		seen[sid] = true
		if _, err := tx.ExecContext(ctx, `INSERT INTO teams_in_seasons (season_id, team_id, created_at, updated_at) VALUES (?, ?, ?, ?)`, sid, teamID, now, now); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) CreateTeam(ctx context.Context, in TeamInput) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	now := nowUTC()
	res, err := tx.ExecContext(ctx, `INSERT INTO teams (name, shorthandle, flag, link, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		in.Name, in.ShortName, in.Flag, nullStr(in.Link), now, now)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := setTeamSeasons(ctx, tx, id, in.SeasonIDs, now); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

func (s *Store) UpdateTeam(ctx context.Context, id int64, in TeamInput) error {
	if err := s.existsID(ctx, "teams", id); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := nowUTC()
	if _, err := tx.ExecContext(ctx, `UPDATE teams SET name = ?, shorthandle = ?, flag = ?, link = ?, updated_at = ? WHERE id = ?`,
		in.Name, in.ShortName, in.Flag, nullStr(in.Link), now, id); err != nil {
		return err
	}
	if err := setTeamSeasons(ctx, tx, id, in.SeasonIDs, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) DeleteTeam(ctx context.Context, id int64) error {
	if err := s.existsID(ctx, "teams", id); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM teams_in_seasons WHERE team_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM teams WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}
