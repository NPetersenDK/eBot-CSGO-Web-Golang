package web

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/store"
)

type countingStats struct {
	matchStore
	global, players int
	err             error
}

func (s *countingStats) GlobalStats(context.Context) (store.GlobalStats, error) {
	s.global++
	return store.GlobalStats{Kills: int64(s.global)}, s.err
}

func (s *countingStats) ListPlayerStats(_ context.Context, season int64, _ int) ([]store.PlayerStat, error) {
	s.players++
	return []store.PlayerStat{{Kills: season}}, s.err
}

func TestStatsCache(t *testing.T) {
	ctx := context.Background()
	st := &countingStats{}
	clock := time.Unix(1000, 0)
	c := newStatsCache()
	c.now = func() time.Time { return clock }

	for range 3 {
		if g, _ := c.Global(ctx, st); g.Kills != 1 {
			t.Fatalf("global = %+v", g)
		}
		c.Players(ctx, st, 1, 10)
	}
	if st.global != 1 || st.players != 1 {
		t.Fatalf("store hit %d/%d times", st.global, st.players)
	}
	c.Players(ctx, st, 2, 10)
	c.Players(ctx, st, 1, 0)
	if st.players != 3 {
		t.Errorf("distinct keys must miss, got %d reads", st.players)
	}

	clock = clock.Add(statsCacheTTL + time.Second)
	if g, _ := c.Global(ctx, st); g.Kills != 2 {
		t.Errorf("expired global not refreshed: %+v", g)
	}
	c.Players(ctx, st, 1, 10)
	if st.players != 4 {
		t.Errorf("expired players not refreshed: %d", st.players)
	}
}

func TestStatsCacheDoesNotKeepErrors(t *testing.T) {
	ctx := context.Background()
	st := &countingStats{err: errors.New("down")}
	c := newStatsCache()
	if _, err := c.Global(ctx, st); err == nil {
		t.Fatal("error swallowed")
	}
	st.err = nil
	if g, err := c.Global(ctx, st); err != nil || g.Kills != 2 {
		t.Errorf("after recovery: %+v %v", g, err)
	}
}

func TestStatsCacheBounded(t *testing.T) {
	st := &countingStats{}
	c := newStatsCache()
	for i := range statsCacheEntries * 3 {
		c.Players(context.Background(), st, int64(i), 0)
	}
	if len(c.players) > statsCacheEntries {
		t.Errorf("%d entries", len(c.players))
	}
}

func TestNilStatsCache(t *testing.T) {
	var c *statsCache
	st := &countingStats{}
	c.Global(context.Background(), st)
	c.Global(context.Background(), st)
	if st.global != 2 {
		t.Errorf("nil cache must pass through, got %d", st.global)
	}
}

func TestStatsCacheEvictsTheOldestNotEverything(t *testing.T) {
	ctx := context.Background()
	now := time.Unix(1_700_000_000, 0)
	c := newStatsCache()
	c.now = func() time.Time { return now }
	st := &countingStats{}
	for i := int64(0); i < statsCacheEntries; i++ {
		now = now.Add(100 * time.Millisecond)
		if _, err := c.Players(ctx, st, i, 0); err != nil {
			t.Fatal(err)
		}
	}
	now = now.Add(100 * time.Millisecond)
	if _, err := c.Players(ctx, st, 1000, 0); err != nil {
		t.Fatal(err)
	}
	if len(c.players) != statsCacheEntries {
		t.Fatalf("entries = %d, want %d", len(c.players), statsCacheEntries)
	}
	before := st.players
	if _, err := c.Players(ctx, st, statsCacheEntries-1, 0); err != nil || st.players != before {
		t.Error("a recent entry was dropped together with the oldest")
	}
	if _, err := c.Players(ctx, st, 0, 0); err != nil || st.players != before+1 {
		t.Error("the oldest entry should have been evicted")
	}
}

func TestStatsCacheRunsOneQueryForConcurrentMisses(t *testing.T) {
	ctx := context.Background()
	c := newStatsCache()
	st := &countingStats{}
	var wg sync.WaitGroup
	for range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = c.Players(ctx, st, 7, 0)
			_, _ = c.Global(ctx, st)
		}()
	}
	wg.Wait()
	if st.players != 1 || st.global != 1 {
		t.Errorf("queries run: players %d, global %d, want 1 each", st.players, st.global)
	}
}
