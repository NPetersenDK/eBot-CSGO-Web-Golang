package store

import (
	"context"
	"strconv"
	"strings"
	"time"
)

// PlayerMatch is one map a player took part in, with the match around it.
type PlayerMatch struct {
	Player
	Date      time.Time
	Status    int
	TeamAName string
	TeamBName string
	MapName   string
	// Score1 and Score2 are team A's and team B's rounds on the map.
	Score1 int
	Score2 int
}

// OwnTeam is the name of the team the player was on.
func (m PlayerMatch) OwnTeam() string {
	if m.Team == "b" {
		return m.TeamBName
	}
	return m.TeamAName
}

// Opponent is the name of the other team.
func (m PlayerMatch) Opponent() string {
	if m.Team == "b" {
		return m.TeamAName
	}
	return m.TeamBName
}

// Result is the map score with the player's team first.
func (m PlayerMatch) Result() string {
	own, other := m.Score1, m.Score2
	if m.Team == "b" {
		own, other = other, own
	}
	return itoa(own) + " - " + itoa(other)
}

func itoa(n int) string { return strconv.Itoa(n) }

func (m PlayerMatch) stat() PlayerStat {
	return PlayerStat{Kills: int64(m.Kills), Deaths: int64(m.Deaths), Headshots: int64(m.Headshots), Matches: 1}
}

// KD and HSRate use the same rounding as the global tables.
func (m PlayerMatch) KD() string     { return m.stat().KD() }
func (m PlayerMatch) HSRate() string { return m.stat().HSRate() }

// PlayerTotals is a sum over some of a player's maps.
type PlayerTotals struct {
	Maps       int
	Kills      int64
	Assists    int64
	Deaths     int64
	Headshots  int64
	FirstKills int64
	TeamKills  int64
	Bombs      int64
	Defuses    int64
	Points     int64
	// Clutch counts won 1v1 to 1v5. MultiKills counts rounds with 1 to 5 kills.
	Clutch     [5]int64
	MultiKills [5]int64
}

// SumPlayerMatches adds up the given maps.
func SumPlayerMatches(rows []PlayerMatch) PlayerTotals {
	var t PlayerTotals
	for _, r := range rows {
		t.Maps++
		t.Kills += int64(r.Kills)
		t.Assists += int64(r.Assists)
		t.Deaths += int64(r.Deaths)
		t.Headshots += int64(r.Headshots)
		t.FirstKills += int64(r.FirstKills)
		t.TeamKills += int64(r.TeamKills)
		t.Bombs += int64(r.Bombs)
		t.Defuses += int64(r.Defuses)
		t.Points += int64(r.Points)
		for i, v := range []int{r.Clutch1v1, r.Clutch1v2, r.Clutch1v3, r.Clutch1v4, r.Clutch1v5} {
			t.Clutch[i] += int64(v)
		}
		for i, v := range []int{r.Rounds1K, r.Rounds2K, r.Rounds3K, r.Rounds4K, r.Rounds5K} {
			t.MultiKills[i] += int64(v)
		}
	}
	return t
}

func (t PlayerTotals) stat() PlayerStat {
	return PlayerStat{Kills: t.Kills, Deaths: t.Deaths, Headshots: t.Headshots, Matches: int64(t.Maps)}
}

func (t PlayerTotals) KD() string     { return t.stat().KD() }
func (t PlayerTotals) HSRate() string { return t.stat().HSRate() }
func (t PlayerTotals) MVP() string    { return t.stat().MVP() }

// ListPlayerMatches returns every map a player took part in, newest match
// first. Spectator rows are left out. It returns ErrNotFound when there are
// none, or when steamID is empty.
func (s *Store) ListPlayerMatches(ctx context.Context, steamID string) ([]PlayerMatch, error) {
	if steamID == "" {
		return nil, ErrNotFound
	}
	rows, err := s.db.QueryContext(ctx, "SELECT "+playerColumns+
		" FROM players WHERE steamid = ? AND COALESCE(team, 'other') <> 'other' ORDER BY match_id DESC, id DESC", steamID)
	if err != nil {
		return nil, err
	}
	players, err := scanPlayers(rows)
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(players) == 0 {
		return nil, ErrNotFound
	}

	var matchIDs, mapIDs []int64
	seenMatch, seenMap := map[int64]bool{}, map[int64]bool{}
	for _, p := range players {
		if !seenMatch[p.MatchID] {
			seenMatch[p.MatchID] = true
			matchIDs = append(matchIDs, p.MatchID)
		}
		if p.MapID != 0 && !seenMap[p.MapID] {
			seenMap[p.MapID] = true
			mapIDs = append(mapIDs, p.MapID)
		}
	}

	type matchInfo struct {
		date         time.Time
		status       int
		teamA, teamB string
	}
	matches := map[int64]matchInfo{}
	in, args := inClause(matchIDs)
	mrows, err := s.db.QueryContext(ctx, `SELECT id, created_at, COALESCE(team_a_name, ''), COALESCE(team_b_name, ''), COALESCE(status, 0)
		FROM matchs WHERE id IN (`+in+`)`, args...)
	if err != nil {
		return nil, err
	}
	for mrows.Next() {
		var id int64
		var mi matchInfo
		var created dbTime
		if err := mrows.Scan(&id, &created, &mi.teamA, &mi.teamB, &mi.status); err != nil {
			mrows.Close()
			return nil, err
		}
		mi.date = created.T
		matches[id] = mi
	}
	mrows.Close()
	if err := mrows.Err(); err != nil {
		return nil, err
	}

	type mapInfo struct {
		name   string
		s1, s2 int
	}
	maps := map[int64]mapInfo{}
	if len(mapIDs) > 0 {
		in, args = inClause(mapIDs)
		prows, err := s.db.QueryContext(ctx, `SELECT id, COALESCE(map_name, ''), COALESCE(score_1, 0), COALESCE(score_2, 0)
			FROM maps WHERE id IN (`+in+`)`, args...)
		if err != nil {
			return nil, err
		}
		for prows.Next() {
			var id int64
			var mi mapInfo
			if err := prows.Scan(&id, &mi.name, &mi.s1, &mi.s2); err != nil {
				prows.Close()
				return nil, err
			}
			maps[id] = mi
		}
		prows.Close()
		if err := prows.Err(); err != nil {
			return nil, err
		}
	}

	out := make([]PlayerMatch, 0, len(players))
	for _, p := range players {
		mi := matches[p.MatchID]
		mp := maps[p.MapID]
		out = append(out, PlayerMatch{
			Player: p, Date: mi.date, Status: mi.status, TeamAName: mi.teamA, TeamBName: mi.teamB,
			MapName: mp.name, Score1: mp.s1, Score2: mp.s2,
		})
	}
	return out, nil
}

func inClause(ids []int64) (string, []any) {
	marks := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		marks[i] = "?"
		args[i] = id
	}
	return strings.Join(marks, ","), args
}
