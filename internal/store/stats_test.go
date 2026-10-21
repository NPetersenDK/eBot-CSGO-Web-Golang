package store

import (
	"context"
	"os"
	"testing"
)

func matchesTestDSN() (string, bool) {
	v := os.Getenv("EBOT_TEST_DSN")
	return v, v != ""
}

func TestPlayersRoundsAndCounts(t *testing.T) {
	s := matchesTestStore(t)
	ctx := context.Background()
	id := createWithMaps(t, s, "de_dust2", "de_nuke")
	maps, _ := s.ListMaps(ctx, id)
	mustExec(t, s, `INSERT INTO players (match_id, map_id, pseudo, nb_kill, point, nb5kill, bombe, created_at, updated_at)
		VALUES (?, ?, 'one', 10, 20, 1, 2, NOW(), NOW()), (?, ?, 'two', 5, 30, 0, 0, NOW(), NOW())`,
		id, maps[0].ID, id, maps[1].ID)
	mustExec(t, s, "INSERT INTO players (match_id, map_id, created_at, updated_at) VALUES (?, ?, NOW(), NOW())", id, maps[1].ID)
	mustExec(t, s, `INSERT INTO round_summary (match_id, map_id, round_id, team_win, score_a, bomb_planted, created_at, updated_at)
		VALUES (?, ?, 2, 'a', 2, 1, NOW(), NOW()), (?, ?, 1, 'b', 0, 0, NOW(), NOW())`, id, maps[0].ID, id, maps[0].ID)

	all, err := s.ListPlayers(ctx, id, 0)
	if err != nil || len(all) != 3 {
		t.Fatalf("all: %v %v", all, err)
	}
	if all[0].Nickname != "two" || all[0].Team != "other" {
		t.Errorf("order by points: %+v", all[0])
	}
	one, _ := s.ListPlayers(ctx, id, maps[0].ID)
	if len(one) != 1 || one[0].Clutch1v5 != 1 || one[0].Bombs != 2 || one[0].Kills != 10 {
		t.Errorf("map filter: %+v", one)
	}

	rounds, err := s.ListRoundSummaries(ctx, id, maps[0].ID)
	if err != nil || len(rounds) != 2 || rounds[0].RoundID != 1 || !rounds[1].BombPlanted || rounds[1].ScoreA != 2 || rounds[0].BestKillerID != nil {
		t.Errorf("rounds: %+v %v", rounds, err)
	}

	seedServer(t, s, "9.9.9.9:1")
	seedTeam(t, s, "T")
	seedSeason(t, s, "S")
	mustExec(t, s, "INSERT INTO sf_guard_user (username, created_at, updated_at) VALUES ('u', NOW(), NOW())")
	b, _ := s.CreateMatch(ctx, sampleInput("de_dust2"))
	c, _ := s.CreateMatch(ctx, sampleInput("de_dust2"))
	mustExec(t, s, "UPDATE matchs SET status = 6, enable = 1 WHERE id = ?", b)
	mustExec(t, s, "UPDATE matchs SET status = 14 WHERE id = ?", c)
	cnt, err := s.Counts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := Counts{Matches: 2, Running: 1, Archived: 1, Servers: 1, Teams: 1, Seasons: 1, Users: 1}
	if cnt != want {
		t.Errorf("counts %+v want %+v", cnt, want)
	}
}

func TestGlobalStats(t *testing.T) {
	s := matchesTestStore(t)
	ctx := context.Background()

	empty, err := s.GlobalStats(ctx)
	if err != nil || empty.Kills != 0 || empty.Matches() != 0 {
		t.Fatalf("empty database: %+v %v", empty, err)
	}
	if _, ok := empty.HeadshotRate(); ok {
		t.Error("no kills must give no rate")
	}

	notStarted := createWithMaps(t, s, "de_dust2")
	running := createWithMaps(t, s, "de_dust2")
	finished := createWithMaps(t, s, "de_dust2")
	archived := createWithMaps(t, s, "de_dust2")
	stopped := createWithMaps(t, s, "de_dust2")
	mustExec(t, s, "UPDATE matchs SET status = 8, enable = 1 WHERE id = ?", running)
	mustExec(t, s, "UPDATE matchs SET status = 13 WHERE id = ?", finished)
	mustExec(t, s, "UPDATE matchs SET status = 14 WHERE id = ?", archived)
	mustExec(t, s, "UPDATE matchs SET status = 8, enable = 0 WHERE id = ?", stopped)
	_ = notStarted

	maps, _ := s.ListMaps(ctx, finished)
	mustExec(t, s, `INSERT INTO players (match_id, map_id, nb_kill, hs, created_at, updated_at)
		VALUES (?, ?, 10, 4, NOW(), NOW()), (?, ?, 6, 1, NOW(), NOW())`, finished, maps[0].ID, finished, maps[0].ID)
	mustExec(t, s, "INSERT INTO servers (ip, rcon, hostname, created_at, updated_at) VALUES ('10.0.0.1:27015', 'x', 'a', NOW(), NOW())")

	g, err := s.GlobalStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if g.Kills != 16 || g.Headshots != 5 || g.InProgress != 1 || g.NotStarted != 1 || g.Finished != 2 || g.Servers != 1 || g.Matches() != 4 {
		t.Errorf("stats = %+v", g)
	}
	if g.HeadshotPercent() != "31.2%" {
		t.Errorf("percent = %s", g.HeadshotPercent())
	}
}
