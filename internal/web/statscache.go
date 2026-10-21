package web

import (
	"context"
	"sync"
	"time"

	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/store"
)

const (
	statsCacheTTL     = 30 * time.Second
	statsCacheEntries = 64
)

type playersKey struct {
	season int64
	limit  int
}

type playersEntry struct {
	at   time.Time
	rows []store.PlayerStat
}

// statsCache keeps the public statistics in memory for a short time. A nil
// cache reads straight from the store. A miss is computed by one request at a
// time, so many visitors arriving after the entry expired run the query once.
type statsCache struct {
	now func() time.Time

	load     sync.Mutex
	mu       sync.Mutex
	globalAt time.Time
	global   store.GlobalStats
	players  map[playersKey]playersEntry
}

func newStatsCache() *statsCache {
	return &statsCache{now: time.Now, players: map[playersKey]playersEntry{}}
}

func (c *statsCache) fresh(at time.Time) bool {
	return !at.IsZero() && c.now().Sub(at) < statsCacheTTL
}

func (c *statsCache) cachedGlobal() (store.GlobalStats, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.global, c.fresh(c.globalAt)
}

func (c *statsCache) Global(ctx context.Context, st matchStore) (store.GlobalStats, error) {
	if c == nil {
		return st.GlobalStats(ctx)
	}
	if g, ok := c.cachedGlobal(); ok {
		return g, nil
	}
	c.load.Lock()
	defer c.load.Unlock()
	if g, ok := c.cachedGlobal(); ok {
		return g, nil
	}
	g, err := st.GlobalStats(ctx)
	if err != nil {
		return g, err
	}
	c.mu.Lock()
	c.global, c.globalAt = g, c.now()
	c.mu.Unlock()
	return g, nil
}

func (c *statsCache) cachedPlayers(key playersKey) ([]store.PlayerStat, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.players[key]
	return e.rows, ok && c.fresh(e.at)
}

func (c *statsCache) Players(ctx context.Context, st matchStore, season int64, limit int) ([]store.PlayerStat, error) {
	if c == nil {
		return st.ListPlayerStats(ctx, season, limit)
	}
	key := playersKey{season, limit}
	if rows, ok := c.cachedPlayers(key); ok {
		return rows, nil
	}
	c.load.Lock()
	defer c.load.Unlock()
	if rows, ok := c.cachedPlayers(key); ok {
		return rows, nil
	}
	rows, err := st.ListPlayerStats(ctx, season, limit)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	if len(c.players) >= statsCacheEntries {
		c.evictOldest()
	}
	c.players[key] = playersEntry{at: c.now(), rows: rows}
	c.mu.Unlock()
	return rows, nil
}

// evictOldest drops the entry that was stored first. The caller holds mu.
func (c *statsCache) evictOldest() {
	var oldest playersKey
	var at time.Time
	first := true
	for k, e := range c.players {
		if first || e.at.Before(at) {
			oldest, at, first = k, e.at, false
		}
	}
	delete(c.players, oldest)
}
