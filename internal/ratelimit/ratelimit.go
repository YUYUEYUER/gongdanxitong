package ratelimit

import (
	"context"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

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
	redis          *redis.Client
	rules          map[string]Rule
	trustedProxies []netip.Prefix
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

// SetTrustedProxies configures the only network peers whose forwarding headers
// may influence client IP resolution.
func (l *Limiter) SetTrustedProxies(cidrs []string) error {
	prefixes := make([]netip.Prefix, 0, len(cidrs))
	for _, raw := range cidrs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(raw)
		if err != nil {
			addr, addrErr := netip.ParseAddr(raw)
			if addrErr != nil {
				return fmt.Errorf("invalid trusted proxy %q: %w", raw, err)
			}
			bits := 128
			if addr.Is4() {
				bits = 32
			}
			prefix = netip.PrefixFrom(addr, bits)
		}
		if prefix.Bits() == 0 {
			return fmt.Errorf("trusted proxy %q is too broad", raw)
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	l.trustedProxies = prefixes
	return nil
}

// ClientIP returns the socket peer unless that peer is explicitly trusted. For
// trusted peers it walks X-Forwarded-For from right to left and stops at the
// first untrusted hop. X-Client-IP is intentionally ignored.
func (l *Limiter) ClientIP(ctx *fasthttp.RequestCtx) string {
	remote, ok := netip.AddrFromSlice(ctx.RemoteIP())
	if !ok {
		return "unknown"
	}
	remote = remote.Unmap()
	if l == nil {
		return remote.String()
	}
	if !l.isTrustedProxy(remote) {
		return remote.String()
	}

	if raw := strings.TrimSpace(string(ctx.Request.Header.Peek("X-Forwarded-For"))); raw != "" {
		chain := strings.Split(raw, ",")
		if len(chain) > 32 {
			return remote.String()
		}
		current := remote
		for i := len(chain) - 1; i >= 0 && l.isTrustedProxy(current); i-- {
			next, err := netip.ParseAddr(strings.TrimSpace(chain[i]))
			if err != nil {
				return remote.String()
			}
			current = next.Unmap()
		}
		return current.String()
	}

	if raw := strings.TrimSpace(string(ctx.Request.Header.Peek("X-Real-IP"))); raw != "" {
		if addr, err := netip.ParseAddr(raw); err == nil {
			return addr.Unmap().String()
		}
	}
	return remote.String()
}

func (l *Limiter) isTrustedProxy(addr netip.Addr) bool {
	if l == nil {
		return false
	}
	for _, prefix := range l.trustedProxies {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// AddRule registers a named rate limiting rule.
func (l *Limiter) AddRule(rule Rule) {
	l.rules[rule.Name] = rule
}

// Check checks if the request should be rate limited for the given rule.
func (l *Limiter) Check(ctx *fasthttp.RequestCtx, ruleName string) error {
	if l == nil {
		return fmt.Errorf("rate limiter is unavailable")
	}
	rule, ok := l.rules[ruleName]
	if !ok {
		ctx.Response.Header.Set("Content-Type", "application/json")
		ctx.SetStatusCode(fasthttp.StatusServiceUnavailable)
		ctx.SetBodyString(`{"status":"error","message":"Rate limiting temporarily unavailable"}`)
		return fmt.Errorf("rate limit rule %q is not configured", ruleName)
	}
	if !rule.Enabled {
		return nil
	}

	clientIP := l.ClientIP(ctx)
	key := fmt.Sprintf("rate_limit:%s:%s", ruleName, clientIP)

	now := time.Now()
	nowUnix := now.Unix()
	nowNano := now.UnixNano()
	windowStart := strconv.FormatInt(nowUnix-60, 10)

	// Single pipeline: cleanup, add, count, set expiry.
	if l.redis == nil {
		ctx.Response.Header.Set("Content-Type", "application/json")
		ctx.SetStatusCode(fasthttp.StatusServiceUnavailable)
		ctx.SetBodyString(`{"status":"error","message":"Rate limiting temporarily unavailable"}`)
		return fmt.Errorf("rate limit backend unavailable")
	}
	pipe := l.redis.Pipeline()
	pipe.ZRemRangeByScore(ctx, key, "-inf", windowStart)
	pipe.ZAdd(ctx, key, redis.Z{Score: float64(nowUnix), Member: fmt.Sprintf("%d:%d", nowNano, windowMemberSeq.Add(1))})
	countCmd := pipe.ZCard(ctx, key)
	pipe.Expire(ctx, key, time.Minute*2)
	if _, err := pipe.Exec(ctx); err != nil {
		ctx.Response.Header.Set("Content-Type", "application/json")
		ctx.SetStatusCode(fasthttp.StatusServiceUnavailable)
		ctx.SetBodyString(`{"status":"error","message":"Rate limiting temporarily unavailable"}`)
		return fmt.Errorf("rate limit backend unavailable: %w", err)
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
	if key == "" || window <= 0 || maxAttempts <= 0 {
		return result, nil
	}
	if l == nil || l.redis == nil {
		return result, fmt.Errorf("rate limit backend unavailable")
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
