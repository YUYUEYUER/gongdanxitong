package main

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/valyala/fasthttp"
)

var trustedDomainPattern = regexp.MustCompile(`^(?:\*\.)?(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)*[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?::[0-9]{1,5})?$`)

func setCommonBrowserSecurityHeaders(ctx *fasthttp.RequestCtx) {
	ctx.Response.Header.Set("X-Content-Type-Options", "nosniff")
	ctx.Response.Header.Set("X-XSS-Protection", "0")
	ctx.Response.Header.Set("Referrer-Policy", "no-referrer")
	ctx.Response.Header.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
	ctx.Response.Header.Set("Strict-Transport-Security", "max-age=31536000")
}

func setWidgetScriptHeaders(ctx *fasthttp.RequestCtx) {
	ctx.Response.Header.Set("Content-Type", "application/javascript")
	ctx.Response.Header.Set("Cache-Control", "no-cache")
	ctx.Response.Header.Set("Access-Control-Allow-Origin", "*")
	ctx.Response.Header.Set("Cross-Origin-Resource-Policy", "cross-origin")
	ctx.Response.Header.Set("X-Content-Type-Options", "nosniff")
}

func setMainPageSecurityHeaders(ctx *fasthttp.RequestCtx) {
	setCommonBrowserSecurityHeaders(ctx)
	ctx.Response.Header.Set("X-Frame-Options", "DENY")
	ctx.Response.Header.Set("Content-Security-Policy", strings.Join([]string{
		"default-src 'self'",
		"script-src 'self' https://challenges.cloudflare.com",
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data: blob: https:",
		"font-src 'self' data:",
		"connect-src 'self' https: wss: ws://localhost:* ws://127.0.0.1:*",
		"media-src 'self' blob: https:",
		"frame-src https://challenges.cloudflare.com",
		"object-src 'none'",
		"base-uri 'none'",
		"form-action 'self'",
		"frame-ancestors 'none'",
	}, "; "))
}

func setWidgetPageSecurityHeaders(ctx *fasthttp.RequestCtx, trustedDomains []string) {
	setCommonBrowserSecurityHeaders(ctx)
	ctx.Response.Header.Set("Content-Security-Policy", strings.Join([]string{
		"default-src 'self'",
		"script-src 'self'",
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data: blob: https:",
		"font-src 'self' data:",
		"connect-src 'self' https: wss: ws://localhost:* ws://127.0.0.1:*",
		"media-src 'self' blob: https:",
		"frame-src 'none'",
		"object-src 'none'",
		"base-uri 'none'",
		"form-action 'self'",
		"frame-ancestors " + widgetFrameAncestors(trustedDomains),
	}, "; "))
}

func setCSATPageSecurityHeaders(ctx *fasthttp.RequestCtx, allowSameOriginFrame bool) {
	setCommonBrowserSecurityHeaders(ctx)
	frameAncestors := "'none'"
	if allowSameOriginFrame {
		frameAncestors = "'self'"
	} else {
		ctx.Response.Header.Set("X-Frame-Options", "DENY")
	}
	ctx.Response.Header.Set("Content-Security-Policy", strings.Join([]string{
		"default-src 'self'",
		"script-src 'self'",
		"style-src 'self'",
		"img-src 'self' data: https:",
		"font-src 'self'",
		"connect-src 'self'",
		"object-src 'none'",
		"base-uri 'none'",
		"form-action 'self'",
		"frame-ancestors " + frameAncestors,
	}, "; "))
}

func widgetFrameAncestors(trustedDomains []string) string {
	sources := []string{"'self'"}
	seen := map[string]struct{}{"'self'": {}}
	for _, raw := range trustedDomains {
		source, ok := trustedDomainCSPSource(raw)
		if !ok {
			continue
		}
		if _, exists := seen[source]; exists {
			continue
		}
		seen[source] = struct{}{}
		sources = append(sources, source)
	}
	return strings.Join(sources, " ")
}

func trustedDomainCSPSource(raw string) (string, bool) {
	domain := strings.ToLower(strings.TrimSpace(raw))
	if domain == "" || !trustedDomainPattern.MatchString(domain) {
		return "", false
	}

	host := domain
	if idx := strings.LastIndex(domain, ":"); idx > -1 {
		port, err := strconv.Atoi(domain[idx+1:])
		if err != nil || port < 1 || port > 65535 {
			return "", false
		}
		host = domain[:idx]
	}

	scheme := "https://"
	plainHost := strings.TrimPrefix(host, "*.")
	if plainHost == "localhost" || plainHost == "127.0.0.1" {
		scheme = "http://"
	}
	return scheme + domain, true
}
