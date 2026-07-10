package main

import "testing"

func TestActiveConnectionLimiterEnforcesAndReleasesEveryDimension(t *testing.T) {
	t.Parallel()

	limiter := newActiveConnectionLimiter(3, 2, 1)
	releaseOne, ok := limiter.acquire("inbox-a", "192.0.2.1")
	if !ok {
		t.Fatal("first connection should be accepted")
	}
	releaseTwo, ok := limiter.acquire("inbox-a", "192.0.2.2")
	if !ok {
		t.Fatal("second connection should be accepted")
	}
	if _, ok := limiter.acquire("inbox-a", "192.0.2.3"); ok {
		t.Fatal("group limit must be enforced")
	}
	if _, ok := limiter.acquire("inbox-b", "192.0.2.1"); ok {
		t.Fatal("IP limit must be enforced")
	}
	releaseOne()
	releaseOne()
	if _, ok := limiter.acquire("inbox-b", "192.0.2.1"); !ok {
		t.Fatal("idempotent release must return capacity")
	}
	releaseTwo()
}
