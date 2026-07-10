package main

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/abhinavxd/libredesk/internal/envelope"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/valyala/fasthttp"
)

func TestPublicTicketCaptchaIsConsumedOnce(t *testing.T) {
	app := testSecurityApp(t)
	server := miniredis.RunT(t)
	app.redis = redis.NewClient(&redis.Options{Addr: server.Addr()})
	const token = "AbCdEfGhIjKlMnOpQrStUvWxYz012345"
	key := publicTicketCaptchaKeyPrefix + token
	if err := app.redis.Set(app.ctx, key, "42", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- validatePublicTicketCaptcha(app, token, "42")
		}()
	}
	wg.Wait()
	close(results)

	successes := 0
	failures := 0
	for err := range results {
		if err == nil {
			successes++
		} else {
			failures++
			requireEnvelopeError(t, err, envelope.InputError, fasthttp.StatusBadRequest)
		}
	}
	if successes != 1 || failures != 1 {
		t.Fatalf("captcha must be consumed exactly once: successes=%d failures=%d", successes, failures)
	}
}

func TestPublicTicketCaptchaFailsClosed(t *testing.T) {
	app := testSecurityApp(t)
	server := miniredis.RunT(t)
	app.redis = redis.NewClient(&redis.Options{Addr: server.Addr()})
	server.Close()

	err := validatePublicTicketCaptcha(app, "AbCdEfGhIjKlMnOpQrStUvWxYz012345", "42")
	requireEnvelopeError(t, err, envelope.GeneralError, fasthttp.StatusInternalServerError)
}

func TestPublicTicketCaptchaRejectsUnboundedKeysBeforeRedis(t *testing.T) {
	app := testSecurityApp(t)
	err := validatePublicTicketCaptcha(app, "../../"+strings.Repeat("x", 1000), "123456")
	requireEnvelopeError(t, err, envelope.InputError, fasthttp.StatusBadRequest)
}
