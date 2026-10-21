package store

import "context"

// Counts returns the numbers shown on the dashboard. Matches counts every
// match that is not archived.
func (s *Store) Counts(ctx context.Context) (Counts, error) {
	var c Counts
	err := s.db.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM matchs WHERE status < ?),
		(SELECT COUNT(*) FROM matchs WHERE status BETWEEN ? AND ? AND enable = 1),
		(SELECT COUNT(*) FROM matchs WHERE status = ?),
		(SELECT COUNT(*) FROM servers),
		(SELECT COUNT(*) FROM teams),
		(SELECT COUNT(*) FROM seasons),
		(SELECT COUNT(*) FROM sf_guard_user)`,
		StatusArchived, StatusStarting, StatusOTSecond, StatusArchived).
		Scan(&c.Matches, &c.Running, &c.Archived, &c.Servers, &c.Teams, &c.Seasons, &c.Users)
	return c, err
}

// GlobalStats returns the totals shown on the public statistics page, with the
// same meaning as the PHP panel: in progress is status 1 to 12 with enable set,
// finished is every status from 13 up.
func (s *Store) GlobalStats(ctx context.Context) (GlobalStats, error) {
	var g GlobalStats
	err := s.db.QueryRowContext(ctx, `SELECT
		(SELECT COALESCE(SUM(nb_kill), 0) FROM players),
		(SELECT COALESCE(SUM(hs), 0) FROM players),
		(SELECT COUNT(*) FROM matchs WHERE status BETWEEN ? AND ? AND enable = 1),
		(SELECT COUNT(*) FROM matchs WHERE status = ?),
		(SELECT COUNT(*) FROM matchs WHERE status >= ?),
		(SELECT COUNT(*) FROM servers)`,
		StatusStarting, StatusOTSecond, StatusNotStarted, StatusEndMatch).
		Scan(&g.Kills, &g.Headshots, &g.InProgress, &g.NotStarted, &g.Finished, &g.Servers)
	return g, err
}

// ListPlayerStats sums every player over all their maps, most kills first.
// Spectators (team "other") are left out. seasonID 0 means all seasons and
// limit 0 means no limit. The name is the one the player used last.
func (s *Store) ListPlayerStats(ctx context.Context, seasonID int64, limit int) ([]PlayerStat, error) {
	q := `SELECT COALESCE(p.steamid, ''),
		SUBSTRING_INDEX(GROUP_CONCAT(COALESCE(p.pseudo, '') ORDER BY p.id DESC SEPARATOR '\n'), '\n', 1),
		COALESCE(SUM(p.nb_kill), 0), COALESCE(SUM(p.assist), 0), COALESCE(SUM(p.death), 0),
		COALESCE(SUM(p.hs), 0), COALESCE(SUM(p.firstkill), 0), COALESCE(SUM(p.tk), 0),
		COALESCE(SUM(p.bombe), 0), COALESCE(SUM(p.defuse), 0), COALESCE(SUM(p.point), 0),
		COALESCE(SUM(p.nb1 + 2 * p.nb2 + 3 * p.nb3 + 4 * p.nb4 + 5 * p.nb5), 0),
		COUNT(*)
		FROM players p`
	var args []any
	if seasonID != 0 {
		q += " JOIN matchs m ON m.id = p.match_id AND m.season_id = ?"
		args = append(args, seasonID)
	}
	q += ` WHERE COALESCE(p.team, 'other') <> 'other'
		GROUP BY COALESCE(p.steamid, '')
		ORDER BY SUM(p.nb_kill) DESC, COALESCE(p.steamid, '') ASC`
	if limit > 0 {
		q += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PlayerStat{}
	for rows.Next() {
		var p PlayerStat
		if err := rows.Scan(&p.SteamID, &p.Name, &p.Kills, &p.Assists, &p.Deaths, &p.Headshots, &p.FirstKills,
			&p.TeamKills, &p.Bombs, &p.Defuses, &p.Points, &p.Clutch, &p.Matches); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
