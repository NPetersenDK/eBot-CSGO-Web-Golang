package store

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"

	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/auth"
)

func TestSchemaFile(t *testing.T) {
	stmts := parseSchema(schemaSQL)
	var creates, alters int
	tables := map[string]bool{}
	for _, st := range stmts {
		if st.create {
			creates++
			tables[st.table] = true
		} else {
			alters++
		}
	}
	if creates != 23 || alters < 30 {
		t.Fatalf("creates=%d alters=%d", creates, alters)
	}
	for _, st := range stmts {
		if !st.create && !tables[st.table] {
			t.Errorf("foreign key on a table that is not created: %s", st.sql)
		}
		if !st.create && !strings.Contains(st.sql, "ADD CONSTRAINT") {
			t.Errorf("only foreign keys may be altered: %s", st.sql)
		}
	}
	for _, want := range []string{"matchs", "maps", "players", "servers", "teams", "seasons", "advertising", "teams_in_seasons", "sf_guard_user", "sf_guard_user_permission"} {
		if !tables[want] {
			t.Errorf("table %s missing from the schema", want)
		}
	}
	// The file must only create. Anything else could damage a live database.
	for _, line := range strings.Split(schemaSQL, "\n") {
		up := strings.ToUpper(line)
		for _, bad := range []string{"DROP ", "DELETE ", "TRUNCATE ", "UPDATE ", "INSERT "} {
			if strings.HasPrefix(up, bad) {
				t.Errorf("schema.sql must not contain %q", bad)
			}
		}
	}
}

// freshStore returns a store on a new, empty database.
func freshStore(t *testing.T, suffix string) *Store {
	t.Helper()
	dsn, ok := matchesTestDSN()
	if !ok {
		t.Skip("EBOT_TEST_DSN is not set")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := cfg.DBName + "_" + suffix
	admin := *cfg
	admin.DBName = ""
	adminDB, err := sql.Open("mysql", admin.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
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
		_, _ = adminDB.Exec("DROP DATABASE IF EXISTS `" + name + "`")
		adminDB.Close()
	})
	return New(db)
}

func scalar(t *testing.T, s *Store, q string, args ...any) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

func TestMigrateEmptyDatabase(t *testing.T) {
	s := freshStore(t, "migrate_empty")
	ctx := context.Background()

	res, err := s.Migrate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Created) != 23 {
		t.Fatalf("created %d tables: %v", len(res.Created), res.Created)
	}
	if n := scalar(t, s, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE()"); n != 23 {
		t.Errorf("tables = %d", n)
	}
	want := 0
	for _, st := range parseSchema(schemaSQL) {
		if !st.create {
			want++
		}
	}
	if n := scalar(t, s, "SELECT COUNT(*) FROM information_schema.referential_constraints WHERE constraint_schema = DATABASE()"); n != want {
		t.Errorf("foreign keys = %d, want %d", n, want)
	}

	again, err := s.Migrate(ctx)
	if err != nil || len(again.Created) != 0 {
		t.Fatalf("a second run must do nothing: %+v %v", again, err)
	}
}

func TestMigrateLeavesExistingTablesAlone(t *testing.T) {
	s := freshStore(t, "migrate_partial")
	ctx := context.Background()

	// An older database: a servers table with an extra column and a row in it.
	for _, q := range []string{
		"CREATE TABLE servers (id BIGINT AUTO_INCREMENT, ip VARCHAR(50) NOT NULL, rcon VARCHAR(50) NOT NULL, hostname VARCHAR(100) NOT NULL, tv_ip VARCHAR(100), extra VARCHAR(20), created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL, PRIMARY KEY(id)) ENGINE = INNODB",
		"INSERT INTO servers (ip, rcon, hostname, extra, created_at, updated_at) VALUES ('1.2.3.4:27015', 'x', 'keep me', 'custom', NOW(), NOW())",
	} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}

	res, err := s.Migrate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Created) != 22 {
		t.Fatalf("created %d, want 22 (everything but servers): %v", len(res.Created), res.Created)
	}
	var host, extra string
	if err := s.db.QueryRow("SELECT hostname, extra FROM servers").Scan(&host, &extra); err != nil || host != "keep me" || extra != "custom" {
		t.Fatalf("the existing table was changed: %q %q %v", host, extra, err)
	}
	// The key from matchs to servers belongs to a new table, so it is added.
	if n := scalar(t, s, "SELECT COUNT(*) FROM information_schema.referential_constraints WHERE constraint_schema = DATABASE() AND constraint_name = 'matchs_server_id_servers_id'"); n != 1 {
		t.Errorf("matchs to servers key = %d", n)
	}

	// A table that goes missing later is brought back, nothing else is touched.
	if _, err := s.db.Exec("INSERT INTO seasons (name, event, start, end, created_at, updated_at) VALUES ('s', 'e', NOW(), NOW(), NOW(), NOW())"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("DROP TABLE advertising"); err != nil {
		t.Fatal(err)
	}
	res, err = s.Migrate(ctx)
	if err != nil || len(res.Created) != 1 || res.Created[0] != "advertising" {
		t.Fatalf("second run: %+v %v", res, err)
	}
	if n := scalar(t, s, "SELECT COUNT(*) FROM seasons"); n != 1 {
		t.Errorf("seasons rows = %d", n)
	}
	if n := scalar(t, s, "SELECT COUNT(*) FROM information_schema.referential_constraints WHERE constraint_schema = DATABASE() AND table_name = 'advertising'"); n != 1 {
		t.Errorf("advertising key = %d", n)
	}
}

func TestMigrateUndoesAHalfBuiltSchema(t *testing.T) {
	s := freshStore(t, "migrate_rollback")
	stmts := []statement{
		{sql: "CREATE TABLE zz_a (id BIGINT AUTO_INCREMENT, PRIMARY KEY(id)) ENGINE = INNODB", table: "zz_a", create: true},
		{sql: "CREATE TABLE zz_b (id BIGINT AUTO_INCREMENT, a_id BIGINT, PRIMARY KEY(id)) ENGINE = INNODB", table: "zz_b", create: true},
		{sql: "ALTER TABLE zz_b ADD CONSTRAINT zz_b_fk FOREIGN KEY (a_id) REFERENCES does_not_exist(id)", table: "zz_b"},
	}
	res, err := s.migrate(context.Background(), stmts)
	if err == nil {
		t.Fatal("expected an error")
	}
	if len(res.Created) != 0 {
		t.Errorf("created = %v after a failure", res.Created)
	}
	if n := scalar(t, s, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE()"); n != 0 {
		t.Errorf("%d tables were left behind", n)
	}
}

// The store and the bot expect the Doctrine schema. Run the store on it.
func TestStoreOnTheGeneratedSchema(t *testing.T) {
	s := freshStore(t, "migrate_store")
	ctx := context.Background()
	if _, err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	if n, err := s.CountUsers(ctx); err != nil || n != 0 {
		t.Fatalf("users: %d %v", n, err)
	}
	uid, err := s.CreateUser(ctx, UserInput{FirstName: "A", LastName: "B", Email: "a@example.org", Username: "admin", Password: "secret123", IsActive: true, IsSuperAdmin: true})
	if err != nil || uid == 0 {
		t.Fatalf("create user: %d %v", uid, err)
	}
	sess, err := s.VerifyLocal(ctx, "admin", "secret123")
	if err != nil || sess.Role != auth.RoleAdmin {
		t.Fatalf("login: %+v %v", sess, err)
	}
	if _, err := s.VerifyLocal(ctx, "admin", "wrong"); err != auth.ErrInvalidCredentials {
		t.Errorf("wrong password: %v", err)
	}

	if n, err := s.CreateServers(ctx, ServerCreate{Mode: "single", Host: "10.0.0.1", Port: 27015, RCON: "pw", Hostname: "srv", TVIP: "10.0.0.1:27020"}); err != nil || n != 1 {
		t.Fatalf("server: %d %v", n, err)
	}
	servers, err := s.ListServers(ctx)
	if err != nil || len(servers) != 1 {
		t.Fatalf("servers: %v %v", servers, err)
	}
	seasonID, err := s.CreateSeason(ctx, SeasonInput{Name: "S1", Event: "Cup", Start: "2026-01-01 00:00:00", End: "2026-12-31 00:00:00", Active: true})
	if err != nil {
		t.Fatal(err)
	}
	teamA, err := s.CreateTeam(ctx, TeamInput{Name: "Alpha", ShortName: "ALP", Flag: "DK", SeasonIDs: []int64{seasonID}})
	if err != nil {
		t.Fatal(err)
	}
	teamB, err := s.CreateTeam(ctx, TeamInput{Name: "Bravo", ShortName: "BRV", Flag: "SE"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAd(ctx, AdInput{SeasonID: seasonID, Message: "hello", Active: true}); err != nil {
		t.Fatal(err)
	}

	id, err := s.CreateMatch(ctx, MatchInput{
		TeamAID: &teamA, TeamBID: &teamB, SeasonID: &seasonID, MaxRound: 12, Rules: "rules",
		MapSelectionMode: "normal", Maps: []MapInput{{Name: "de_dust2", PickedBy: "default"}}, Side: "ct",
	})
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.GetMatch(ctx, id)
	if err != nil || m.CurrentMap == nil || m.CurrentMap.Name != "de_dust2" || m.TeamAName != "Alpha" || m.ConfigAuthKey == "" {
		t.Fatalf("match: %+v %v", m, err)
	}
	if err := s.StartMatch(ctx, id, servers[0].ID); err != nil {
		t.Fatal(err)
	}
	list, page, err := s.ListMatches(ctx, MatchFilter{Page: 1, PerPage: 10})
	if err != nil || len(list) != 1 || page.Total != 1 || !list[0].Enable {
		t.Fatalf("list: %+v %v", list, err)
	}
	if _, err := s.db.Exec(`INSERT INTO players (match_id, map_id, steamid, pseudo, team, nb_kill, hs, created_at, updated_at) VALUES (?, ?, '7656', 'p', 'a', 5, 2, NOW(), NOW())`, id, m.CurrentMap.ID); err != nil {
		t.Fatal(err)
	}
	g, err := s.GlobalStats(ctx)
	if err != nil || g.Kills != 5 || g.InProgress != 1 || g.Servers != 1 {
		t.Fatalf("global stats: %+v %v", g, err)
	}
	if ps, err := s.ListPlayerStats(ctx, seasonID, 0); err != nil || len(ps) != 1 {
		t.Fatalf("player stats: %v %v", ps, err)
	}
	if pm, err := s.ListPlayerMatches(ctx, "7656"); err != nil || len(pm) != 1 || pm[0].OwnTeam() != "Alpha" {
		t.Fatalf("player matches: %+v %v", pm, err)
	}
	if c, err := s.Counts(ctx); err != nil || c.Matches != 1 || c.Users != 1 || c.Teams != 2 {
		t.Fatalf("counts: %+v %v", c, err)
	}
	// Deleting a match removes its maps and players through the foreign keys.
	if _, err := s.db.Exec("UPDATE matchs SET enable = 0 WHERE id = ?", id); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteMatch(ctx, id); err != nil {
		t.Fatal(err)
	}
	if n := scalar(t, s, "SELECT COUNT(*) FROM players") + scalar(t, s, "SELECT COUNT(*) FROM maps"); n != 0 {
		t.Errorf("%d rows left after deleting the match", n)
	}
}
