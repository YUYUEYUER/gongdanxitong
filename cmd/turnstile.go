package main

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/abhinavxd/libredesk/internal/envelope"
	turnstilesvc "github.com/abhinavxd/libredesk/internal/turnstile"
	realip "github.com/ferluci/fast-realip"
	"github.com/zerodha/fastglue"
)

func validateTurnstileToken(r *fastglue.Request, token string, opts ...turnstilesvc.Option) error {
	app := r.Context.(*App)
	if app.turnstile == nil || !app.turnstile.Enabled() {
		return nil
	}

	ctx, cancel := context.WithTimeout(app.ctx, turnstileVerifyTimeout())
	defer cancel()

	if err := app.turnstile.Verify(ctx, token, realip.FromRequest(r.RequestCtx), opts...); err != nil {
		if errors.Is(err, turnstilesvc.ErrTokenMissing) {
			return envelope.NewError(envelope.InputError, app.i18n.T("auth.turnstileRequired"), nil)
		}

		var verificationErr *turnstilesvc.VerificationError
		if errors.As(err, &verificationErr) {
			return envelope.NewError(envelope.InputError, app.i18n.T("auth.turnstileFailed"), nil)
		}

		app.lo.Error("error verifying turnstile token", "error", err)
		return envelope.NewError(envelope.InputError, app.i18n.T("auth.turnstileFailed"), nil)
	}

	return nil
}

func turnstileVerifyURL() string {
	rawURL := strings.TrimSpace(ko.String("turnstile.verify_url"))
	if rawURL == "" {
		return "https://challenges.cloudflare.com/turnstile/v0/siteverify"
	}
	return rawURL
}

func turnstileVerifyTimeout() time.Duration {
	timeoutMS := ko.Int("turnstile.verify_timeout_ms")
	if timeoutMS <= 0 {
		timeoutMS = 5000
	}
	return time.Duration(timeoutMS) * time.Millisecond
}
