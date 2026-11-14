// Package authlimit counts sign-in attempts in memory.
// A new process starts fresh, same as the existing IP limiter.
package authlimit

import (
	"sync"
	"time"
)

// Limiter is a fixed-window counter plus a failure lockout.
type Limiter struct {
	mu    sync.Mutex
	now   func() time.Time
	hits  map[string]*window
	fails map[string]*failure
}

type window struct {
	start time.Time
	n     int
}

type failure struct {
	n     int
	until time.Time
}

// New returns a limiter that uses the wall clock.
func New() *Limiter {
	return &Limiter{
		now:   time.Now,
		hits:  map[string]*window{},
		fails: map[string]*failure{},
	}
}

// Allow records one hit and reports whether it is still inside the window.
func (l *Limiter) Allow(key string, limit int, windowSize time.Duration) bool {
	if limit <= 0 || windowSize <= 0 {
		return true
	}
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	w := l.hits[key]
	if w == nil || now.Sub(w.start) >= windowSize {
		l.hits[key] = &window{start: now, n: 1}
		return true
	}
	if w.n >= limit {
		return false
	}
	w.n++
	return true
}

// Locked reports an active failure lock.
func (l *Limiter) Locked(key string) bool {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	f := l.fails[key]
	return f != nil && !f.until.IsZero() && now.Before(f.until)
}

// Fail counts a bad attempt and locks the key once max is reached.
func (l *Limiter) Fail(key string, max int, lock time.Duration) {
	if max <= 0 || lock <= 0 {
		return
	}
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	f := l.fails[key]
	if f == nil || (!f.until.IsZero() && !now.Before(f.until)) {
		f = &failure{}
	}
	if !f.until.IsZero() && now.Before(f.until) {
		return
	}
	f.n++
	if f.n >= max {
		f.until = now.Add(lock)
	}
	l.fails[key] = f
}

// Clear forgets failures after a successful password check.
func (l *Limiter) Clear(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, key)
}
