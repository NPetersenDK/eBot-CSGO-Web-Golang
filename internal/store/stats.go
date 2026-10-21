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
