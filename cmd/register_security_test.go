package main

import (
	"context"
	"errors"
	"testing"

	"github.com/abhinavxd/libredesk/internal/envelope"
	"github.com/abhinavxd/libredesk/internal/ratelimit"
	turnstilesvc "github.com/abhinavxd/libredesk/internal/turnstile"
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
		"auth.turnstileRequired":"Please complete the verification challenge.",
		"globals.messages.badRequest":"Bad request",
		"globals.messages.somethingWentWrong":"Something went wrong",
		"globals.messages.tooManyRequests":"Too many requests",
		"publicTicket.nameRequired":"Please enter your name.",
		"validation.invalidEmail":"Invalid email address"
	}`))
	require.NoError(t, err)

	lo := logf.New(logf.Opts{})
	return &App{ctx: context.Background(), i18n: tr, lo: &lo}
}

func TestRequestBodyLimit(t *testing.T) {
	app := testSecurityApp(t)
	called := false
	handler := requestBodyLimit(func(_ *fastglue.Request) error {
		called = true
		return nil
	}, 4)
	req := testFastRequest(app, fasthttp.MethodPost, "application/json")
	req.RequestCtx.Request.SetBodyString("12345")
	_ = handler(req)
	require.False(t, called)
	require.Equal(t, fasthttp.StatusRequestEntityTooLarge, req.RequestCtx.Response.StatusCode())
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

func TestSafeNextPathRejectsExternalRedirects(t *testing.T) {
	t.Parallel()

	require.Equal(t, "/inboxes/assigned?status=open", safeNextPath("/inboxes/assigned?status=open", "/"))
	for _, raw := range []string{
		"https://evil.example/path",
		"//evil.example/path",
		"/\\evil.example/path",
		"/%2f%2fevil.example/path",
		"javascript:alert(1)",
		"/safe\r\nLocation: https://evil.example",
	} {
		require.Equal(t, "/fallback", safeNextPath(raw, "/fallback"), raw)
	}
}

func TestValidateCustomerRegisterFields(t *testing.T) {
	app := testSecurityApp(t)

	requireEnvelopeError(t, validateCustomerRegisterFields(app, customerRegisterRequest{
		Email: "user@example.com",
	}), envelope.InputError, fasthttp.StatusBadRequest)

	requireEnvelopeError(t, validateCustomerRegisterFields(app, customerRegisterRequest{
		FirstName: "User",
		Email:     "not-an-email",
	}), envelope.InputError, fasthttp.StatusBadRequest)

	require.NoError(t, validateCustomerRegisterFields(app, customerRegisterRequest{
		FirstName: "User",
		Email:     "user@example.com",
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

func TestCheckCustomerRegisterRateLimitFailsClosed(t *testing.T) {
	app := testSecurityApp(t)
	srv := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: srv.Addr()})
	app.rateLimit = ratelimit.New(client)
	srv.Close()

	err := checkCustomerRegisterRateLimit(t.Context(), app, "203.0.113.10", "test-agent", customerRegisterRequest{
		FirstName: "Rate",
		Email:     "rate@example.com",
	})
	requireEnvelopeError(t, err, envelope.GeneralError, fasthttp.StatusServiceUnavailable)
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

func TestCustomerLoginRequestTurnstileResponse(t *testing.T) {
	req := customerLoginRequest{
		CFTurnstileResponse: " cf-token ",
		TurnstileToken:      "legacy-token",
	}
	require.Equal(t, "cf-token", req.turnstileResponse())

	req.CFTurnstileResponse = ""
	require.Equal(t, "legacy-token", req.turnstileResponse())
}

func TestHandleCustomerLoginRequiresTurnstileToken(t *testing.T) {
	app := testSecurityApp(t)
	app.turnstile = turnstilesvc.New(true, "site-key", "secret-key", app.lo)

	req := testFastRequest(app, fasthttp.MethodPost, "application/json")
	req.RequestCtx.Request.SetRequestURI("/api/v1/customer/auth/login")
	req.RequestCtx.Request.SetBodyString(`{"email":"user@example.com","password":"Password1!"}`)
	req.RequestCtx.Request.Header.SetCookie("csrf_token", "same-token")
	req.RequestCtx.Request.Header.Set("X-CSRFTOKEN", "same-token")

	require.NoError(t, handleCustomerLogin(req))
	require.Equal(t, fasthttp.StatusBadRequest, req.RequestCtx.Response.StatusCode())
	require.Contains(t, string(req.RequestCtx.Response.Body()), "Please complete the verification challenge.")
}

func TestHandleCustomerLoginRequiresCSRFToken(t *testing.T) {
	app := testSecurityApp(t)
	req := testFastRequest(app, fasthttp.MethodPost, "application/json")
	req.RequestCtx.Request.SetBodyString(`{"email":"user@example.com","password":"Password1!"}`)

	require.NoError(t, handleCustomerLogin(req))
	require.Equal(t, fasthttp.StatusForbidden, req.RequestCtx.Response.StatusCode())
	require.Contains(t, string(req.RequestCtx.Response.Body()), "Page state expired")
}

func requireEnvelopeError(t *testing.T, err error, errorType string, code int) {
	t.Helper()

	require.Error(t, err)
	var envErr envelope.Error
	require.True(t, errors.As(err, &envErr), "expected envelope error, got %T", err)
	require.Equal(t, errorType, envErr.ErrorType)
	require.Equal(t, code, envErr.Code)
}
