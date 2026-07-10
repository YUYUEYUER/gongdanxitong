package main

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/abhinavxd/libredesk/internal/envelope"
	"github.com/valyala/fasthttp"
	"github.com/zerodha/fastglue"
)

// handleWidgetLogout revokes both current and legacy visitor sessions. It is
// intentionally idempotent so an expired Redis session can still clear the
// browser's HttpOnly partitioned cookies.
func handleWidgetLogout(r *fastglue.Request) error {
	app := r.Context.(*App)
	inbox, err := getWidgetInbox(r)
	if err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, app.i18n.T("validation.notFoundInbox"), nil, envelope.InputError)
	}

	tokens, err := collectWidgetLogoutTokens(
		getWidgetTokenCookie(r.RequestCtx, inbox.UUID, widgetSessionCookie),
		string(r.RequestCtx.Request.Header.Peek("Authorization")),
		getWidgetTokenCookie(r.RequestCtx, inbox.UUID, widgetVisitorCookie),
		string(r.RequestCtx.Request.Header.Peek(hdrWidgetVisitorToken)),
	)
	if err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, app.i18n.T("globals.messages.badRequest"), nil, envelope.InputError)
	}

	var revokeErr error
	for _, token := range tokens {
		if err := deleteSessionToken(app, token); err != nil && revokeErr == nil {
			revokeErr = err
		}
	}

	if revokeErr != nil {
		app.lo.Error("error revoking widget logout sessions", "error", revokeErr)
		return r.SendErrorEnvelope(fasthttp.StatusServiceUnavailable, app.i18n.T("globals.messages.somethingWentWrong"), nil, envelope.GeneralError)
	}
	clearWidgetTokenCookie(r.RequestCtx, inbox.UUID, widgetSessionCookie)
	clearWidgetTokenCookie(r.RequestCtx, inbox.UUID, widgetVisitorCookie)
	return r.SendEnvelope(true)
}

func collectWidgetLogoutTokens(sessionCookie, authorization, visitorCookie, visitorHeader string) ([]string, error) {
	tokens := make([]string, 0, 4)
	seen := make(map[string]struct{}, 4)
	add := func(token string) error {
		token = strings.TrimSpace(token)
		if token == "" {
			return nil
		}
		if len(token) > 256 || strings.IndexFunc(token, func(r rune) bool {
			return unicode.IsSpace(r) || unicode.IsControl(r) ||
				!((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
					(r >= '0' && r <= '9') || r == '-' || r == '_')
		}) >= 0 {
			return fmt.Errorf("invalid widget logout token")
		}
		if _, exists := seen[token]; !exists {
			seen[token] = struct{}{}
			tokens = append(tokens, token)
		}
		return nil
	}

	if err := add(sessionCookie); err != nil {
		return nil, err
	}
	authorization = strings.TrimSpace(authorization)
	if authorization != "" {
		parts := strings.Fields(authorization)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			return nil, fmt.Errorf("invalid widget authorization header")
		}
		if err := add(parts[1]); err != nil {
			return nil, err
		}
	}
	if err := add(visitorCookie); err != nil {
		return nil, err
	}
	if err := add(visitorHeader); err != nil {
		return nil, err
	}
	return tokens, nil
}
