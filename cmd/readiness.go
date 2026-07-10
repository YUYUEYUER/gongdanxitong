package main

import (
	"context"
	"sync"
	"time"
)

const (
	readinessCacheTTL     = 2 * time.Second
	readinessProbeTimeout = 2 * time.Second
)

type readinessResultCache struct {
	mu        sync.Mutex
	ttl       time.Duration
	expiresAt time.Time
	ready     bool
	running   bool
	done      chan struct{}
}

func newReadinessResultCache(ttl time.Duration) *readinessResultCache {
	return &readinessResultCache{ttl: ttl}
}

// check returns a fresh cached result or coalesces callers behind the single
// dependency probe already in progress.
func (c *readinessResultCache) check(ctx context.Context, probe func(context.Context) bool) bool {
	if c == nil || c.ttl <= 0 || probe == nil {
		return false
	}

	for {
		now := time.Now()
		c.mu.Lock()
		if !c.expiresAt.IsZero() && now.Before(c.expiresAt) {
			ready := c.ready
			c.mu.Unlock()
			return ready
		}
		if c.running {
			done := c.done
			c.mu.Unlock()
			select {
			case <-done:
				continue
			case <-ctx.Done():
				return false
			}
		}

		c.running = true
		c.done = make(chan struct{})
		c.mu.Unlock()

		ready := probe(ctx)

		c.mu.Lock()
		c.ready = ready
		c.expiresAt = time.Now().Add(c.ttl)
		c.running = false
		close(c.done)
		c.mu.Unlock()
		return ready
	}
}

var readinessCaches sync.Map // map[*App]*readinessResultCache

func readinessCacheFor(app *App) *readinessResultCache {
	cache, _ := readinessCaches.LoadOrStore(app, newReadinessResultCache(readinessCacheTTL))
	return cache.(*readinessResultCache)
}

func dependenciesReady(app *App) bool {
	if app == nil || app.db == nil || app.redis == nil {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), readinessProbeTimeout)
	defer cancel()
	return readinessCacheFor(app).check(ctx, func(probeCtx context.Context) bool {
		if err := app.db.PingContext(probeCtx); err != nil {
			return false
		}
		return app.redis.Ping(probeCtx).Err() == nil
	})
}
