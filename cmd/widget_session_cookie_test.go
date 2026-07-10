package main

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"
)

func TestWidgetTokenCookieSecurityAttributes(t *testing.T) {
	ctx := &fasthttp.RequestCtx{}
	setWidgetTokenCookie(ctx, "inbox-uuid", "secret-token", widgetSessionCookie, time.Hour)

	header := string(ctx.Response.Header.Peek("Set-Cookie"))
	require.Contains(t, header, "__Host-libredesk-widget-session-inbox-uuid=secret-token")
	lowerHeader := strings.ToLower(header)
	for _, attr := range []string{"path=/", "httponly", "secure", "samesite=none", "partitioned"} {
		require.Contains(t, lowerHeader, attr)
	}
	require.False(t, strings.Contains(lowerHeader, "domain="))
}

func TestWidgetTokenCookieRoundTripName(t *testing.T) {
	ctx := &fasthttp.RequestCtx{}
	name := widgetTokenCookieName("inbox/unsafe", widgetVisitorCookie)
	ctx.Request.Header.SetCookie(name, "visitor-token")
	require.Equal(t, "visitor-token", getWidgetTokenCookie(ctx, "inbox/unsafe", widgetVisitorCookie))
}
