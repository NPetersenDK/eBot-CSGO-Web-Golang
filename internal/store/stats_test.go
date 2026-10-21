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
