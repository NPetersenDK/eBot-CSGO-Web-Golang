package store

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"database/sql"

	"github.com/go-sql-driver/mysql"
)

//go:embed testdata/schema_matches.sql
var schemaMatches string

// matchesTestStore returns a store on a fresh throwaway database derived from
// EBOT_TEST_DSN. The test is skipped when the variable is empty.
func matchesTestStore(t *testing.T) *Store {
	t.Helper()
	dsn := ""
	if v, ok := matchesTestDSN(); ok {
		dsn = v
	}
	if dsn == "" {
		t.Skip("EBOT_TEST_DSN is not set")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := cfg.DBName + "_matches_test"
	admin := *cfg
	admin.DBName = ""
	adminDB, err := sql.Open("mysql", admin.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer adminDB.Close()
	ctx := context.Background()
	for _, q := range []string{"DROP DATABASE IF EXISTS `" + name + "`", "CREATE DATABASE `" + name + "`"} {
		if _, err := adminDB.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	cfg.DBName = name
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		if a, err := sql.Open("mysql", admin.FormatDSN()); err == nil {
			_, _ = a.Exec("DROP DATABASE IF EXISTS `" + name + "`")
			a.Close()
		}
	})
	for _, st := range strings.Split(schemaMatches, ";\n") {
		if strings.TrimSpace(st) == "" {
			continue
		}
		if _, err := db.ExecContext(ctx, st); err != nil {
			t.Fatalf("schema: %v\n%s", err, st)
		}
	}
	return New(db)
}

func seedServer(t *testing.T, s *Store, ip string) int64 {
	t.Helper()
	res, err := s.db.Exec(`INSERT INTO servers (ip, rcon, hostname, tv_ip, created_at, updated_at) VALUES (?, 'x', ?, ?, NOW(), NOW())`,
		ip, "host-"+ip, "tv-"+ip)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func seedTeam(t *testing.T, s *Store, name string) int64 {
	t.Helper()
	res, err := s.db.Exec(`INSERT INTO teams (name, shorthandle, flag, created_at, updated_at) VALUES (?, ?, 'DK', NOW(), NOW())`, name, name)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func seedSeason(t *testing.T, s *Store, name string) int64 {
	t.Helper()
	res, err := s.db.Exec(`INSERT INTO seasons (name, active, created_at, updated_at) VALUES (?, 1, NOW(), NOW())`, name)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func sampleInput(maps ...string) MatchInput {
	in := MatchInput{TeamAName: "Alpha", TeamBName: "Bravo", MaxRound: 15, Rules: "esl", MapSelectionMode: "normal"}
	for _, m := range maps {
		in.Maps = append(in.Maps, MapInput{Name: m, PickedBy: "default"})
	}
	return in
}

func TestEscapeLike(t *testing.T) {
	cases := map[string]string{"abc": "abc", "50%": "50|%", "a_b": "a|_b", "x|y": "x||y", "%_|": "|%|_||"}
	for in, want := range cases {
		if got := escapeLike(in); got != want {
			t.Errorf("escapeLike(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMatchWhere(t *testing.T) {
	where, args := matchWhere(MatchFilter{})
	if where != " WHERE m.status BETWEEN 0 AND 13" || len(args) != 0 {
		t.Errorf("default: %q %v", where, args)
	}
	where, _ = matchWhere(MatchFilter{Archived: true})
	if where != " WHERE m.status = 14" {
		t.Errorf("archived: %q", where)
	}
	where, args = matchWhere(MatchFilter{SeasonID: 2, TeamID: 3, Search: " 50% "})
	if strings.Count(where, "?") != len(args) || len(args) != 5 {
		t.Errorf("placeholders and args differ: %q %v", where, args)
	}
	if args[3] != "%50|%%" {
		t.Errorf("search arg = %v", args[3])
	}
	if strings.Contains(where, "50") {
		t.Errorf("input leaked into SQL: %q", where)
	}
}

func TestNormalizePage(t *testing.T) {
	if p, pp := normalizePage(MatchFilter{}); p != 1 || pp != 12 {
		t.Errorf("got %d %d", p, pp)
	}
	if p, pp := normalizePage(MatchFilter{Page: 3, PerPage: 5}); p != 3 || pp != 5 {
		t.Errorf("got %d %d", p, pp)
	}
}

func TestNewAuthKey(t *testing.T) {
	seen := map[string]bool{}
	for range 50 {
		k, err := newAuthKey()
		if err != nil || len(k) != 32 || seen[k] {
			t.Fatalf("bad key %q err %v", k, err)
		}
		seen[k] = true
	}
}

func TestInputValuesMatchColumns(t *testing.T) {
	if got, want := len(inputValues(MatchInput{}, "")), len(inputCols); got != want {
		t.Fatalf("%d values for %d columns", got, want)
	}
}

func TestDBTimeScan(t *testing.T) {
	var d dbTime
	if err := d.Scan([]byte("2024-05-06 07:08:09")); err != nil || !d.Valid || d.T.Hour() != 7 {
		t.Errorf("bytes: %v %v", d, err)
	}
	if err := d.Scan(nil); err != nil || d.Valid {
		t.Errorf("nil: %v %v", d, err)
	}
	if err := d.Scan("0000-00-00 00:00:00"); err != nil || d.Valid {
		t.Errorf("zero date: %v %v", d, err)
	}
	if err := d.Scan(time.Now()); err != nil || !d.Valid {
		t.Errorf("time: %v %v", d, err)
	}
}

func TestCreateMatchRejectsWrongMapCount(t *testing.T) {
	s := &Store{}
	if _, err := s.CreateMatch(context.Background(), sampleInput()); err == nil {
		t.Fatal("expected error")
	}
}

func TestCreateBo1AndBo3(t *testing.T) {
	s := matchesTestStore(t)
	ctx := context.Background()
	srv := seedServer(t, s, "1.1.1.1:27015")

	in := sampleInput("de_dust2")
	in.ServerID = &srv
	id1, err := s.CreateMatch(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.GetMatch(ctx, id1)
	if err != nil {
		t.Fatal(err)
	}
	if m.CurrentMap == nil || m.CurrentMap.Name != "de_dust2" || *m.CurrentMapID != m.CurrentMap.ID {
		t.Fatalf("current map: %+v", m.CurrentMap)
	}
	if m.Status != 0 || m.Enable || m.ConfigAuthKey == "" || m.IP != "1.1.1.1:27015" {
		t.Errorf("match: %+v", m)
	}
	if m.ServerHostname != "host-1.1.1.1:27015" || m.TVIP != "tv-1.1.1.1:27015" {
		t.Errorf("server extras: %q %q", m.ServerHostname, m.TVIP)
	}
	if m.CreatedAt.IsZero() || m.UpdatedAt.IsZero() {
		t.Error("timestamps not set")
	}

	in3 := sampleInput("de_inferno")
	in3.MapSelectionMode = "bo3_modeb"
	in3.Side = "t"
	id3, err := s.CreateMatch(ctx, in3)
	if err != nil {
		t.Fatal(err)
	}
	maps, err := s.ListMaps(ctx, id3)
	if err != nil || len(maps) != 1 {
		t.Fatalf("maps: %v %v", maps, err)
	}
	m3, _ := s.GetMatch(ctx, id3)
	if m3.MapSelectionMode != "bo3_modeb" || *m3.CurrentMapID != maps[0].ID || maps[0].CurrentSide != "t" || maps[0].PickedBy != "default" {
		t.Errorf("bo3: %+v %+v", m3, maps[0])
	}
	if m.CurrentMap.CurrentSide != "ct" && m.CurrentMap.CurrentSide != "t" {
		t.Errorf("random side: %q", m.CurrentMap.CurrentSide)
	}
	if m3.ConfigAuthKey == m.ConfigAuthKey {
		t.Error("auth keys equal")
	}
	bad := sampleInput("a", "b")
	if _, err := s.CreateMatch(ctx, bad); err == nil {
		t.Error("two maps accepted")
	}
	bad = sampleInput("a")
	bad.Side = "x"
	if _, err := s.CreateMatch(ctx, bad); err == nil {
		t.Error("bad side accepted")
	}

	if _, err := s.GetMatch(ctx, 9999); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing match: %v", err)
	}
}

func TestCreateFillsTeamNames(t *testing.T) {
	s := matchesTestStore(t)
	ctx := context.Background()
	a := seedTeam(t, s, "Astralis")
	in := sampleInput("de_dust2")
	in.TeamAID, in.TeamAName = &a, ""
	id, err := s.CreateMatch(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := s.GetMatch(ctx, id)
	if m.TeamAName != "Astralis" || m.TeamAFlag != "DK" || m.TeamAID == nil {
		t.Errorf("%+v", m)
	}
}

func TestUpdateMatch(t *testing.T) {
	s := matchesTestStore(t)
	ctx := context.Background()
	id := createWithMaps(t, s, "de_dust2", "de_nuke", "de_mirage")
	before, _ := s.ListMaps(ctx, id)
	mustExec(t, s, "UPDATE matchs SET current_map = ? WHERE id = ?", before[2].ID, id)

	in := sampleInput("de_train")
	in.Rules = "faceit"
	if err := s.UpdateMatch(ctx, id, in); err != nil {
		t.Fatal(err)
	}
	m, _ := s.GetMatch(ctx, id)
	maps, _ := s.ListMaps(ctx, id)
	if m.Rules != "faceit" || len(maps) != 3 || maps[2].Name != "de_train" || maps[0].Name != "de_dust2" {
		t.Fatalf("after update: %+v %+v", m, maps)
	}
	if m.CurrentMapID == nil || *m.CurrentMapID != before[2].ID {
		t.Errorf("current map changed: %v", m.CurrentMapID)
	}
	if err := s.UpdateMatch(ctx, id, sampleInput("a", "b")); err == nil {
		t.Error("two maps accepted")
	}

	mustExec(t, s, "UPDATE matchs SET enable = 1 WHERE id = ?", id)
	if err := s.UpdateMatch(ctx, id, in); !errors.Is(err, ErrMatchEnabled) {
		t.Errorf("enabled edit: %v", err)
	}
	if err := s.UpdateMatch(ctx, 4242, in); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing edit: %v", err)
	}
}

func TestStartAndServerBusy(t *testing.T) {
	s := matchesTestStore(t)
	ctx := context.Background()
	srv := seedServer(t, s, "2.2.2.2:27015")
	a, _ := s.CreateMatch(ctx, sampleInput("de_dust2"))
	b, _ := s.CreateMatch(ctx, sampleInput("de_nuke"))
	if _, err := s.db.Exec("UPDATE matchs SET score_a = 5, config_authkey = '' WHERE id = ?", a); err != nil {
		t.Fatal(err)
	}

	if err := s.StartMatch(ctx, a, srv); err != nil {
		t.Fatal(err)
	}
	m, _ := s.GetMatch(ctx, a)
	if !m.Enable || m.Status != StatusStarting || m.IP != "2.2.2.2:27015" || m.ServerID == nil || m.ScoreA != 0 || m.ConfigAuthKey == "" {
		t.Errorf("started: %+v", m)
	}
	if err := s.StartMatch(ctx, b, srv); !errors.Is(err, ErrServerBusy) {
		t.Errorf("busy: %v", err)
	}
	if err := s.StartMatch(ctx, a, srv); !errors.Is(err, ErrMatchEnabled) {
		t.Errorf("already enabled: %v", err)
	}
	if err := s.StartMatch(ctx, b, 777); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing server: %v", err)
	}
	if err := s.DeleteMatch(ctx, a); !errors.Is(err, ErrMatchEnabled) {
		t.Errorf("delete enabled: %v", err)
	}
	if err := s.ArchiveMatch(ctx, a); !errors.Is(err, ErrMatchEnabled) {
		t.Errorf("archive enabled: %v", err)
	}

	if _, err := s.db.Exec("UPDATE matchs SET enable = 0 WHERE id = ?", a); err != nil {
		t.Fatal(err)
	}
	if err := s.AssignServer(ctx, b, srv); err != nil {
		t.Fatal(err)
	}
	if err := s.RetryMatch(ctx, b); err != nil {
		t.Fatalf("retry on a free server: %v", err)
	}
	if err := s.RetryMatch(ctx, a); !errors.Is(err, ErrServerBusy) {
		t.Errorf("retry on a busy server: %v", err)
	}
	mb, _ := s.GetMatch(ctx, b)
	if !mb.Enable || mb.ServerID == nil || mb.ConfigAuthKey == "" {
		t.Errorf("retried: %+v", mb)
	}
}

func TestResetMatch(t *testing.T) {
	s := matchesTestStore(t)
	ctx := context.Background()
	id := createWithMaps(t, s, "de_dust2", "de_nuke")
	maps, _ := s.ListMaps(ctx, id)
	mustExec(t, s, "UPDATE matchs SET score_a = 7, score_b = 3, status = 8, enable = 1, is_paused = 1, current_map = ? WHERE id = ?", maps[1].ID, id)
	mustExec(t, s, "UPDATE maps SET score_1 = 7, score_2 = 3, nb_ot = 2, status = 4 WHERE id = ?", maps[0].ID)
	mustExec(t, s, "INSERT INTO maps_score (map_id, type_score, score1_side1, created_at, updated_at) VALUES (?, 'normal', 7, NOW(), NOW())", maps[0].ID)
	mustExec(t, s, "INSERT INTO round_summary (match_id, map_id, round_id, created_at, updated_at) VALUES (?, ?, 1, NOW(), NOW())", id, maps[0].ID)

	if err := s.ResetMatch(ctx, id); err != nil {
		t.Fatal(err)
	}
	m, _ := s.GetMatch(ctx, id)
	if m.ScoreA != 0 || m.ScoreB != 0 || m.Status != 0 || m.Enable || m.IsPaused || *m.CurrentMapID != maps[0].ID {
		t.Errorf("reset: %+v", m)
	}
	after, _ := s.ListMaps(ctx, id)
	if after[0].Score1 != 0 || after[0].Overtimes != 0 || after[0].Status != 0 {
		t.Errorf("map: %+v", after[0])
	}
	if sc, _ := s.ListMapScores(ctx, maps[0].ID); len(sc) != 0 {
		t.Errorf("scores left: %v", sc)
	}
	if rs, _ := s.ListRoundSummaries(ctx, id, 0); len(rs) != 0 {
		t.Errorf("rounds left: %v", rs)
	}
	if err := s.ResetMatch(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing: %v", err)
	}
}

func TestDuplicateMatch(t *testing.T) {
	s := matchesTestStore(t)
	ctx := context.Background()
	srv := seedServer(t, s, "3.3.3.3:27015")
	in := sampleInput("de_dust2")
	in.ServerID = &srv
	in.Rules = "faceit"
	id, _ := s.CreateMatch(ctx, in)
	extraMap(t, s, id, "de_nuke")
	maps, _ := s.ListMaps(ctx, id)
	mustExec(t, s, "UPDATE matchs SET current_map = ?, status = 13, score_a = 16 WHERE id = ?", maps[1].ID, id)

	nid, err := s.DuplicateMatch(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if nid == id {
		t.Fatal("same id")
	}
	orig, _ := s.GetMatch(ctx, id)
	cp, _ := s.GetMatch(ctx, nid)
	if cp.Status != 0 || cp.Enable || cp.ScoreA != 0 || cp.Rules != "faceit" || cp.ConfigAuthKey == "" || cp.ConfigAuthKey == orig.ConfigAuthKey {
		t.Errorf("copy: %+v", cp)
	}
	cmaps, _ := s.ListMaps(ctx, nid)
	if len(cmaps) != 2 || cmaps[1].Name != "de_nuke" || cmaps[0].MatchID != nid {
		t.Fatalf("maps: %+v", cmaps)
	}
	if cp.CurrentMapID == nil || *cp.CurrentMapID != cmaps[1].ID {
		t.Errorf("current map %v want %d", cp.CurrentMapID, cmaps[1].ID)
	}
	if _, err := s.DuplicateMatch(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing: %v", err)
	}
}

func TestArchiveAndDelete(t *testing.T) {
	s := matchesTestStore(t)
	ctx := context.Background()
	id, _ := s.CreateMatch(ctx, sampleInput("de_dust2"))
	maps, _ := s.ListMaps(ctx, id)
	mustExec(t, s, "INSERT INTO players (match_id, map_id, pseudo, created_at, updated_at) VALUES (?, ?, 'p', NOW(), NOW())", id, maps[0].ID)
	mustExec(t, s, "INSERT INTO round_summary (match_id, map_id, round_id, created_at, updated_at) VALUES (?, ?, 1, NOW(), NOW())", id, maps[0].ID)

	if err := s.ArchiveMatch(ctx, id); err != nil {
		t.Fatal(err)
	}
	m, _ := s.GetMatch(ctx, id)
	if m.Status != StatusArchived {
		t.Errorf("status %d", m.Status)
	}
	if err := s.DeleteMatch(ctx, id); err != nil {
		t.Fatal(err)
	}
	for _, tbl := range []string{"matchs", "maps", "players", "round_summary"} {
		var n int
		if err := s.db.QueryRow("SELECT COUNT(*) FROM " + tbl).Scan(&n); err != nil || n != 0 {
			t.Errorf("%s: %d rows left, err %v", tbl, n, err)
		}
	}
	if err := s.DeleteMatch(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete: %v", err)
	}
}

func TestListMatchesFiltersAndPaging(t *testing.T) {
	s := matchesTestStore(t)
	ctx := context.Background()
	season := seedSeason(t, s, "S1")
	ta := seedTeam(t, s, "Alpha")
	tb := seedTeam(t, s, "Bravo")
	var ids []int64
	for i := range 15 {
		in := sampleInput("de_dust2")
		in.TeamAName = fmt.Sprintf("Team%d", i)
		if i < 5 {
			in.SeasonID = &season
		}
		switch i {
		case 0:
			in.TeamAID = &ta
		case 1:
			in.TeamBID = &tb
		case 2:
			in.TeamAName = "100%_sure"
		}
		id, err := s.CreateMatch(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := s.ArchiveMatch(ctx, ids[14]); err != nil {
		t.Fatal(err)
	}

	list, p, err := s.ListMatches(ctx, MatchFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 12 || p.Total != 14 || p.Page != 1 || p.PerPage != 12 || p.Pages() != 2 {
		t.Fatalf("page 1: len %d %+v", len(list), p)
	}
	if list[0].ID != ids[13] || list[0].CurrentMap == nil {
		t.Errorf("order or current map: %d %+v", list[0].ID, list[0].CurrentMap)
	}
	list, _, _ = s.ListMatches(ctx, MatchFilter{Page: 2})
	if len(list) != 2 {
		t.Errorf("page 2: %d", len(list))
	}
	list, p, _ = s.ListMatches(ctx, MatchFilter{Archived: true})
	if len(list) != 1 || p.Total != 1 || list[0].ID != ids[14] {
		t.Errorf("archived: %d %+v", len(list), p)
	}
	_, p, _ = s.ListMatches(ctx, MatchFilter{SeasonID: season})
	if p.Total != 5 {
		t.Errorf("season total %d", p.Total)
	}
	_, p, _ = s.ListMatches(ctx, MatchFilter{TeamID: ta})
	if p.Total != 1 {
		t.Errorf("team a total %d", p.Total)
	}
	_, p, _ = s.ListMatches(ctx, MatchFilter{TeamID: tb})
	if p.Total != 1 {
		t.Errorf("team b total %d", p.Total)
	}
	list, _, _ = s.ListMatches(ctx, MatchFilter{Search: "100%"})
	if len(list) != 1 || list[0].ID != ids[2] {
		t.Errorf("escaped search: %+v", list)
	}
	_, p, _ = s.ListMatches(ctx, MatchFilter{Search: "%"})
	if p.Total != 1 {
		t.Errorf("a bare percent must not match everything: %d", p.Total)
	}
	_, p, _ = s.ListMatches(ctx, MatchFilter{Search: "Bravo"})
	if p.Total != 14 {
		t.Errorf("team b name search: %d", p.Total)
	}
}

func mustExec(t *testing.T, s *Store, q string, args ...any) {
	t.Helper()
	if _, err := s.db.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func extraMap(t *testing.T, s *Store, matchID int64, name string) {
	t.Helper()
	if _, err := insertMap(context.Background(), s.db, matchID, MapInput{Name: name}, "ct"); err != nil {
		t.Fatal(err)
	}
}

func createWithMaps(t *testing.T, s *Store, names ...string) int64 {
	t.Helper()
	id, err := s.CreateMatch(context.Background(), sampleInput(names[0]))
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range names[1:] {
		extraMap(t, s, id, n)
	}
	return id
}
