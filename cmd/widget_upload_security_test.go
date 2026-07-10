package main

import (
	"testing"

	"github.com/abhinavxd/libredesk/internal/envelope"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/valyala/fasthttp"
)

func TestWidgetUploadBudgetIsPerContactAndPersistentBytes(t *testing.T) {
	app := testSecurityApp(t)
	server := miniredis.RunT(t)
	app.redis = redis.NewClient(&redis.Options{Addr: server.Addr()})

	for range maxWidgetUploadsPerContactHour {
		if err := checkWidgetUploadBudget(app, 11, 22, 1); err != nil {
			t.Fatal(err)
		}
	}
	err := checkWidgetUploadBudget(app, 11, 22, 1)
	requireEnvelopeError(t, err, envelope.RateLimitError, fasthttp.StatusTooManyRequests)

	if err := checkWidgetUploadBudget(app, 12, 22, maxWidgetUploadBytesContactDay); err != nil {
		t.Fatal(err)
	}
	err = checkWidgetUploadBudget(app, 12, 22, 1)
	requireEnvelopeError(t, err, envelope.RateLimitError, fasthttp.StatusTooManyRequests)
}

func TestWidgetUploadBudgetFailsClosed(t *testing.T) {
	app := testSecurityApp(t)
	server := miniredis.RunT(t)
	app.redis = redis.NewClient(&redis.Options{Addr: server.Addr()})
	server.Close()

	err := checkWidgetUploadBudget(app, 1, 2, 1)
	requireEnvelopeError(t, err, envelope.GeneralError, fasthttp.StatusServiceUnavailable)
}
