package httputil

import (
	"net"
	"net/url"
	"strings"
)

func IsValidHTTPURL(raw string) bool {
	u, err := url.ParseRequestURI(raw)
	if err != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

// IsOriginTrusted checks if the given origin is trusted based on the trusted domains list
// Expects trustedDomains to be a list of domain strings, which can include wildcards.
// Like "*.example.com" or "example.com".
func IsOriginTrusted(origin string, trustedDomains []string) bool {
	if len(trustedDomains) == 0 {
		return false
	}

	originHost, originPort := parseOriginHostPort(origin)
	if originHost == "" {
		return false
	}

	for _, trusted := range trustedDomains {
		trustedHost, trustedPort := parseTrustedDomain(trusted)
		if trustedHost != "" && portMatches(originHost, originPort, trustedHost, trustedPort) && hostMatches(originHost, trustedHost) {
			return true
		}
	}

	return false
}

// parseOriginHostPort extracts a canonical host and effective port from an
// HTTP Origin value. Non-TLS origins are accepted only for loopback
// development hosts.
func parseOriginHostPort(origin string) (host, port string) {
	u, err := url.Parse(strings.TrimSpace(strings.ToLower(origin)))
	if err != nil || u.User != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return "", ""
	}
	host = strings.TrimSuffix(u.Hostname(), ".")
	if host == "" || (u.Scheme != "https" && !isLoopbackHost(host)) {
		return "", ""
	}
	port = u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return host, port
}

// parseTrustedDomain extracts host and port from trusted domain entry
func parseTrustedDomain(domain string) (host, port string) {
	domain = strings.TrimSpace(strings.ToLower(domain))

	if strings.HasPrefix(domain, "http://") || strings.HasPrefix(domain, "https://") {
		u, err := url.Parse(domain)
		if err != nil {
			return "", ""
		}
		host = strings.TrimSuffix(u.Hostname(), ".")
		port = u.Port()
		return host, port
	}

	// Handle non-URL patterns (wildcards/domains)
	if strings.ContainsAny(domain, "/?#@ \\") {
		return "", ""
	}
	if splitHost, splitPort, err := net.SplitHostPort(domain); err == nil {
		host, port = splitHost, splitPort
	} else {
		host = domain
	}
	host = strings.TrimSuffix(host, ".")
	return host, port
}

// portMatches checks if ports are compatible
func portMatches(originHost, originPort, trustedHost, trustedPort string) bool {
	if trustedPort != "" {
		return trustedPort == originPort
	}
	if isLoopbackHost(originHost) && isLoopbackHost(strings.TrimPrefix(trustedHost, "*.")) {
		return true
	}
	return originPort == "443"
}

// hostMatches checks if host matches trusted pattern
func hostMatches(origin, trusted string) bool {
	if strings.HasPrefix(trusted, "*.") {
		base := trusted[2:]
		return strings.HasSuffix(origin, "."+base)
	}
	return trusted == origin
}

func isLoopbackHost(host string) bool {
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
