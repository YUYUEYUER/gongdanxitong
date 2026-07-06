package auth

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
