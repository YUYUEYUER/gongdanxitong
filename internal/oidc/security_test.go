package oidc

import (
	"testing"

	"github.com/abhinavxd/libredesk/internal/oidc/models"
	"github.com/stretchr/testify/require"
)

func TestProviderDeletionQueriesRevokeLinkedSessions(t *testing.T) {
	t.Parallel()

	b, err := efs.ReadFile("queries.sql")
	require.NoError(t, err)
	queries := string(b)
	require.Contains(t, queries, "SELECT id FROM oidc WHERE id = $1 FOR UPDATE")
	require.Contains(t, queries, "WHERE provider_id = $1\nFOR UPDATE")
	require.Contains(t, queries, "SET session_version = session_version + 1")
	require.Contains(t, queries, "api_key = NULL")
	require.Contains(t, queries, "api_secret = NULL")
}

func TestOIDCSecurityBoundaryChangesRequireRevocation(t *testing.T) {
	t.Parallel()

	base := models.OIDC{
		Enabled:      true,
		Provider:     "Custom",
		ProviderURL:  "https://id.example.com",
		ClientID:     "client",
		ClientSecret: "secret",
		Name:         "Identity",
		LogoURL:      "/logo.png",
	}
	cosmetic := base
	cosmetic.Name = "Renamed"
	cosmetic.LogoURL = "/new-logo.png"
	require.False(t, oidcSecurityBoundaryChanged(base, cosmetic))

	changes := []models.OIDC{}
	changed := base
	changed.Enabled = false
	changes = append(changes, changed)
	changed = base
	changed.ProviderURL = "https://new-id.example.com"
	changes = append(changes, changed)
	changed = base
	changed.Provider = "Google"
	changes = append(changes, changed)
	changed = base
	changed.ClientID = "other-client"
	changes = append(changes, changed)
	changed = base
	changed.ClientSecret = "rotated"
	changes = append(changes, changed)

	for _, next := range changes {
		require.True(t, oidcSecurityBoundaryChanged(base, next))
	}
}
