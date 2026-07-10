package ratelimit

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/valyala/fasthttp"
)

func TestLimiterCheckWindowAllowsUntilLimit(t *testing.T) {
	srv := miniredis.RunT(t)
	limiter := New(redis.NewClient(&redis.Options{Addr: srv.Addr()}))

	for i := 0; i < 5; i++ {
		result, err := limiter.CheckWindow(context.Background(), "register:test", 5*time.Minute, 5)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.Allowed {
			t.Fatalf("attempt %d should be allowed", i+1)
		}
	}

	result, err := limiter.CheckWindow(context.Background(), "register:test", 5*time.Minute, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Allowed {
		t.Fatal("expected sixth attempt to be limited")
	}
	if result.Remaining != 0 {
		t.Fatalf("expected no remaining attempts, got %d", result.Remaining)
	}
}

func requestContext(t *testing.T, remoteIP string, headers map[string]string) *fasthttp.RequestCtx {
	t.Helper()
	var req fasthttp.Request
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	ctx := &fasthttp.RequestCtx{}
	ctx.Init(&req, &net.TCPAddr{IP: net.ParseIP(remoteIP), Port: 1234}, nil)
	return ctx
}

func TestClientIPIgnoresForwardingHeadersFromUntrustedPeer(t *testing.T) {
	limiter := New(nil)
	ctx := requestContext(t, "203.0.113.10", map[string]string{
		"X-Client-IP":     "198.51.100.1",
		"X-Forwarded-For": "198.51.100.2",
	})
	if got := limiter.ClientIP(ctx); got != "203.0.113.10" {
		t.Fatalf("expected socket peer, got %q", got)
	}
}

func TestClientIPWalksTrustedProxyChain(t *testing.T) {
	limiter := New(nil)
	if err := limiter.SetTrustedProxies([]string{"10.0.0.0/8"}); err != nil {
		t.Fatal(err)
	}
	ctx := requestContext(t, "10.0.0.2", map[string]string{
		"X-Forwarded-For": "198.51.100.7, 10.0.0.1",
	})
	if got := limiter.ClientIP(ctx); got != "198.51.100.7" {
		t.Fatalf("expected original client, got %q", got)
	}
}

func TestClientIPFallsBackToPeerForMalformedChain(t *testing.T) {
	limiter := New(nil)
	if err := limiter.SetTrustedProxies([]string{"10.0.0.0/8"}); err != nil {
		t.Fatal(err)
	}
	ctx := requestContext(t, "10.0.0.2", map[string]string{
		"X-Forwarded-For": "attacker-controlled",
	})
	if got := limiter.ClientIP(ctx); got != "10.0.0.2" {
		t.Fatalf("expected safe peer fallback, got %q", got)
	}
}

func TestSetTrustedProxiesRejectsInvalidEntry(t *testing.T) {
	limiter := New(nil)
	if err := limiter.SetTrustedProxies([]string{"not-a-network"}); err == nil {
		t.Fatal("expected invalid trusted proxy to fail")
	}
	if err := limiter.SetTrustedProxies([]string{"0.0.0.0/0"}); err == nil {
		t.Fatal("expected trust-all proxy network to fail")
	}
}

func TestCheckFailsClosedWhenRedisIsUnavailable(t *testing.T) {
	srv := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: srv.Addr()})
	limiter := New(client)
	limiter.AddRule(Rule{Name: "auth", Enabled: true, RequestsPerMinute: 10})
	srv.Close()

	ctx := requestContext(t, "203.0.113.10", nil)
	if err := limiter.Check(ctx, "auth"); err == nil {
		t.Fatal("expected Redis failure to reject request")
	}
	if ctx.Response.StatusCode() != fasthttp.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", ctx.Response.StatusCode())
	}
}

func TestCheckFailsClosedForUnknownRule(t *testing.T) {
	limiter := New(nil)
	ctx := requestContext(t, "203.0.113.10", nil)

	if err := limiter.Check(ctx, "misspelled-rule"); err == nil {
		t.Fatal("expected an unknown rule to reject the request")
	}
	if ctx.Response.StatusCode() != fasthttp.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", ctx.Response.StatusCode())
	}
}

func TestLimiterCheckWindowSeparatesKeys(t *testing.T) {
	srv := miniredis.RunT(t)
	limiter := New(redis.NewClient(&redis.Options{Addr: srv.Addr()}))

	for i := 0; i < 5; i++ {
		if result, err := limiter.CheckWindow(context.Background(), "register:email:a", time.Minute, 5); err != nil || !result.Allowed {
			t.Fatalf("attempt %d for first key failed: result=%+v err=%v", i+1, result, err)
		}
	}

	result, err := limiter.CheckWindow(context.Background(), "register:email:b", time.Minute, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Allowed {
		t.Fatal("separate key should not inherit another subject limit")
	}
}
