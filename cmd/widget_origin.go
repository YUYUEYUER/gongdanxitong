package main

import (
	"net"
	"net/url"
	"strings"

	"github.com/abhinavxd/libredesk/internal/httputil"
	"github.com/abhinavxd/libredesk/internal/inbox/channel/livechat"
	"github.com/valyala/fasthttp"
)

const (
	hdrWidgetParentOrigin = "X-Libredesk-Parent-Origin"
	argWidgetParentOrigin = "parent_origin"
)

// validateWidgetParentOrigin binds browser widget traffic to the configured
// embedding origins. Origin headers are a browser boundary rather than a
// replacement for authentication or abuse controls.
func validateWidgetParentOrigin(ctx *fasthttp.RequestCtx, config livechat.Config) (string, bool) {
	headerOrigin := strings.TrimSpace(string(ctx.Request.Header.Peek(hdrWidgetParentOrigin)))
	queryOrigin := strings.TrimSpace(string(ctx.QueryArgs().Peek(argWidgetParentOrigin)))
	if headerOrigin != "" && queryOrigin != "" && !originsEqual(headerOrigin, queryOrigin) {
		return "", false
	}

	parentOrigin := headerOrigin
	if parentOrigin == "" {
		parentOrigin = queryOrigin
	}
	requestOrigin := strings.TrimSpace(string(ctx.Request.Header.Peek("Origin")))
	if parentOrigin == "" {
		parentOrigin = requestOrigin
	}
	if parentOrigin == "" || len(parentOrigin) > 512 {
		return "", false
	}
	canonicalParentOrigin, ok := canonicalWidgetOrigin(parentOrigin)
	if !ok {
		return "", false
	}
	parentOrigin = canonicalParentOrigin

	requestIsSameOrigin := requestOrigin == "" || originMatchesRequestHost(requestOrigin, string(ctx.Host()))
	if requestOrigin != "" && !requestIsSameOrigin && !originsEqual(requestOrigin, parentOrigin) {
		return "", false
	}

	trusted := config.TrustedDomains
	if len(trusted) == 0 {
		trusted = []string{string(ctx.Host())}
	}
	if !httputil.IsOriginTrusted(parentOrigin, trusted) {
		return "", false
	}

	if requestOrigin != "" && !requestIsSameOrigin {
		ctx.Response.Header.Set("Access-Control-Allow-Origin", requestOrigin)
		ctx.Response.Header.Set("Vary", "Origin")
	}
	return parentOrigin, true
}

func canonicalWidgetOrigin(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil || u.Hostname() == "" ||
		(u.Scheme != "http" && u.Scheme != "https") ||
		u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", false
	}
	scheme := strings.ToLower(u.Scheme)
	hostname := strings.ToLower(u.Hostname())
	port := u.Port()
	if (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		port = ""
	}
	host := hostname
	if port != "" {
		host = net.JoinHostPort(hostname, port)
	} else if strings.Contains(hostname, ":") {
		host = "[" + hostname + "]"
	}
	return scheme + "://" + host, true
}

func originMatchesRequestHost(origin, requestHost string) bool {
	u, err := url.Parse(strings.TrimSpace(origin))
	if err != nil || u.Host == "" {
		return false
	}
	return strings.EqualFold(u.Host, requestHost)
}

func originsEqual(a, b string) bool {
	ua, errA := url.Parse(strings.TrimSpace(a))
	ub, errB := url.Parse(strings.TrimSpace(b))
	if errA != nil || errB != nil || ua.Host == "" || ub.Host == "" {
		return false
	}
	return strings.EqualFold(ua.Scheme, ub.Scheme) && strings.EqualFold(ua.Host, ub.Host) && ua.Path == "" && ub.Path == "" && ua.RawQuery == "" && ub.RawQuery == "" && ua.Fragment == "" && ub.Fragment == ""
}
