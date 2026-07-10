package httputil

import "testing"

func TestIsOriginTrusted(t *testing.T) {
	tests := []struct {
		name    string
		origin  string
		trusted []string
		want    bool
	}{
		{name: "exact TLS origin", origin: "https://support.example.com", trusted: []string{"support.example.com"}, want: true},
		{name: "wildcard subdomain", origin: "https://shop.example.com", trusted: []string{"*.example.com"}, want: true},
		{name: "wildcard excludes base", origin: "https://example.com", trusted: []string{"*.example.com"}, want: false},
		{name: "explicit port", origin: "https://example.com:8443", trusted: []string{"example.com:8443"}, want: true},
		{name: "unlisted port", origin: "https://example.com:8443", trusted: []string{"example.com"}, want: false},
		{name: "suffix confusion", origin: "https://example.com.attacker.test", trusted: []string{"example.com"}, want: false},
		{name: "insecure public origin", origin: "http://example.com", trusted: []string{"example.com"}, want: false},
		{name: "loopback development", origin: "http://localhost:5173", trusted: []string{"localhost"}, want: true},
		{name: "origin path rejected", origin: "https://example.com/path", trusted: []string{"example.com"}, want: false},
		{name: "userinfo rejected", origin: "https://example.com@attacker.test", trusted: []string{"attacker.test"}, want: false},
		{name: "null rejected", origin: "null", trusted: []string{"null"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsOriginTrusted(tt.origin, tt.trusted); got != tt.want {
				t.Fatalf("IsOriginTrusted(%q, %v) = %v, want %v", tt.origin, tt.trusted, got, tt.want)
			}
		})
	}
}
