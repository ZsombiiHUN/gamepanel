package api

import (
	"sync"
	"time"
)

// limiter is a small sliding-window rate limiter keyed by string (client IP,
// account email).
type limiter struct {
	mu   sync.Mutex
	max  int
	win  time.Duration
	hits map[string][]time.Time
}

// sweepAbove is how many keys the map may hold before stale ones are dropped.
const sweepAbove = 4096

func newLimiter(max int, win time.Duration) *limiter {
	return &limiter{max: max, win: win, hits: map[string][]time.Time{}}
}

// allow records a hit for key and reports whether it is within the limit.
// A refused attempt is not recorded, so a client that keeps hammering is let
// back in once its earlier hits leave the window.
func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.recent(key)) >= l.max {
		return false
	}
	l.hits[key] = append(l.hits[key], time.Now())
	return true
}

// over reports whether key already has max recorded hits in the window,
// without recording a new one.
func (l *limiter) over(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.recent(key)) >= l.max
}

// add records one hit for key.
func (l *limiter) add(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.hits[key] = append(l.recent(key), time.Now())
}

func (l *limiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.hits, key)
}

// recent returns key's hits inside the window (caller holds the lock). Every
// entry point goes through it, and it sweeps keys with no recent hits once the
// map grows, so a stream of distinct IPs cannot grow it without bound.
func (l *limiter) recent(key string) []time.Time {
	cut := time.Now().Add(-l.win)
	if len(l.hits) > sweepAbove {
		for k, v := range l.hits {
			if len(v) == 0 || !v[len(v)-1].After(cut) {
				delete(l.hits, k)
			}
		}
	}
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(l.hits, key)
		return nil
	}
	l.hits[key] = kept
	return kept
}
