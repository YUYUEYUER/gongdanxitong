package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/abhinavxd/libredesk/internal/envelope"
	usersvc "github.com/abhinavxd/libredesk/internal/user"
	"github.com/valyala/fasthttp"
)

const (
	turnstileActionCustomerRegister = "customer_register"
	turnstileActionCustomerLogin    = "customer_login"
)

func requireJSONPost(rctx *fasthttp.RequestCtx, app *App) error {
	if string(rctx.Method()) != fasthttp.MethodPost {
		return envelope.NewErrorWithCode(envelope.InputError, fasthttp.StatusMethodNotAllowed, app.i18n.T("globals.messages.badRequest"), nil)
	}

	contentType := bytes.ToLower(rctx.Request.Header.ContentType())
	if !bytes.Contains(contentType, []byte("application/json")) {
		return envelope.NewErrorWithCode(envelope.InputError, fasthttp.StatusUnsupportedMediaType, app.i18n.T("globals.messages.badRequest"), nil)
	}

	return nil
}

func normalizeCustomerRegisterRequest(req *customerRegisterRequest) {
	req.FirstName = strings.TrimSpace(req.FirstName)
	req.LastName = strings.TrimSpace(req.LastName)
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	req.CFTurnstileResponse = strings.TrimSpace(req.CFTurnstileResponse)
	req.TurnstileToken = strings.TrimSpace(req.TurnstileToken)
}

func (req customerRegisterRequest) turnstileResponse() string {
	if req.CFTurnstileResponse != "" {
		return req.CFTurnstileResponse
	}
	return req.TurnstileToken
}

func (req customerLoginRequest) turnstileResponse() string {
	if strings.TrimSpace(req.CFTurnstileResponse) != "" {
		return strings.TrimSpace(req.CFTurnstileResponse)
	}
	return strings.TrimSpace(req.TurnstileToken)
}

func (req customerRegisterRequest) displayName() string {
	return strings.TrimSpace(strings.Join([]string{req.FirstName, req.LastName}, " "))
}

func validateCustomerRegisterFields(app *App, req customerRegisterRequest) error {
	if req.FirstName == "" {
		return envelope.NewError(envelope.InputError, app.i18n.T("publicTicket.nameRequired"), nil)
	}
	if req.Email == "" || !strings.Contains(req.Email, "@") {
		return envelope.NewError(envelope.InputError, app.i18n.T("validation.invalidEmail"), nil)
	}
	if !usersvc.IsStrongPassword(req.Password) {
		return envelope.NewError(envelope.InputError, usersvc.PasswordHint, nil)
	}
	return nil
}

func checkCustomerRegisterRateLimit(ctx context.Context, app *App, ip, userAgent string, req customerRegisterRequest) error {
	if app.rateLimit == nil {
		return nil
	}

	window := registerRateLimitWindow()
	maxAttempts := registerRateLimitMaxAttempts()
	subjects := []struct {
		kind  string
		value string
	}{
		{kind: "ip", value: ip},
		{kind: "email", value: req.Email},
		{kind: "name", value: strings.ToLower(req.displayName())},
	}

	for _, subject := range subjects {
		if strings.TrimSpace(subject.value) == "" {
			continue
		}

		subjectHash := sha256Hex(subject.value)
		key := fmt.Sprintf("rate_limit:register:%s:%s", subject.kind, subjectHash)
		result, err := app.rateLimit.CheckWindow(ctx, key, window, maxAttempts)
		if err != nil {
			app.lo.Warn("register rate limit check failed", "subject", subject.kind, "error", err)
			continue
		}
		if result.Allowed {
			continue
		}

		app.lo.Warn(
			"register rate limit triggered",
			"ip", ip,
			"user_agent", userAgent,
			"email_hash", sha256Hex(req.Email),
			"subject", subject.kind,
			"subject_hash", subjectHash,
		)
		return envelope.NewError(envelope.RateLimitError, app.i18n.T("auth.rateLimited"), nil)
	}

	return nil
}

func registerRateLimitWindow() time.Duration {
	seconds := ko.Int("register_rate_limit.window_seconds")
	if seconds <= 0 {
		seconds = 300
	}
	return time.Duration(seconds) * time.Second
}

func registerRateLimitMaxAttempts() int {
	maxAttempts := ko.Int("register_rate_limit.max_attempts")
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	return maxAttempts
}

func sha256Hex(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
