package ratelimit

import (
	"context"
	"fmt"
	"strconv"
	"sync/atomic"
	"time"

	realip "github.com/ferluci/fast-realip"
	"github.com/redis/go-redis/v9"
	"github.com/valyala/fasthttp"
)

// Rule defines a rate limiting rule for a named group of endpoints.
type Rule struct {
	Name              string
	Enabled           bool
	RequestsPerMinute int
}

// Limiter handles rate limiting using Redis.
type Limiter struct {
	redis *redis.Client
	rules map[string]Rule
}

var windowMemberSeq atomic.Uint64

type WindowResult struct {
	Allowed    bool
	Limit      int
	Remaining  int
	RetryAfter time.Duration
}

// New creates a new rate limiter.
func New(redisClient *redis.Client) *Limiter {
	return &Limiter{
		redis: redisClient,
		rules: make(map[string]Rule),
	}
}

// AddRule registers a named rate limiting rule.
func (l *Limiter) AddRule(rule Rule) {
	l.rules[rule.Name] = rule
}

// Check checks if the request should be rate limited for the given rule.
func (l *Limiter) Check(ctx *fasthttp.RequestCtx, ruleName string) error {
	rule, ok := l.rules[ruleName]
	if !ok || !rule.Enabled {
		return nil
	}

	clientIP := realip.FromRequest(ctx)
	key := fmt.Sprintf("rate_limit:%s:%s", ruleName, clientIP)

	now := time.Now()
	nowUnix := now.Unix()
	nowNano := now.UnixNano()
	windowStart := strconv.FormatInt(nowUnix-60, 10)

	// Single pipeline: cleanup, add, count, set expiry.
	pipe := l.redis.Pipeline()
	pipe.ZRemRangeByScore(ctx, key, "-inf", windowStart)
	pipe.ZAdd(ctx, key, redis.Z{Score: float64(nowUnix), Member: nowNano})
	countCmd := pipe.ZCard(ctx, key)
	pipe.Expire(ctx, key, time.Minute*2)
	if _, err := pipe.Exec(ctx); err != nil {
		return nil
	}

	count := countCmd.Val()
	limit := int64(rule.RequestsPerMinute)

	ctx.Response.Header.Set("X-RateLimit-Limit", strconv.Itoa(rule.RequestsPerMinute))
	ctx.Response.Header.Set("X-RateLimit-Reset", strconv.FormatInt(nowUnix+60, 10))

	if count > limit {
		ctx.Response.Header.Set("X-RateLimit-Remaining", "0")
		ctx.Response.Header.Set("Retry-After", "60")
		ctx.Response.Header.Set("Content-Type", "application/json")
		ctx.SetStatusCode(fasthttp.StatusTooManyRequests)
		ctx.SetBodyString(`{"status":"error","message":"Rate limit exceeded"}`)
		return fmt.Errorf("rate limit exceeded")
	}

	remaining := max(int(limit-count), 0)
	ctx.Response.Header.Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
	return nil
}

// CheckWindow applies a fixed custom window limit for a fully-qualified key.
func (l *Limiter) CheckWindow(ctx context.Context, key string, window time.Duration, maxAttempts int) (WindowResult, error) {
	result := WindowResult{
		Allowed:   true,
		Limit:     maxAttempts,
		Remaining: max(maxAttempts-1, 0),
	}
	if l == nil || l.redis == nil || key == "" || window <= 0 || maxAttempts <= 0 {
		return result, nil
	}

	now := time.Now()
	nowMilli := now.UnixMilli()
	windowStart := strconv.FormatInt(now.Add(-window).UnixMilli(), 10)

	pipe := l.redis.Pipeline()
	pipe.ZRemRangeByScore(ctx, key, "-inf", windowStart)
	pipe.ZAdd(ctx, key, redis.Z{Score: float64(nowMilli), Member: fmt.Sprintf("%d:%d", now.UnixNano(), windowMemberSeq.Add(1))})
	countCmd := pipe.ZCard(ctx, key)
	pipe.Expire(ctx, key, window+time.Minute)
	if _, err := pipe.Exec(ctx); err != nil {
		return result, err
	}

	count := int(countCmd.Val())
	result.Remaining = max(maxAttempts-count, 0)
	if count > maxAttempts {
		result.Allowed = false
		result.Remaining = 0
		result.RetryAfter = window
	}

	return result, nil
}
