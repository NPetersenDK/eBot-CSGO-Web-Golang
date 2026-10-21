package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	defaultPage    = 1
	defaultPerPage = 12
)

// execer is the part of *sql.DB and *sql.Tx the helpers need.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type scanner interface {
	Scan(dest ...any) error
}

// dbTime scans DATETIME columns whether or not the DSN has parseTime=true.
type dbTime struct {
	T     time.Time
	Valid bool
}

func (t *dbTime) Scan(v any) error {
	t.T, t.Valid = time.Time{}, false
	var raw string
	switch x := v.(type) {
	case nil:
		return nil
	case time.Time:
		t.T, t.Valid = x.UTC(), !x.IsZero()
		return nil
	case []byte:
		raw = string(x)
	case string:
		raw = x
	default:
		return fmt.Errorf("store: cannot scan %T into a time", v)
	}
	parsed, err := time.ParseInLocation("2006-01-02 15:04:05", raw, time.UTC)
	if err != nil {
		return nil
	}
	t.T, t.Valid = parsed, true
	return nil
}

func (t dbTime) ptr() *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.T
	return &v
}

func nullPtr(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
	return &v
}

func ptrArg(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

func strArg(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func now() time.Time { return time.Now().UTC() }

// newAuthKey returns a random, practically unique key for matchs.config_authkey.
// startSide returns side when valid, or a random side when empty.
func startSide(side string) (string, error) {
	switch side {
	case "ct", "t":
		return side, nil
	case "":
		var b [1]byte
		if _, err := rand.Read(b[:]); err != nil {
			return "", err
		}
		if b[0]&1 == 0 {
			return "ct", nil
		}
		return "t", nil
	}
	return "", fmt.Errorf("invalid side %q", side)
}

func newAuthKey() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// escapeLike escapes LIKE wildcards for use with ESCAPE '|'.
func escapeLike(s string) string {
	r := strings.NewReplacer("|", "||", "%", "|%", "_", "|_")
	return r.Replace(s)
}

// matchWhere builds the WHERE clause of a match list. Columns are prefixed
// with the alias m. The clause contains only literals and ? placeholders.
func matchWhere(f MatchFilter) (string, []any) {
	var conds []string
	var args []any
	if f.Archived {
		conds = append(conds, "m.status = 14")
	} else {
		conds = append(conds, "m.status BETWEEN 0 AND 13")
	}
	if f.SeasonID > 0 {
		conds = append(conds, "m.season_id = ?")
		args = append(args, f.SeasonID)
	}
	if f.TeamID > 0 {
		conds = append(conds, "(m.team_a = ? OR m.team_b = ?)")
		args = append(args, f.TeamID, f.TeamID)
	}
	if q := strings.TrimSpace(f.Search); q != "" {
		like := "%" + escapeLike(q) + "%"
		conds = append(conds, "(m.team_a_name LIKE ? ESCAPE '|' OR m.team_b_name LIKE ? ESCAPE '|')")
		args = append(args, like, like)
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

func normalizePage(f MatchFilter) (page, perPage int) {
	page, perPage = f.Page, f.PerPage
	if page < 1 {
		page = defaultPage
	}
	if perPage < 1 {
		perPage = defaultPerPage
	}
	return page, perPage
}

// inputCols are the matchs columns written from a MatchInput, in the order
// of inputValues.
var inputCols = []string{
	"server_id", "ip", "season_id",
	"team_a", "team_a_flag", "team_a_name", "team_b", "team_b_flag", "team_b_name",
	"max_round", "rules", "overtime_startmoney", "overtime_max_round",
	"config_full_score", "config_ot", "config_streamer", "config_knife_round",
	"config_switch_auto", "config_auto_change_password", "config_password", "config_heatmap",
	"map_selection_mode", "ingame_enable", "force_zoom_match", "identifier_id",
	"startdate", "auto_start", "auto_start_time",
}

func inputValues(in MatchInput, ip string) []any {
	mode := in.MapSelectionMode
	if mode == "" {
		mode = "normal"
	}
	var start any
	if in.StartDate != nil {
		start = in.StartDate.UTC()
	}
	return []any{
		ptrArg(in.ServerID), strArg(ip), ptrArg(in.SeasonID),
		ptrArg(in.TeamAID), in.TeamAFlag, in.TeamAName, ptrArg(in.TeamBID), in.TeamBFlag, in.TeamBName,
		in.MaxRound, in.Rules, in.OvertimeStartMoney, in.OvertimeMaxRound,
		in.ConfigFullScore, in.ConfigOT, in.ConfigStreamer, in.ConfigKnifeRound,
		in.ConfigSwitchAuto, in.ConfigAutoChangePassword, in.ConfigPassword, in.ConfigHeatmap,
		mode, in.IngameEnable, in.ForceZoomMatch, strArg(in.IdentifierID),
		start, in.AutoStart, in.AutoStartTime,
	}
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}

func (s *Store) inTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

const matchColumns = `m.id, m.server_id, m.season_id, COALESCE(m.ip, ''),
	m.team_a, m.team_b,
	COALESCE(m.team_a_name, ''), COALESCE(m.team_b_name, ''),
	COALESCE(m.team_a_flag, ''), COALESCE(m.team_b_flag, ''),
	COALESCE(m.status, 0), COALESCE(m.is_paused, 0), COALESCE(m.score_a, 0), COALESCE(m.score_b, 0),
	COALESCE(m.max_round, 0), COALESCE(m.rules, ''),
	COALESCE(m.overtime_startmoney, 0), COALESCE(m.overtime_max_round, 0),
	COALESCE(m.config_full_score, 0), COALESCE(m.config_ot, 0), COALESCE(m.config_streamer, 0),
	COALESCE(m.config_knife_round, 0), COALESCE(m.config_switch_auto, 0),
	COALESCE(m.config_auto_change_password, 0), COALESCE(m.config_password, ''),
	COALESCE(m.config_heatmap, 0), COALESCE(m.config_authkey, ''),
	COALESCE(m.enable, 0), COALESCE(m.map_selection_mode, ''), COALESCE(m.ingame_enable, 0),
	COALESCE(m.force_zoom_match, 0), COALESCE(m.identifier_id, ''), m.startdate,
	COALESCE(m.auto_start, 0), COALESCE(m.auto_start_time, 0), m.current_map,
	m.created_at, m.updated_at`

const mapColumnsCM = `cm.id, COALESCE(cm.match_id, 0), COALESCE(cm.map_name, ''),
	COALESCE(cm.score_1, 0), COALESCE(cm.score_2, 0), COALESCE(cm.current_side, ''),
	COALESCE(cm.status, 0), COALESCE(cm.maps_for, ''), COALESCE(cm.nb_ot, 0),
	COALESCE(CAST(cm.identifier_id AS CHAR), ''), COALESCE(cm.tv_record_file, ''),
	cm.created_at, cm.updated_at`

const matchSelect = `SELECT ` + matchColumns + `,
	` + mapColumnsCM + `,
	COALESCE(sv.hostname, ''), COALESCE(sv.tv_ip, '')
	FROM matchs m
	LEFT JOIN maps cm ON cm.id = m.current_map
	LEFT JOIN servers sv ON sv.id = m.server_id`

func scanMatch(sc scanner) (*Match, error) {
	var m Match
	var serverID, seasonID, teamA, teamB, currentMap sql.NullInt64
	var start, created, updated dbTime
	var md mapDest
	dest := []any{
		&m.ID, &serverID, &seasonID, &m.IP,
		&teamA, &teamB,
		&m.TeamAName, &m.TeamBName, &m.TeamAFlag, &m.TeamBFlag,
		&m.Status, &m.IsPaused, &m.ScoreA, &m.ScoreB,
		&m.MaxRound, &m.Rules, &m.OvertimeStartMoney, &m.OvertimeMaxRound,
		&m.ConfigFullScore, &m.ConfigOT, &m.ConfigStreamer,
		&m.ConfigKnifeRound, &m.ConfigSwitchAuto,
		&m.ConfigAutoChangePassword, &m.ConfigPassword,
		&m.ConfigHeatmap, &m.ConfigAuthKey,
		&m.Enable, &m.MapSelectionMode, &m.IngameEnable,
		&m.ForceZoomMatch, &m.IdentifierID, &start,
		&m.AutoStart, &m.AutoStartTime, &currentMap,
		&created, &updated,
	}
	dest = append(dest, md.dests()...)
	dest = append(dest, &m.ServerHostname, &m.TVIP)
	err := sc.Scan(dest...)
	if err != nil {
		return nil, err
	}
	m.ServerID, m.SeasonID, m.TeamAID, m.TeamBID = nullPtr(serverID), nullPtr(seasonID), nullPtr(teamA), nullPtr(teamB)
	m.CurrentMapID = nullPtr(currentMap)
	m.StartDate = start.ptr()
	m.CreatedAt, m.UpdatedAt = created.T, updated.T
	m.CurrentMap = md.result()
	return &m, nil
}

func (s *Store) ListMatches(ctx context.Context, f MatchFilter) ([]Match, Page, error) {
	page, perPage := normalizePage(f)
	where, args := matchWhere(f)

	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM matchs m"+where, args...).Scan(&total); err != nil {
		return nil, Page{}, err
	}
	p := Page{Page: page, PerPage: perPage, Total: total}

	q := matchSelect + where + " ORDER BY m.id DESC LIMIT ? OFFSET ?"
	rows, err := s.db.QueryContext(ctx, q, append(args, perPage, (page-1)*perPage)...)
	if err != nil {
		return nil, Page{}, err
	}
	defer rows.Close()
	out := []Match{}
	for rows.Next() {
		m, err := scanMatch(rows)
		if err != nil {
			return nil, Page{}, err
		}
		out = append(out, *m)
	}
	return out, p, rows.Err()
}

func (s *Store) GetMatch(ctx context.Context, id int64) (*Match, error) {
	m, err := scanMatch(s.db.QueryRowContext(ctx, matchSelect+" WHERE m.id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return m, err
}

// serverIP returns the ip of a server, or ErrNotFound.
func serverIP(ctx context.Context, q execer, id int64, lock bool) (string, error) {
	query := "SELECT ip FROM servers WHERE id = ?"
	if lock {
		query += " FOR UPDATE"
	}
	var ip string
	err := q.QueryRowContext(ctx, query, id).Scan(&ip)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("server %d: %w", id, ErrNotFound)
	}
	return ip, err
}

// fillTeams copies name and flag from the team row when the input has none.
func fillTeams(ctx context.Context, q execer, in *MatchInput) error {
	fill := func(id *int64, name, flag *string) error {
		if id == nil || *name != "" {
			return nil
		}
		var n, f sql.NullString
		err := q.QueryRowContext(ctx, "SELECT name, flag FROM teams WHERE id = ?", *id).Scan(&n, &f)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		*name = n.String
		if *flag == "" {
			*flag = f.String
		}
		return nil
	}
	if err := fill(in.TeamAID, &in.TeamAName, &in.TeamAFlag); err != nil {
		return err
	}
	return fill(in.TeamBID, &in.TeamBName, &in.TeamBFlag)
}

func insertMap(ctx context.Context, q execer, matchID int64, mi MapInput, side string) (int64, error) {
	pickedBy := mi.PickedBy
	if pickedBy == "" {
		pickedBy = "default"
	}
	t := now()
	res, err := q.ExecContext(ctx,
		`INSERT INTO maps (match_id, map_name, score_1, score_2, current_side, status, maps_for, nb_ot, created_at, updated_at)
		 VALUES (?, ?, 0, 0, ?, 0, ?, 0, ?, ?)`,
		matchID, mi.Name, side, pickedBy, t, t)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) CreateMatch(ctx context.Context, in MatchInput) (int64, error) {
	if len(in.Maps) != 1 {
		return 0, invalid("a match is created with exactly one map")
	}
	if err := validateMatch(in, matchSnapshot{}); err != nil {
		return 0, err
	}
	side, err := startSide(in.Side)
	if err != nil {
		return 0, err
	}
	key, err := newAuthKey()
	if err != nil {
		return 0, err
	}
	var id int64
	err = s.inTx(ctx, func(tx *sql.Tx) error {
		ip := ""
		if in.ServerID != nil {
			if ip, err = serverIP(ctx, tx, *in.ServerID, false); err != nil {
				return err
			}
		}
		if err := fillTeams(ctx, tx, &in); err != nil {
			return err
		}
		t := now()
		cols := append(append([]string{}, inputCols...),
			"status", "is_paused", "score_a", "score_b", "config_authkey", "enable", "created_at", "updated_at")
		args := append(inputValues(in, ip), 0, false, 0, 0, key, false, t, t)
		res, err := tx.ExecContext(ctx,
			"INSERT INTO matchs ("+strings.Join(cols, ", ")+") VALUES ("+placeholders(len(cols))+")", args...)
		if err != nil {
			return err
		}
		if id, err = res.LastInsertId(); err != nil {
			return err
		}
		first, err := insertMap(ctx, tx, id, MapInput{Name: in.Maps[0].Name}, side)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE matchs SET current_map = ? WHERE id = ?", first, id)
		return err
	})
	if err != nil {
		return 0, err
	}
	return id, nil
}

// lockMatch reads enable and status of a match and locks the row.
func lockMatch(ctx context.Context, tx *sql.Tx, id int64) (enable bool, status int, err error) {
	err = tx.QueryRowContext(ctx,
		"SELECT COALESCE(enable, 0), COALESCE(status, 0) FROM matchs WHERE id = ? FOR UPDATE", id).Scan(&enable, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return false, 0, ErrNotFound
	}
	return enable, status, err
}

func (s *Store) UpdateMatch(ctx context.Context, id int64, in MatchInput) error {
	if len(in.Maps) != 1 {
		return invalid("a match is edited with exactly one map")
	}
	return s.inTx(ctx, func(tx *sql.Tx) error {
		enable, _, err := lockMatch(ctx, tx, id)
		if err != nil {
			return err
		}
		if enable {
			return ErrMatchEnabled
		}
		ip := ""
		if in.ServerID != nil {
			if ip, err = serverIP(ctx, tx, *in.ServerID, false); err != nil {
				return err
			}
		}
		if err := fillTeams(ctx, tx, &in); err != nil {
			return err
		}

		existing, err := mapIDs(ctx, tx, id)
		if err != nil {
			return err
		}
		var current sql.NullInt64
		if err := tx.QueryRowContext(ctx, "SELECT current_map FROM matchs WHERE id = ?", id).Scan(&current); err != nil {
			return err
		}

		t := now()
		next := current.Int64
		if !current.Valid {
			if len(existing) == 0 {
				return errors.New("match has no map")
			}
			next = existing[0]
		}
		var old matchSnapshot
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(team_a_name, ''), COALESCE(team_b_name, ''),
			COALESCE(team_a_flag, ''), COALESCE(team_b_flag, ''), COALESCE(rules, ''), COALESCE(config_password, ''),
			COALESCE((SELECT map_name FROM maps WHERE id = ?), '') FROM matchs WHERE id = ?`, next, id).
			Scan(&old.TeamAName, &old.TeamBName, &old.TeamAFlag, &old.TeamBFlag, &old.Rules, &old.Password, &old.MapName); err != nil {
			return err
		}
		if err := validateMatch(in, old); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE maps SET map_name = ?, updated_at = ? WHERE id = ?",
			in.Maps[0].Name, t, next); err != nil {
			return err
		}

		sets := make([]string, 0, len(inputCols)+2)
		for _, c := range inputCols {
			sets = append(sets, c+" = ?")
		}
		sets = append(sets, "current_map = ?", "updated_at = ?")
		args := append(inputValues(in, ip), next, t, id)
		if _, err := tx.ExecContext(ctx, "UPDATE matchs SET "+strings.Join(sets, ", ")+" WHERE id = ?", args...); err != nil {
			return err
		}
		return nil
	})
}

func mapIDs(ctx context.Context, q execer, matchID int64) ([]int64, error) {
	rows, err := q.QueryContext(ctx, "SELECT id FROM maps WHERE match_id = ? ORDER BY id ASC", matchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// deleteMap removes a map and everything that hangs off it, children first.
func deleteMap(ctx context.Context, q execer, mapID int64) error {
	stmts := []string{
		"DELETE FROM players_snapshot WHERE player_id IN (SELECT id FROM players WHERE map_id = ?)",
		"DELETE FROM players_heatmap WHERE map_id = ?",
		"DELETE FROM `round` WHERE map_id = ?",
		"DELETE FROM round_summary WHERE map_id = ?",
		"DELETE FROM player_kill WHERE map_id = ?",
		"DELETE FROM players WHERE map_id = ?",
		"DELETE FROM maps_score WHERE map_id = ?",
		"DELETE FROM maps WHERE id = ?",
	}
	for _, st := range stmts {
		if _, err := q.ExecContext(ctx, st, mapID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) DeleteMatch(ctx context.Context, id int64) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		enable, _, err := lockMatch(ctx, tx, id)
		if err != nil {
			return err
		}
		if enable {
			return ErrMatchEnabled
		}
		ids, err := mapIDs(ctx, tx, id)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE matchs SET current_map = NULL WHERE id = ?", id); err != nil {
			return err
		}
		for _, mapID := range ids {
			if err := deleteMap(ctx, tx, mapID); err != nil {
				return err
			}
		}
		for _, st := range []string{
			"DELETE FROM players_heatmap WHERE match_id = ?",
			"DELETE FROM `round` WHERE match_id = ?",
			"DELETE FROM round_summary WHERE match_id = ?",
			"DELETE FROM player_kill WHERE match_id = ?",
			"DELETE FROM players WHERE match_id = ?",
			"DELETE FROM matchs WHERE id = ?",
		} {
			if _, err := tx.ExecContext(ctx, st, id); err != nil {
				return err
			}
		}
		return nil
	})
}

// serverBusy reports whether another enabled, unfinished match uses the server.
func serverBusy(ctx context.Context, tx *sql.Tx, serverID, exceptMatch int64) (bool, error) {
	var n int
	err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM matchs WHERE server_id = ? AND id <> ? AND enable = 1 AND status < ?`,
		serverID, exceptMatch, StatusEndMatch).Scan(&n)
	return n > 0, err
}

func ensureAuthKey(ctx context.Context, tx *sql.Tx, id int64) error {
	var key sql.NullString
	if err := tx.QueryRowContext(ctx, "SELECT config_authkey FROM matchs WHERE id = ?", id).Scan(&key); err != nil {
		return err
	}
	if key.String != "" {
		return nil
	}
	nk, err := newAuthKey()
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE matchs SET config_authkey = ?, updated_at = ? WHERE id = ?", nk, now(), id)
	return err
}

// StartMatch assigns the server and enables the match: ip, server_id, enable=1,
// status=1, scores zeroed, config_authkey generated when empty.
func (s *Store) StartMatch(ctx context.Context, id, serverID int64) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		enable, status, err := lockMatch(ctx, tx, id)
		if err != nil {
			return err
		}
		if enable {
			return ErrMatchEnabled
		}
		if status == StatusArchived {
			return invalid("an archived match cannot be started")
		}
		ip, err := serverIP(ctx, tx, serverID, true)
		if err != nil {
			return err
		}
		busy, err := serverBusy(ctx, tx, serverID, id)
		if err != nil {
			return err
		}
		if busy {
			return ErrServerBusy
		}
		t := now()
		if _, err := tx.ExecContext(ctx,
			`UPDATE matchs SET ip = ?, server_id = ?, enable = 1, status = ?, score_a = 0, score_b = 0, updated_at = ? WHERE id = ?`,
			ip, serverID, StatusStarting, t, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE maps SET score_1 = 0, score_2 = 0, updated_at = ?
			 WHERE id = (SELECT current_map FROM matchs WHERE id = ?)`, t, id); err != nil {
			return err
		}
		return ensureAuthKey(ctx, tx, id)
	})
}

// AssignServer sets the server without starting the match.
func (s *Store) AssignServer(ctx context.Context, id, serverID int64) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		enable, _, err := lockMatch(ctx, tx, id)
		if err != nil {
			return err
		}
		if enable {
			return ErrMatchEnabled
		}
		ip, err := serverIP(ctx, tx, serverID, false)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE matchs SET ip = ?, server_id = ?, updated_at = ? WHERE id = ?",
			ip, serverID, now(), id)
		return err
	})
}

// RetryMatch enables a match again on the server it already has. It returns
// ErrServerBusy when another enabled match uses that server.
func (s *Store) RetryMatch(ctx context.Context, id int64) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		enable, _, err := lockMatch(ctx, tx, id)
		if err != nil {
			return err
		}
		if enable {
			return ErrMatchEnabled
		}
		var serverID sql.NullInt64
		if err := tx.QueryRowContext(ctx, "SELECT server_id FROM matchs WHERE id = ?", id).Scan(&serverID); err != nil {
			return err
		}
		if !serverID.Valid {
			return fmt.Errorf("match %d has no server: %w", id, ErrNotFound)
		}
		if _, err := serverIP(ctx, tx, serverID.Int64, true); err != nil {
			return err
		}
		busy, err := serverBusy(ctx, tx, serverID.Int64, id)
		if err != nil {
			return err
		}
		if busy {
			return ErrServerBusy
		}
		if _, err := tx.ExecContext(ctx, "UPDATE matchs SET enable = 1, updated_at = ? WHERE id = ?", now(), id); err != nil {
			return err
		}
		return ensureAuthKey(ctx, tx, id)
	})
}

func (s *Store) ResetMatch(ctx context.Context, id int64) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		enable, status, err := lockMatch(ctx, tx, id)
		if err != nil {
			return err
		}
		if enable && IsRunning(status) {
			return ErrMatchEnabled
		}
		ids, err := mapIDs(ctx, tx, id)
		if err != nil {
			return err
		}
		t := now()
		var first any
		if len(ids) > 0 {
			first = ids[0]
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE matchs SET score_a = 0, score_b = 0, status = 0, enable = 0, is_paused = 0,
			 ingame_enable = NULL, current_map = ?, updated_at = ? WHERE id = ?`, first, t, id); err != nil {
			return err
		}
		for _, st := range []string{
			"DELETE FROM maps_score WHERE map_id IN (SELECT id FROM maps WHERE match_id = ?)",
			"DELETE FROM round_summary WHERE match_id = ?",
		} {
			if _, err := tx.ExecContext(ctx, st, id); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx,
			"UPDATE maps SET score_1 = 0, score_2 = 0, nb_ot = 0, status = 0, updated_at = ? WHERE match_id = ?", t, id)
		return err
	})
}

func (s *Store) ArchiveMatch(ctx context.Context, id int64) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		enable, _, err := lockMatch(ctx, tx, id)
		if err != nil {
			return err
		}
		if enable {
			return ErrMatchEnabled
		}
		_, err = tx.ExecContext(ctx, "UPDATE matchs SET enable = 0, status = ?, updated_at = ? WHERE id = ?",
			StatusArchived, now(), id)
		return err
	})
}

func (s *Store) DuplicateMatch(ctx context.Context, id int64) (int64, error) {
	key, err := newAuthKey()
	if err != nil {
		return 0, err
	}
	var newID int64
	err = s.inTx(ctx, func(tx *sql.Tx) error {
		var current sql.NullInt64
		err := tx.QueryRowContext(ctx, "SELECT current_map FROM matchs WHERE id = ?", id).Scan(&current)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		t := now()
		cols := strings.Join(inputCols, ", ")
		res, err := tx.ExecContext(ctx,
			"INSERT INTO matchs ("+cols+`, status, is_paused, score_a, score_b, config_authkey, enable, created_at, updated_at)
			 SELECT `+cols+`, 0, 0, 0, 0, ?, 0, ?, ? FROM matchs WHERE id = ?`, key, t, t, id)
		if err != nil {
			return err
		}
		if newID, err = res.LastInsertId(); err != nil {
			return err
		}
		ids, err := mapIDs(ctx, tx, id)
		if err != nil {
			return err
		}
		var next any
		for i, src := range ids {
			res, err := tx.ExecContext(ctx,
				`INSERT INTO maps (match_id, map_name, score_1, score_2, current_side, status, maps_for, nb_ot, created_at, updated_at)
				 SELECT ?, map_name, 0, 0, current_side, 0, maps_for, 0, ?, ? FROM maps WHERE id = ?`, newID, t, t, src)
			if err != nil {
				return err
			}
			mapID, err := res.LastInsertId()
			if err != nil {
				return err
			}
			if i == 0 || (current.Valid && src == current.Int64) {
				next = mapID
			}
		}
		_, err = tx.ExecContext(ctx, "UPDATE matchs SET current_map = ? WHERE id = ?", next, newID)
		return err
	})
	if err != nil {
		return 0, err
	}
	return newID, nil
}
