package main

import (
	"errors"
	"testing"

	"github.com/abhinavxd/libredesk/internal/envelope"
	"github.com/abhinavxd/libredesk/internal/ratelimit"
	"github.com/alicebob/miniredis/v2"
	"github.com/knadh/go-i18n"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"
	"github.com/zerodha/fastglue"
	"github.com/zerodha/logf"
)

func testSecurityApp(t *testing.T) *App {
	t.Helper()

	tr, err := i18n.New([]byte(`{
		"_.code":"en",
		"_.name":"English",
		"auth.csrfTokenMismatch":"Page state expired. Please refresh and try again.",
		"auth.rateLimited":"Too many attempts. Please try again later.",
		"globals.messages.badRequest":"Bad request",
		"publicTicket.nameRequired":"Please enter your name.",
		"validation.invalidEmail":"Invalid email address"
	}`))
	require.NoError(t, err)

	lo := logf.New(logf.Opts{})
	return &App{i18n: tr, lo: &lo}
}

func testFastRequest(app *App, method, contentType string) *fastglue.Request {
	ctx := &fasthttp.RequestCtx{}
	ctx.Request.Header.SetMethod(method)
	ctx.Request.SetRequestURI("/api/v1/customer/auth/register")
	if contentType != "" {
		ctx.Request.Header.SetContentType(contentType)
	}
	return &fastglue.Request{RequestCtx: ctx, Context: app}
}

func TestValidateCSRFCookie(t *testing.T) {
	app := testSecurityApp(t)

	t.Run("missing token fails", func(t *testing.T) {
		req := testFastRequest(app, fasthttp.MethodPost, "application/json")
		err := validateCSRFCookie(req, app)
		requireEnvelopeError(t, err, envelope.PermissionError, fasthttp.StatusForbidden)
	})

	t.Run("wrong token fails", func(t *testing.T) {
		req := testFastRequest(app, fasthttp.MethodPost, "application/json")
		req.RequestCtx.Request.Header.SetCookie("csrf_token", "cookie-token")
		req.RequestCtx.Request.Header.Set("X-CSRFTOKEN", "header-token")
		err := validateCSRFCookie(req, app)
		requireEnvelopeError(t, err, envelope.PermissionError, fasthttp.StatusForbidden)
	})

	t.Run("matching token passes", func(t *testing.T) {
		req := testFastRequest(app, fasthttp.MethodPost, "application/json")
		req.RequestCtx.Request.Header.SetCookie("csrf_token", "same-token")
		req.RequestCtx.Request.Header.Set("X-CSRFTOKEN", "same-token")
		require.NoError(t, validateCSRFCookie(req, app))
	})
}

func TestRequireJSONPost(t *testing.T) {
	app := testSecurityApp(t)

	require.NoError(t, requireJSONPost(testFastRequest(app, fasthttp.MethodPost, "application/json").RequestCtx, app))
	requireEnvelopeError(t, requireJSONPost(testFastRequest(app, fasthttp.MethodGet, "application/json").RequestCtx, app), envelope.InputError, fasthttp.StatusMethodNotAllowed)
	requireEnvelopeError(t, requireJSONPost(testFastRequest(app, fasthttp.MethodPost, "text/plain").RequestCtx, app), envelope.InputError, fasthttp.StatusUnsupportedMediaType)
}

func TestValidateCustomerRegisterFields(t *testing.T) {
	app := testSecurityApp(t)

	requireEnvelopeError(t, validateCustomerRegisterFields(app, customerRegisterRequest{
		Email:    "user@example.com",
		Password: "StrongPassword1!",
	}), envelope.InputError, fasthttp.StatusBadRequest)

	requireEnvelopeError(t, validateCustomerRegisterFields(app, customerRegisterRequest{
		FirstName: "User",
		Email:     "not-an-email",
		Password:  "StrongPassword1!",
	}), envelope.InputError, fasthttp.StatusBadRequest)

	require.NoError(t, validateCustomerRegisterFields(app, customerRegisterRequest{
		FirstName: "User",
		Email:     "user@example.com",
		Password:  "StrongPassword1!",
	}))
}

func TestCheckCustomerRegisterRateLimit(t *testing.T) {
	app := testSecurityApp(t)
	srv := miniredis.RunT(t)
	app.rateLimit = ratelimit.New(redis.NewClient(&redis.Options{Addr: srv.Addr()}))

	req := customerRegisterRequest{
		FirstName: "Rate",
		LastName:  "Limited",
		Email:     "rate@example.com",
	}

	for i := 0; i < 5; i++ {
		require.NoError(t, checkCustomerRegisterRateLimit(t.Context(), app, "203.0.113.10", "test-agent", req))
	}

	err := checkCustomerRegisterRateLimit(t.Context(), app, "203.0.113.10", "test-agent", req)
	requireEnvelopeError(t, err, envelope.RateLimitError, fasthttp.StatusTooManyRequests)
}

func TestCustomerRegisterRequestTurnstileResponse(t *testing.T) {
	req := customerRegisterRequest{
		CFTurnstileResponse: "cf-token",
		TurnstileToken:      "legacy-token",
	}
	require.Equal(t, "cf-token", req.turnstileResponse())

	req.CFTurnstileResponse = ""
	require.Equal(t, "legacy-token", req.turnstileResponse())
}

func TestLoginRequestTurnstileResponse(t *testing.T) {
	req := loginRequest{
		CFTurnstileResponse: " cf-token ",
		TurnstileToken:      "legacy-token",
	}
	require.Equal(t, "cf-token", req.turnstileResponse())

	req.CFTurnstileResponse = ""
	require.Equal(t, "legacy-token", req.turnstileResponse())
}

func requireEnvelopeError(t *testing.T, err error, errorType string, code int) {
	t.Helper()

	require.Error(t, err)
	var envErr envelope.Error
	require.True(t, errors.As(err, &envErr), "expected envelope error, got %T", err)
	require.Equal(t, errorType, envErr.ErrorType)
	require.Equal(t, code, envErr.Code)
}
