package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"
)

func TestWidgetFrameAncestorsFiltersInvalidSources(t *testing.T) {
	got := widgetFrameAncestors([]string{
		"example.com",
		"*.support.example.com:8443",
		"localhost:5173",
		"example.com; script-src *",
		"https://evil.example",
		"example.com/unsafe",
		"example.com:70000",
	})

	require.Equal(t, "'self' https://example.com https://*.support.example.com:8443 http://localhost:5173", got)
}

func TestPageSecurityHeaders(t *testing.T) {
	ctx := &fasthttp.RequestCtx{}
	setMainPageSecurityHeaders(ctx)

	require.Equal(t, "DENY", string(ctx.Response.Header.Peek("X-Frame-Options")))
	require.Equal(t, "no-referrer", string(ctx.Response.Header.Peek("Referrer-Policy")))
	csp := string(ctx.Response.Header.Peek("Content-Security-Policy"))
	require.Contains(t, csp, "frame-ancestors 'none'")
	require.Contains(t, csp, "script-src 'self' https://challenges.cloudflare.com")
	require.Contains(t, csp, "frame-src https://challenges.cloudflare.com")
	require.Contains(t, csp, "object-src 'none'")
	require.False(t, strings.Contains(csp, "script-src *"))
}

func TestWidgetSecurityHeadersDefaultToSameOriginEmbedding(t *testing.T) {
	ctx := &fasthttp.RequestCtx{}
	setWidgetPageSecurityHeaders(ctx, nil)

	require.Empty(t, ctx.Response.Header.Peek("X-Frame-Options"))
	require.Contains(t, string(ctx.Response.Header.Peek("Content-Security-Policy")), "frame-ancestors 'self'")
}

func TestWidgetScriptSupportsCredentiallessCrossOriginEmbedding(t *testing.T) {
	ctx := &fasthttp.RequestCtx{}
	setWidgetScriptHeaders(ctx)

	require.Equal(t, "*", string(ctx.Response.Header.Peek("Access-Control-Allow-Origin")))
	require.Equal(t, "cross-origin", string(ctx.Response.Header.Peek("Cross-Origin-Resource-Policy")))
	require.Equal(t, "nosniff", string(ctx.Response.Header.Peek("X-Content-Type-Options")))
}
