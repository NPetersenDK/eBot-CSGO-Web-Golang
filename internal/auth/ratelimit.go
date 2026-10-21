package auth

import (
	"sync"
	"time"
)

// loginLimiter counts failed logins per key in a sliding window. A success
// clears the key. It is in memory and per process.
type loginLimiter struct {
	max    int
	window time.Duration
	now    func() time.Time

	mu        sync.Mutex
	fail      map[string][]time.Time
	lastSweep time.Time
}

func newLoginLimiter(max int, window time.Duration, now func() time.Time) *loginLimiter {
	if now == nil {
		now = time.Now
	}
	return &loginLimiter{max: max, window: window, now: now, fail: map[string][]time.Time{}}
}

func (l *loginLimiter) blocked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	cutoff := now.Add(-l.window)
	l.sweep(now, cutoff)
	fresh := 0
	for _, t := range l.fail[key] {
		if t.After(cutoff) {
			fresh++
		}
	}
	return fresh >= l.max
}

func (l *loginLimiter) recordFail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	cutoff := now.Add(-l.window)
	kept := l.fail[key][:0]
	for _, t := range l.fail[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	l.fail[key] = append(kept, now)
}

func (l *loginLimiter) success(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fail, key)
}

// sweep drops stale keys at most once per window. The caller holds the lock.
func (l *loginLimiter) sweep(now, cutoff time.Time) {
	if !l.lastSweep.IsZero() && now.Sub(l.lastSweep) < l.window {
		return
	}
	l.lastSweep = now
	for k, times := range l.fail {
		if len(times) == 0 || times[len(times)-1].Before(cutoff) {
			delete(l.fail, k)
		}
	}
}
