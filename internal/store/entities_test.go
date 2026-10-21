package store

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/auth"
)

//go:embed testdata/schema_entities.sql
var schemaEntities string

func TestExpandServers(t *testing.T) {
	cases := []struct {
		name string
		in   ServerCreate
		want []newServer
		err  bool
	}{
		{"single", ServerCreate{Mode: "single", Host: "10.0.0.1", Port: 27015, Hostname: "A"}, []newServer{{"10.0.0.1:27015", "A"}}, false},
		{"single bad host", ServerCreate{Mode: "single", Host: "nope", Port: 27015}, nil, true},
		{"hostname", ServerCreate{Mode: "hostname", Host: "gs.example.com", Port: 27016, Hostname: "H"}, []newServer{{"gs.example.com:27016", "H"}}, false},
		{"hostname bad", ServerCreate{Mode: "hostname", Host: "10.0.0.1", Port: 1}, nil, true},
		{"range", ServerCreate{Mode: "range", Host: "10.0.0.1", Port: 27015, PortEnd: 27017, Hostname: "R"},
			[]newServer{{"10.0.0.1:27015", "R #1"}, {"10.0.0.1:27016", "R #2"}, {"10.0.0.1:27017", "R #3"}}, false},
		{"range hosts and ports", ServerCreate{Mode: "range", Host: "10.0.0.1-2", Port: 1, PortEnd: 2, Hostname: "R"},
			[]newServer{{"10.0.0.1:1", "R #1"}, {"10.0.0.1:2", "R #2"}, {"10.0.0.2:1", "R #3"}, {"10.0.0.2:2", "R #4"}}, false},
		{"range reversed", ServerCreate{Mode: "range", Host: "10.0.0.1", Port: 5, PortEnd: 4}, nil, true},
		{"unknown mode", ServerCreate{Mode: "x"}, nil, true},
	}
	for _, c := range cases {
		got, err := expandServers(c.in)
		if (err != nil) != c.err {
			t.Fatalf("%s: err %v", c.name, err)
		}
		if !c.err && !reflect.DeepEqual(got, c.want) {
			t.Fatalf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func entityStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("EBOT_TEST_DSN")
	if dsn == "" {
		t.Skip("EBOT_TEST_DSN not set")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, stmt := range strings.Split(schemaEntities, ";\n") {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("schema: %v\n%s", err, stmt)
		}
	}
	return New(db)
}

func TestEntityServers(t *testing.T) {
	s := entityStore(t)
	ctx := context.Background()
	n, err := s.CreateServers(ctx, ServerCreate{Mode: "range", Host: "10.0.0.1", Port: 27015, PortEnd: 27017, RCON: "pw", Hostname: "S", TVIP: "10.0.0.1:27020"})
	if err != nil || n != 3 {
		t.Fatalf("range: %d %v", n, err)
	}
	if n, err = s.CreateServers(ctx, ServerCreate{Mode: "single", Host: "10.0.0.9", Port: 1, RCON: "pw", Hostname: "One"}); err != nil || n != 1 {
		t.Fatalf("single: %d %v", n, err)
	}
	if n, err = s.CreateServers(ctx, ServerCreate{Mode: "hostname", Host: "gs.example.com", Port: 2, RCON: "pw", Hostname: "Host"}); err != nil || n != 1 {
		t.Fatalf("hostname: %d %v", n, err)
	}
	list, err := s.ListServers(ctx)
	if err != nil || len(list) != 5 {
		t.Fatalf("list: %d %v", len(list), err)
	}
	if list[0].IP != "10.0.0.1:27015" || list[0].Hostname != "S #1" || list[0].TVIP != "10.0.0.1:27020" || list[0].CreatedAt.IsZero() {
		t.Fatalf("first: %+v", list[0])
	}
	if _, err := s.db.Exec(`INSERT INTO matchs (server_id, enable, status) VALUES (?, 1, 6)`, list[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteServer(ctx, list[0].ID); !errors.Is(err, ErrServerBusy) {
		t.Fatalf("delete busy: %v", err)
	}
	list, _ = s.ListServers(ctx)
	if !list[0].InUse || list[1].InUse {
		t.Fatal("InUse wrong")
	}
	free, err := s.FreeServers(ctx)
	if err != nil || len(free) != 4 {
		t.Fatalf("free: %d %v", len(free), err)
	}
	if err := s.UpdateServer(ctx, free[0].ID, ServerInput{IP: "1.1.1.1:1", RCON: "x", Hostname: "New"}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetServer(ctx, free[0].ID)
	if got.IP != "1.1.1.1:1" || got.TVIP != "" {
		t.Fatalf("update: %+v", got)
	}
	if err := s.UpdateServer(ctx, free[0].ID, ServerInput{IP: "1.1.1.1:1", RCON: "x", Hostname: `N"ew`}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad server name: %v", err)
	}
	if err := s.UpdateServer(ctx, free[0].ID, ServerInput{IP: `1.1.1.1";x:1`, RCON: "x", Hostname: "New"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad server ip: %v", err)
	}
	if err := s.DeleteServer(ctx, free[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetServer(ctx, free[0].ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get deleted: %v", err)
	}
	s.db.Exec(`DELETE FROM matchs`)
}

func TestEntityTeamsSeasonsAds(t *testing.T) {
	s := entityStore(t)
	ctx := context.Background()
	s1, err := s.CreateSeason(ctx, SeasonInput{Name: "S1", Event: "E", Start: "2025-01-01 10:00:00", End: "2025-02-01 10:00:00", Active: true})
	if err != nil {
		t.Fatal(err)
	}
	s2, _ := s.CreateSeason(ctx, SeasonInput{Name: "S2", Event: "E", Start: "2025-03-01 00:00:00", End: "2025-04-01 00:00:00", Link: "http://x", Active: true})
	se, err := s.GetSeason(ctx, s1)
	if err != nil || se.Start != "2025-01-01 10:00:00" || !se.Active {
		t.Fatalf("season: %+v %v", se, err)
	}

	tid, err := s.CreateTeam(ctx, TeamInput{Name: "Alpha", ShortName: "A", Flag: "DK", SeasonIDs: []int64{s1, s2, s1}})
	if err != nil {
		t.Fatal(err)
	}
	other, _ := s.CreateTeam(ctx, TeamInput{Name: "Beta", ShortName: "B", Flag: "SE", SeasonIDs: []int64{s2}})
	tm, err := s.GetTeam(ctx, tid)
	if err != nil || !reflect.DeepEqual(tm.SeasonIDs, []int64{s1, s2}) {
		t.Fatalf("team: %+v %v", tm, err)
	}
	by, _ := s.ListTeamsBySeason(ctx, s2)
	if len(by) != 2 {
		t.Fatalf("by season: %d", len(by))
	}
	if err := s.UpdateTeam(ctx, tid, TeamInput{Name: "Alpha2", ShortName: "A", Flag: "DK", SeasonIDs: []int64{s1}}); err != nil {
		t.Fatal(err)
	}
	by, _ = s.ListTeamsBySeason(ctx, s2)
	if len(by) != 1 || by[0].ID != other {
		t.Fatalf("after update: %+v", by)
	}
	if err := s.UpdateTeam(ctx, 9999, TeamInput{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update missing: %v", err)
	}
	if _, err := s.CreateTeam(ctx, TeamInput{Name: `Bad"; quit`, ShortName: "X", Flag: "DK"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("create bad team name: %v", err)
	}
	if err := s.UpdateTeam(ctx, tid, TeamInput{Name: "A;B", ShortName: "A", Flag: "DK"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("update bad team name: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE teams SET name = ? WHERE id = ?`, `Legacy"name`, tid); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTeam(ctx, tid, TeamInput{Name: `Legacy"name`, ShortName: "A2", Flag: "DK"}); err != nil {
		t.Fatalf("unchanged legacy name: %v", err)
	}

	aid, err := s.CreateAd(ctx, AdInput{SeasonID: s1, Message: "hello", Active: true})
	if err != nil {
		t.Fatal(err)
	}
	ads, _ := s.ListAds(ctx)
	if len(ads) != 1 || ads[0].Season != "S1" || !ads[0].Active {
		t.Fatalf("ads: %+v", ads)
	}
	if err := s.DeactivateAd(ctx, aid); err != nil {
		t.Fatal(err)
	}
	if ad, _ := s.GetAd(ctx, aid); ad.Active {
		t.Fatal("ad still active")
	}
	if err := s.DeactivateSeason(ctx, s1); err != nil {
		t.Fatal(err)
	}
	if se, _ := s.GetSeason(ctx, s1); se.Active {
		t.Fatal("season still active")
	}

	if err := s.DeleteTeam(ctx, tid); err != nil {
		t.Fatal(err)
	}
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM teams_in_seasons WHERE team_id = ?`, tid).Scan(&n)
	if n != 0 {
		t.Fatal("teams_in_seasons not cleaned")
	}
	if err := s.DeleteSeason(ctx, s1); err != nil {
		t.Fatal(err)
	}
	if ads, _ := s.ListAds(ctx); len(ads) != 0 {
		t.Fatal("ads not cleaned")
	}
	if err := s.DeleteSeason(ctx, s1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete twice: %v", err)
	}
}

func TestEntityUsers(t *testing.T) {
	s := entityStore(t)
	ctx := context.Background()
	if _, err := s.CreateUser(ctx, UserInput{Username: "x", Email: "x@x"}); err == nil {
		t.Fatal("password should be required")
	}
	id, err := s.CreateUser(ctx, UserInput{FirstName: "Ada", LastName: "Lovelace", Email: "ada@x.dk", Username: "ada", Password: "pw1", IsActive: true, IsSuperAdmin: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateUser(ctx, UserInput{Email: "other@x.dk", Username: "ada", Password: "p"}); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("dup username: %v", err)
	}
	if _, err := s.CreateUser(ctx, UserInput{Email: "ada@x.dk", Username: "ada2", Password: "p"}); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("dup email: %v", err)
	}

	var salt, hash, alg string
	s.db.QueryRow(`SELECT salt, password, algorithm FROM sf_guard_user WHERE id = ?`, id).Scan(&salt, &hash, &alg)
	if alg != "sha1" || hash != HashPassword(salt, "pw1") {
		t.Fatal("stored hash is not PHP compatible")
	}

	sess, err := s.VerifyLocal(ctx, "ada", "pw1")
	if err != nil || sess.Subject != "ada" || sess.Name != "Ada Lovelace" || sess.Role != auth.RoleAdmin || sess.Source != auth.SourceLocal {
		t.Fatalf("verify: %+v %v", sess, err)
	}
	if u, _ := s.GetUser(ctx, id); u.LastLogin == nil {
		t.Fatal("last_login not set")
	}
	for _, c := range [][2]string{{"ada", "wrong"}, {"nobody", "pw1"}} {
		if _, err := s.VerifyLocal(ctx, c[0], c[1]); !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Fatalf("%v: %v", c, err)
		}
	}

	if err := s.UpdateUser(ctx, id, UserInput{Email: "ada@x.dk", Username: "ada", IsActive: true, IsSuperAdmin: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.VerifyLocal(ctx, "ada", "pw1"); err != nil {
		t.Fatalf("password changed on empty update: %v", err)
	}
	if err := s.UpdateUser(ctx, id, UserInput{Email: "ada@x.dk", Username: "ada", Password: "pw2", IsActive: false, IsSuperAdmin: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.VerifyLocal(ctx, "ada", "pw2"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("inactive user logged in: %v", err)
	}
	if ok, _ := s.IsAdmin(ctx, "ada"); ok {
		t.Fatal("inactive user is admin")
	}
	s.UpdateUser(ctx, id, UserInput{Email: "ada@x.dk", Username: "ada", IsActive: true, IsSuperAdmin: true})
	if _, err := s.VerifyLocal(ctx, "ada", "pw2"); err != nil {
		t.Fatalf("pw2: %v", err)
	}

	bob, _ := s.CreateUser(ctx, UserInput{Email: "bob@x.dk", Username: "bob", Password: "pw", IsActive: true})
	if _, err := s.VerifyLocal(ctx, "bob", "pw"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("non-admin logged in: %v", err)
	}
	if ok, _ := s.IsAdmin(ctx, "bob"); ok {
		t.Fatal("bob is admin")
	}
	s.db.Exec(`INSERT INTO sf_guard_permission (id, name, created_at, updated_at) VALUES (1, 'admin', NOW(), NOW())`)
	s.db.Exec(`INSERT INTO sf_guard_group (id, name, created_at, updated_at) VALUES (1, 'admins', NOW(), NOW())`)
	s.db.Exec(`INSERT INTO sf_guard_group_permission VALUES (1, 1, NOW(), NOW())`)
	s.db.Exec(`INSERT INTO sf_guard_user_group VALUES (?, 1, NOW(), NOW())`, bob)
	if ok, err := s.IsAdmin(ctx, "bob"); err != nil || !ok {
		t.Fatalf("group admin: %v %v", ok, err)
	}
	if _, err := s.VerifyLocal(ctx, "bob", "pw"); err != nil {
		t.Fatalf("group admin login: %v", err)
	}
	if ok, err := s.IsAdmin(ctx, "ghost"); ok || err != nil {
		t.Fatalf("ghost: %v %v", ok, err)
	}

	s.db.Exec(`UPDATE sf_guard_user SET algorithm = 'md5' WHERE id = ?`, id)
	if _, err := s.VerifyLocal(ctx, "ada", "pw2"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("md5 algorithm accepted: %v", err)
	}

	if n, _ := s.CountUsers(ctx); n != 2 {
		t.Fatalf("count %d", n)
	}
	if l, _ := s.ListUsers(ctx); len(l) != 2 {
		t.Fatalf("list %d", len(l))
	}
	if err := s.DeleteUser(ctx, bob); err != nil {
		t.Fatal(err)
	}
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM sf_guard_user_group`).Scan(&n)
	if n != 0 {
		t.Fatal("group rows left")
	}
	if err := s.DeleteUser(ctx, bob); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete twice: %v", err)
	}
}
