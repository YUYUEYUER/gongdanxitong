package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	fsessionmodels "github.com/abhinavxd/libredesk/internal/auth/models"
	"github.com/alicebob/miniredis/v2"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"
	"github.com/zerodha/fastglue"
	"github.com/zerodha/logf"
	"golang.org/x/oauth2"
)

func TestReloadPreservesSecurityConfig(t *testing.T) {
	t.Parallel()

	a := &Auth{
		cfg: Config{
			Providers: []Provider{
				{ID: 1, Provider: "old"},
			},
			SecureCookies:   true,
			SessionLifetime: 2 * time.Hour,
			CookieName:      "libredesk_customer_session",
		},
	}

	var issuer string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_, _ = fmt.Fprintf(w, `{
				"issuer": %q,
				"authorization_endpoint": %q,
				"token_endpoint": %q,
				"jwks_uri": %q
			}`, issuer, issuer+"/auth", issuer+"/token", issuer+"/keys")
		case "/keys":
			_, _ = w.Write([]byte(`{"keys":[]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	issuer = srv.URL
	a.httpClient = srv.Client()

	err := a.Reload(Config{
		Providers: []Provider{
			{ID: 2, Provider: "new", ProviderURL: issuer, ClientID: "client-id"},
		},
	})
	require.NoError(t, err)
	require.True(t, a.cfg.SecureCookies)
	require.Equal(t, 2*time.Hour, a.cfg.SessionLifetime)
	require.Equal(t, "libredesk_customer_session", a.cfg.CookieName)
	require.Len(t, a.cfg.Providers, 1)
	require.Equal(t, 2, a.cfg.Providers[0].ID)
	require.Equal(t, "new", a.cfg.Providers[0].Provider)
}

func TestOIDCProviderURLRequiresHTTPS(t *testing.T) {
	t.Parallel()

	require.NoError(t, validateOIDCProviderURL("https://accounts.example.com/tenant"))
	for _, raw := range []string{
		"http://accounts.example.com",
		"https://user:pass@accounts.example.com",
		"https://accounts.example.com?target=internal",
		"https://accounts.example.com#fragment",
		"file:///etc/passwd",
	} {
		require.Error(t, validateOIDCProviderURL(raw), raw)
	}
}

func TestOIDCDiscoveryBlocksPrivateAddress(t *testing.T) {
	t.Parallel()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"issuer":"blocked"}`))
	}))
	defer srv.Close()

	_, err := discoverOIDCProvider(context.Background(), newOIDCHTTPClient(), srv.URL)
	require.Error(t, err)
	require.Contains(t, strings.ToLower(err.Error()), "blocked")
}

func TestOIDCDiscoveryRejectsHTTPMetadataEndpoint(t *testing.T) {
	t.Parallel()

	var issuer string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `{
			"issuer": %q,
			"authorization_endpoint": %q,
			"token_endpoint": %q,
			"jwks_uri": "http://keys.example.com/jwks"
		}`, issuer, issuer+"/auth", issuer+"/token")
	}))
	defer srv.Close()
	issuer = srv.URL

	_, err := discoverOIDCProvider(context.Background(), srv.Client(), issuer)
	require.Error(t, err)
	require.Contains(t, err.Error(), "HTTPS")
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestOIDCResponsesAreSizeLimited(t *testing.T) {
	t.Parallel()

	transport := limitedOIDCTransport{base: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(strings.Repeat("x", maxOIDCResponseBytes+1))),
			Header:     make(http.Header),
		}, nil
	})}
	req, err := http.NewRequest(http.MethodGet, "https://accounts.example.com", nil)
	require.NoError(t, err)
	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	_, err = io.ReadAll(resp.Body)
	require.Error(t, err)
}

func TestOIDCTransportRejectsDiscoveredHTTPEndpoint(t *testing.T) {
	t.Parallel()

	called := false
	transport := limitedOIDCTransport{base: roundTripFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, errors.New("must not be called")
	})}
	req, err := http.NewRequest(http.MethodPost, "http://accounts.example.com/token", nil)
	require.NoError(t, err)

	_, err = transport.RoundTrip(req)
	require.Error(t, err)
	require.False(t, called)
}

func TestValidateOIDCClaims(t *testing.T) {
	t.Parallel()

	valid := OIDCclaim{Email: "agent@example.com", EmailVerified: true, Issuer: "https://issuer.example", Sub: "subject-1"}
	require.NoError(t, ValidateOIDCClaims(valid))

	tests := []OIDCclaim{
		{Email: valid.Email, Issuer: valid.Issuer, Sub: valid.Sub},
		{EmailVerified: true, Issuer: valid.Issuer, Sub: valid.Sub},
		{Email: valid.Email, EmailVerified: true, Sub: valid.Sub},
		{Email: valid.Email, EmailVerified: true, Issuer: valid.Issuer},
	}
	for _, claims := range tests {
		require.Error(t, ValidateOIDCClaims(claims))
	}
}

func TestSessionRoundTripPreservesSessionVersion(t *testing.T) {
	mini := miniredis.RunT(t)
	rd := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() { _ = rd.Close() })
	logger := logf.New(logf.Opts{})
	a, err := New(Config{CookieName: "test_session", SessionLifetime: time.Hour}, nil, rd, &logger)
	require.NoError(t, err)
	anonymousReq := &fastglue.Request{RequestCtx: &fasthttp.RequestCtx{}}
	_, err = a.ValidateSession(anonymousReq)
	require.Error(t, err)
	require.Empty(t, mini.Keys(), "anonymous validation must not allocate a Redis session")

	loginCtx := &fasthttp.RequestCtx{}
	loginReq := &fastglue.Request{RequestCtx: loginCtx}
	require.NoError(t, a.SaveSession(fsessionmodels.User{
		ID:             42,
		Email:          "agent@example.com",
		Type:           "agent",
		SessionVersion: 7,
	}, loginReq))

	cookieValue := loginCtx.Response.Header.PeekCookie("test_session")
	require.NotEmpty(t, cookieValue)
	requestCtx := &fasthttp.RequestCtx{}
	requestCtx.Request.Header.SetCookie("test_session", string(cookieValue))
	request := &fastglue.Request{RequestCtx: requestCtx}
	user, err := a.ValidateSession(request)
	require.NoError(t, err)
	require.Equal(t, 42, user.ID)
	require.Equal(t, int64(7), user.SessionVersion)
}

func TestRemoveProviderRevokesRuntimeProvider(t *testing.T) {
	t.Parallel()

	a := &Auth{
		cfg:       Config{Providers: []Provider{{ID: 1}, {ID: 2}}},
		oauthCfgs: map[int]oauth2.Config{1: {}, 2: {}},
		verifiers: map[int]*oidc.IDTokenVerifier{1: {}, 2: {}},
	}
	a.RemoveProvider(1)

	require.NotContains(t, a.oauthCfgs, 1)
	require.NotContains(t, a.verifiers, 1)
	require.Len(t, a.cfg.Providers, 1)
	require.Equal(t, 2, a.cfg.Providers[0].ID)
}
