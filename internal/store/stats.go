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
