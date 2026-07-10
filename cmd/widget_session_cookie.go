package main

import (
	"strings"
	"time"

	"github.com/valyala/fasthttp"
)

type widgetCookieKind string

const (
	widgetSessionCookie widgetCookieKind = "session"
	widgetVisitorCookie widgetCookieKind = "visitor"
)

func widgetTokenCookieName(inboxUUID string, kind widgetCookieKind) string {
	safeInbox := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return -1
	}, inboxUUID)
	return "__Host-libredesk-widget-" + string(kind) + "-" + safeInbox
}

func setWidgetTokenCookie(ctx *fasthttp.RequestCtx, inboxUUID, token string, kind widgetCookieKind, ttl time.Duration) {
	if token == "" || ttl <= 0 {
		return
	}
	cookie := fasthttp.AcquireCookie()
	defer fasthttp.ReleaseCookie(cookie)
	cookie.SetKey(widgetTokenCookieName(inboxUUID, kind))
	cookie.SetValue(token)
	cookie.SetPath("/")
	cookie.SetHTTPOnly(true)
	cookie.SetSecure(true)
	cookie.SetSameSite(fasthttp.CookieSameSiteNoneMode)
	cookie.SetPartitioned(true)
	cookie.SetMaxAge(int(ttl.Seconds()))
	cookie.SetExpire(time.Now().Add(ttl))
	ctx.Response.Header.SetCookie(cookie)
}

func getWidgetTokenCookie(ctx *fasthttp.RequestCtx, inboxUUID string, kind widgetCookieKind) string {
	return string(ctx.Request.Header.Cookie(widgetTokenCookieName(inboxUUID, kind)))
}

func clearWidgetTokenCookie(ctx *fasthttp.RequestCtx, inboxUUID string, kind widgetCookieKind) {
	cookie := fasthttp.AcquireCookie()
	defer fasthttp.ReleaseCookie(cookie)
	cookie.SetKey(widgetTokenCookieName(inboxUUID, kind))
	cookie.SetPath("/")
	cookie.SetHTTPOnly(true)
	cookie.SetSecure(true)
	cookie.SetSameSite(fasthttp.CookieSameSiteNoneMode)
	cookie.SetPartitioned(true)
	cookie.SetMaxAge(-1)
	cookie.SetExpire(time.Unix(1, 0))
	ctx.Response.Header.SetCookie(cookie)
}
