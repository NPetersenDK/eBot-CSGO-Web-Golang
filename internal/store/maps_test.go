package store

import (
	"context"
	"errors"
	"testing"
)

func TestUpdateMapScoreRecomputes(t *testing.T) {
	s := matchesTestStore(t)
	ctx := context.Background()
	id := createWithMaps(t, s, "de_dust2", "de_nuke")
	maps, _ := s.ListMaps(ctx, id)
	for _, mp := range []struct {
		mapID int64
		a, b  int
	}{{maps[0].ID, 8, 7}, {maps[0].ID, 3, 1}, {maps[1].ID, 5, 5}} {
		mustExec(t, s, "INSERT INTO maps_score (map_id, type_score, score1_side1, score1_side2, score2_side1, score2_side2, created_at, updated_at) VALUES (?, 'normal', ?, 0, ?, 0, NOW(), NOW())",
			mp.mapID, mp.a, mp.b)
	}
	scores, err := s.ListMapScores(ctx, maps[0].ID)
	if err != nil || len(scores) != 2 {
		t.Fatalf("scores: %v %v", scores, err)
	}

	if err := s.UpdateMapScore(ctx, scores[0].ID, MapScoreInput{Score1Side1: 10, Score1Side2: 2, Score2Side1: 4, Score2Side2: 3}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetMapScore(ctx, scores[0].ID)
	if got.Score1Side1 != 10 || got.Score2Side2 != 3 || got.Type != "normal" {
		t.Errorf("score row: %+v", got)
	}
	mp, _ := s.GetMap(ctx, maps[0].ID)
	if mp.Score1 != 10+2+3 || mp.Score2 != 4+3+1 {
		t.Errorf("map score %d-%d", mp.Score1, mp.Score2)
	}
	m, _ := s.GetMatch(ctx, id)
	if m.ScoreA != 15 || m.ScoreB != 8 {
		t.Errorf("match score %d-%d", m.ScoreA, m.ScoreB)
	}

	other, _ := s.ListMapScores(ctx, maps[1].ID)
	if err := s.UpdateMapScore(ctx, other[0].ID, MapScoreInput{Score1Side1: 9}); err != nil {
		t.Fatal(err)
	}
	m, _ = s.GetMatch(ctx, id)
	if m.ScoreA != 15 || m.ScoreB != 8 {
		t.Errorf("match score follows the current map only: %d-%d", m.ScoreA, m.ScoreB)
	}
	mp2, _ := s.GetMap(ctx, maps[1].ID)
	if mp2.Score1 != 9 {
		t.Errorf("second map score %d", mp2.Score1)
	}

	mustExec(t, s, "UPDATE matchs SET enable = 1 WHERE id = ?", id)
	if err := s.UpdateMapScore(ctx, scores[0].ID, MapScoreInput{}); !errors.Is(err, ErrMatchEnabled) {
		t.Errorf("enabled: %v", err)
	}
	if err := s.UpdateMapScore(ctx, 9999, MapScoreInput{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing: %v", err)
	}
	if _, err := s.GetMapScore(ctx, 9999); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing score: %v", err)
	}
	if _, err := s.GetMap(ctx, 9999); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing map: %v", err)
	}
}
