package main

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"
)

func TestReadinessFailsClosedWithoutDependencies(t *testing.T) {
	app := testSecurityApp(t)
	req := testFastRequest(app, fasthttp.MethodGet, "")

	require.NoError(t, handleReadinessCheck(req))
	require.Equal(t, fasthttp.StatusServiceUnavailable, req.RequestCtx.Response.StatusCode())
}

func TestReadinessCacheCoalescesConcurrentProbes(t *testing.T) {
	t.Parallel()

	cache := newReadinessResultCache(time.Second)
	const callers = 64
	start := make(chan struct{})
	releaseProbe := make(chan struct{})
	probeEntered := make(chan struct{})
	var (
		attempted  atomic.Int32
		probeCalls atomic.Int32
		once       sync.Once
		wg         sync.WaitGroup
	)

	probe := func(context.Context) bool {
		probeCalls.Add(1)
		once.Do(func() { close(probeEntered) })
		<-releaseProbe
		return true
	}

	results := make(chan bool, callers)
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			attempted.Add(1)
			results <- cache.check(context.Background(), probe)
		}()
	}
	close(start)
	<-probeEntered

	deadline := time.Now().Add(time.Second)
	for attempted.Load() != callers && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	require.Equal(t, int32(callers), attempted.Load())
	close(releaseProbe)
	wg.Wait()
	close(results)

	for ready := range results {
		require.True(t, ready)
	}
	require.Equal(t, int32(1), probeCalls.Load())
	require.True(t, cache.check(context.Background(), probe))
	require.Equal(t, int32(1), probeCalls.Load(), "fresh cached result must not re-probe dependencies")
}

func TestReadinessCacheCachesFailures(t *testing.T) {
	t.Parallel()

	cache := newReadinessResultCache(time.Second)
	var calls atomic.Int32
	probe := func(context.Context) bool {
		calls.Add(1)
		return false
	}

	require.False(t, cache.check(context.Background(), probe))
	require.False(t, cache.check(context.Background(), probe))
	require.Equal(t, int32(1), calls.Load())
}
