package store

import (
	"context"
	"database/sql"
	"errors"
)

// mapDest scans the 13 map columns of mapColumns. The id is nullable so the
// same scan works for a LEFT JOIN.
type mapDest struct {
	id      sql.NullInt64
	m       Map
	created dbTime
	updated dbTime
}

func (d *mapDest) dests() []any {
	return []any{&d.id, &d.m.MatchID, &d.m.Name, &d.m.Score1, &d.m.Score2, &d.m.CurrentSide,
		&d.m.Status, &d.m.PickedBy, &d.m.Overtimes, &d.m.IdentifierID, &d.m.TVRecordFile,
		&d.created, &d.updated}
}

func (d *mapDest) result() *Map {
	if !d.id.Valid {
		return nil
	}
	m := d.m
	m.ID = d.id.Int64
	m.CreatedAt, m.UpdatedAt = d.created.T, d.updated.T
	return &m
}

const mapColumns = `id, COALESCE(match_id, 0), COALESCE(map_name, ''),
	COALESCE(score_1, 0), COALESCE(score_2, 0), COALESCE(current_side, ''),
	COALESCE(status, 0), COALESCE(maps_for, ''), COALESCE(nb_ot, 0),
	COALESCE(CAST(identifier_id AS CHAR), ''), COALESCE(tv_record_file, ''),
	created_at, updated_at`

func scanMap(sc scanner) (*Map, error) {
	var d mapDest
	if err := sc.Scan(d.dests()...); err != nil {
		return nil, err
	}
	return d.result(), nil
}

func (s *Store) ListMaps(ctx context.Context, matchID int64) ([]Map, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+mapColumns+" FROM maps WHERE match_id = ? ORDER BY id ASC", matchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Map{}
	for rows.Next() {
		m, err := scanMap(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

func (s *Store) GetMap(ctx context.Context, id int64) (*Map, error) {
	m, err := scanMap(s.db.QueryRowContext(ctx, "SELECT "+mapColumns+" FROM maps WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return m, err
}

const mapScoreColumns = `id, map_id, COALESCE(type_score, 'normal'),
	COALESCE(score1_side1, 0), COALESCE(score1_side2, 0),
	COALESCE(score2_side1, 0), COALESCE(score2_side2, 0)`

func scanMapScore(sc scanner) (*MapScore, error) {
	var ms MapScore
	err := sc.Scan(&ms.ID, &ms.MapID, &ms.Type, &ms.Score1Side1, &ms.Score1Side2, &ms.Score2Side1, &ms.Score2Side2)
	if err != nil {
		return nil, err
	}
	return &ms, nil
}

func (s *Store) ListMapScores(ctx context.Context, mapID int64) ([]MapScore, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+mapScoreColumns+" FROM maps_score WHERE map_id = ? ORDER BY id ASC", mapID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MapScore{}
	for rows.Next() {
		ms, err := scanMapScore(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *ms)
	}
	return out, rows.Err()
}

func (s *Store) GetMapScore(ctx context.Context, id int64) (*MapScore, error) {
	ms, err := scanMapScore(s.db.QueryRowContext(ctx, "SELECT "+mapScoreColumns+" FROM maps_score WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return ms, err
}

// mapSums adds up the per-side scores of a map.
func mapSums(ctx context.Context, q execer, mapID int64) (a, b int, err error) {
	err = q.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(COALESCE(score1_side1, 0) + COALESCE(score1_side2, 0)), 0),
		        COALESCE(SUM(COALESCE(score2_side1, 0) + COALESCE(score2_side2, 0)), 0)
		 FROM maps_score WHERE map_id = ?`, mapID).Scan(&a, &b)
	return a, b, err
}

// UpdateMapScore stores the per-side scores and recomputes the map and match
// scores. Like the PHP edit score action, the match score is the sum over the
// match's current map, and a running match cannot be edited.
func (s *Store) UpdateMapScore(ctx context.Context, id int64, in MapScoreInput) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		var mapID, matchID int64
		err := tx.QueryRowContext(ctx,
			"SELECT ms.map_id, mp.match_id FROM maps_score ms JOIN maps mp ON mp.id = ms.map_id WHERE ms.id = ?", id).
			Scan(&mapID, &matchID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		enable, _, err := lockMatch(ctx, tx, matchID)
		if err != nil {
			return err
		}
		if enable {
			return ErrMatchEnabled
		}
		t := now()
		if _, err := tx.ExecContext(ctx,
			`UPDATE maps_score SET score1_side1 = ?, score1_side2 = ?, score2_side1 = ?, score2_side2 = ?, updated_at = ?
			 WHERE id = ?`, in.Score1Side1, in.Score1Side2, in.Score2Side1, in.Score2Side2, t, id); err != nil {
			return err
		}

		current := mapID
		var cur sql.NullInt64
		if err := tx.QueryRowContext(ctx, "SELECT current_map FROM matchs WHERE id = ?", matchID).Scan(&cur); err != nil {
			return err
		}
		if cur.Valid {
			current = cur.Int64
		}
		toUpdate := []int64{mapID}
		if current != mapID {
			toUpdate = append(toUpdate, current)
		}
		var scoreA, scoreB int
		for _, m := range toUpdate {
			a, b, err := mapSums(ctx, tx, m)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "UPDATE maps SET score_1 = ?, score_2 = ?, updated_at = ? WHERE id = ?",
				a, b, t, m); err != nil {
				return err
			}
			if m == current {
				scoreA, scoreB = a, b
			}
		}
		_, err = tx.ExecContext(ctx, "UPDATE matchs SET score_a = ?, score_b = ?, updated_at = ? WHERE id = ?",
			scoreA, scoreB, t, matchID)
		return err
	})
}

const playerColumns = `id, COALESCE(match_id, 0), COALESCE(map_id, 0), COALESCE(player_key, ''),
	COALESCE(team, 'other'), COALESCE(ip, ''), COALESCE(steamid, ''),
	COALESCE(first_side, ''), COALESCE(current_side, ''), COALESCE(pseudo, ''),
	COALESCE(nb_kill, 0), COALESCE(assist, 0), COALESCE(death, 0), COALESCE(point, 0),
	COALESCE(hs, 0), COALESCE(defuse, 0), COALESCE(bombe, 0), COALESCE(tk, 0),
	COALESCE(nb1, 0), COALESCE(nb2, 0), COALESCE(nb3, 0), COALESCE(nb4, 0), COALESCE(nb5, 0),
	COALESCE(nb1kill, 0), COALESCE(nb2kill, 0), COALESCE(nb3kill, 0), COALESCE(nb4kill, 0), COALESCE(nb5kill, 0),
	COALESCE(firstkill, 0), COALESCE(pluskill, 0)`

// ListPlayers returns the players of a match. A zero mapID means all maps.
func (s *Store) ListPlayers(ctx context.Context, matchID, mapID int64) ([]Player, error) {
	q := "SELECT " + playerColumns + " FROM players WHERE match_id = ?"
	args := []any{matchID}
	if mapID != 0 {
		q += " AND map_id = ?"
		args = append(args, mapID)
	}
	q += " ORDER BY point DESC, nb_kill DESC, id ASC"
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPlayers(rows)
}

func scanPlayers(rows *sql.Rows) ([]Player, error) {
	out := []Player{}
	for rows.Next() {
		var p Player
		if err := rows.Scan(&p.ID, &p.MatchID, &p.MapID, &p.Key, &p.Team, &p.IP, &p.SteamID,
			&p.FirstSide, &p.CurrentSide, &p.Nickname,
			&p.Kills, &p.Assists, &p.Deaths, &p.Points, &p.Headshots, &p.Defuses, &p.Bombs, &p.TeamKills,
			&p.Clutch1v1, &p.Clutch1v2, &p.Clutch1v3, &p.Clutch1v4, &p.Clutch1v5,
			&p.Rounds1K, &p.Rounds2K, &p.Rounds3K, &p.Rounds4K, &p.Rounds5K,
			&p.FirstKills, &p.PlusKills); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ListRoundSummaries returns the rounds of a match in play order. A zero mapID means all maps.
func (s *Store) ListRoundSummaries(ctx context.Context, matchID, mapID int64) ([]RoundSummary, error) {
	q := `SELECT id, COALESCE(match_id, 0), COALESCE(map_id, 0), COALESCE(round_id, 0),
	             COALESCE(bomb_planted, 0), COALESCE(bomb_defused, 0), COALESCE(bomb_exploded, 0),
	             COALESCE(win_type, ''), COALESCE(team_win, ''), COALESCE(ct_win, 0), COALESCE(t_win, 0),
	             COALESCE(score_a, 0), COALESCE(score_b, 0), best_killer, COALESCE(best_killer_nb, 0),
	             COALESCE(best_killer_fk, 0), COALESCE(backup_file_name, '')
	      FROM round_summary WHERE match_id = ?`
	args := []any{matchID}
	if mapID != 0 {
		q += " AND map_id = ?"
		args = append(args, mapID)
	}
	q += " ORDER BY map_id ASC, round_id ASC, id ASC"
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RoundSummary{}
	for rows.Next() {
		var r RoundSummary
		var killer sql.NullInt64
		if err := rows.Scan(&r.ID, &r.MatchID, &r.MapID, &r.RoundID,
			&r.BombPlanted, &r.BombDefused, &r.BombExploded,
			&r.WinType, &r.TeamWin, &r.CTWin, &r.TWin,
			&r.ScoreA, &r.ScoreB, &killer, &r.BestKillerNb,
			&r.BestKillerFK, &r.BackupFileName); err != nil {
			return nil, err
		}
		r.BestKillerID = nullPtr(killer)
		out = append(out, r)
	}
	return out, rows.Err()
}
