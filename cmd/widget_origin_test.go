package main

import (
	"testing"

	"github.com/abhinavxd/libredesk/internal/inbox/channel/livechat"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"
)

func TestValidateWidgetParentOrigin(t *testing.T) {
	t.Run("trusted cross-origin launcher", func(t *testing.T) {
		ctx := &fasthttp.RequestCtx{}
		ctx.Request.SetHost("desk.example.com")
		ctx.Request.Header.Set("Origin", "https://shop.example.com")
		ctx.QueryArgs().Set(argWidgetParentOrigin, "https://shop.example.com")

		origin, ok := validateWidgetParentOrigin(ctx, livechat.Config{TrustedDomains: []string{"shop.example.com"}})
		require.True(t, ok)
		require.Equal(t, "https://shop.example.com", origin)
		require.Equal(t, origin, string(ctx.Response.Header.Peek("Access-Control-Allow-Origin")))
	})

	t.Run("untrusted parent", func(t *testing.T) {
		ctx := &fasthttp.RequestCtx{}
		ctx.Request.SetHost("desk.example.com")
		ctx.QueryArgs().Set(argWidgetParentOrigin, "https://evil.example")

		_, ok := validateWidgetParentOrigin(ctx, livechat.Config{TrustedDomains: []string{"shop.example.com"}})
		require.False(t, ok)
	})

	t.Run("cross-origin claim mismatch", func(t *testing.T) {
		ctx := &fasthttp.RequestCtx{}
		ctx.Request.SetHost("desk.example.com")
		ctx.Request.Header.Set("Origin", "https://evil.example")
		ctx.QueryArgs().Set(argWidgetParentOrigin, "https://shop.example.com")

		_, ok := validateWidgetParentOrigin(ctx, livechat.Config{TrustedDomains: []string{"shop.example.com", "evil.example"}})
		require.False(t, ok)
	})

	t.Run("empty allowlist is same-origin only", func(t *testing.T) {
		ctx := &fasthttp.RequestCtx{}
		ctx.Request.SetHost("desk.example.com")
		ctx.QueryArgs().Set(argWidgetParentOrigin, "https://desk.example.com")

		_, ok := validateWidgetParentOrigin(ctx, livechat.Config{})
		require.True(t, ok)

		ctx.QueryArgs().Set(argWidgetParentOrigin, "https://shop.example.com")
		_, ok = validateWidgetParentOrigin(ctx, livechat.Config{})
		require.False(t, ok)
	})
}
