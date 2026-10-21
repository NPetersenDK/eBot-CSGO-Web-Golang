package auth

import (
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	maxTrackedKeys = 10000
	evictShare     = 10
	userKeyPrefix  = "u:"
	ipKeyPrefix    = "i:"
)

// loginLimiter counts login attempts in a sliding window, per username and
// address together and per address alone. It is in memory and per process.
// ipMax 0 turns the per-address limit off: without a trusted proxy every
// visitor behind one looks like the same address, and a limit per address
// would let one person lock everybody out.
type loginLimiter struct {
	max    int
	ipMax  int
	window time.Duration
	now    func() time.Time

	mu       sync.Mutex
	attempts map[string][]time.Time
}

func newLoginLimiter(max, ipMax int, window time.Duration, now func() time.Time) *loginLimiter {
	if now == nil {
		now = time.Now
	}
	return &loginLimiter{max: max, ipMax: ipMax, window: window, now: now, attempts: map[string][]time.Time{}}
}

// allow checks both limits and records the attempt in one step, so concurrent
// attempts cannot all pass. The caller reports the outcome with success or
// refund. An attempt that is never reported counts as a failure.
func (l *loginLimiter) allow(user, ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	cutoff := now.Add(-l.window)
	uk, ik := userKeyPrefix+user+"|"+ip, ipKeyPrefix+ip
	if l.fresh(uk, cutoff) >= l.max || (l.ipMax > 0 && l.fresh(ik, cutoff) >= l.ipMax) {
		return false
	}
	l.add(uk, now, cutoff)
	if l.ipMax > 0 {
		l.add(ik, now, cutoff)
	}
	return true
}

// success clears the username counter and takes the attempt back from the
// address counter, which only counts failures.
func (l *loginLimiter) success(user, ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, userKeyPrefix+user+"|"+ip)
	l.dropLast(ipKeyPrefix + ip)
}

// refund takes back an attempt that did not reach a verdict.
func (l *loginLimiter) refund(user, ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.dropLast(userKeyPrefix + user + "|" + ip)
	l.dropLast(ipKeyPrefix + ip)
}

func (l *loginLimiter) fresh(key string, cutoff time.Time) int {
	n := 0
	for _, t := range l.attempts[key] {
		if t.After(cutoff) {
			n++
		}
	}
	return n
}

func (l *loginLimiter) dropLast(key string) {
	times := l.attempts[key]
	if len(times) <= 1 {
		delete(l.attempts, key)
		return
	}
	l.attempts[key] = times[:len(times)-1]
}

// add records an attempt. At the key cap it drops expired keys first, then keys
// that are not at their limit (oldest first), and only then blocked keys, so
// flooding the table with new names does not free a key that is locked out.
// The caller holds the lock.
func (l *loginLimiter) add(key string, now, cutoff time.Time) {
	times, known := l.attempts[key]
	if !known && len(l.attempts) >= maxTrackedKeys {
		l.evict(cutoff)
	}
	kept := times[:0]
	for _, t := range times {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	l.attempts[key] = append(kept, now)
}

func (l *loginLimiter) evict(cutoff time.Time) {
	for k, times := range l.attempts {
		if len(times) == 0 || !times[len(times)-1].After(cutoff) {
			delete(l.attempts, k)
		}
	}
	if len(l.attempts) < maxTrackedKeys {
		return
	}
	type aged struct {
		key     string
		last    time.Time
		blocked bool
	}
	all := make([]aged, 0, len(l.attempts))
	for k, times := range l.attempts {
		limit := l.max
		if strings.HasPrefix(k, ipKeyPrefix) {
			limit = l.ipMax
		}
		all = append(all, aged{k, times[len(times)-1], len(times) >= limit})
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].blocked != all[j].blocked {
			return !all[i].blocked
		}
		return all[i].last.Before(all[j].last)
	})
	for _, a := range all[:maxTrackedKeys/evictShare] {
		delete(l.attempts, a.key)
	}
}
