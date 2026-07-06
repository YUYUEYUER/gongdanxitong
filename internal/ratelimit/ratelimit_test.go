package ratelimit

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
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
