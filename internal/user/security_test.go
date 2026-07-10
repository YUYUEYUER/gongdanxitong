package user

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSecurityTokenHashDoesNotStoreRawToken(t *testing.T) {
	t.Parallel()

	token := "SecretOneTimeToken123"
	sum := sha256.Sum256([]byte(token))
	require.Equal(t, hex.EncodeToString(sum[:]), securityTokenHash(token))
	require.NotContains(t, securityTokenHash(token), token)
	require.Empty(t, securityTokenHash(""))
}

func TestCustomerRegistrationQueriesEnforceAtomicHashedActivation(t *testing.T) {
	t.Parallel()

	b, err := efs.ReadFile("queries.sql")
	require.NoError(t, err)
	queries := string(b)

	require.Contains(t, queries, "WHERE token_hash = $1 AND expires_at > now()")
	require.Contains(t, queries, "FOR UPDATE;")
	require.Contains(t, queries, "DELETE FROM customer_portal_registrations WHERE id = $1")
	require.NotContains(t, queries, "customer_portal_registrations\n    (email, first_name, last_name, password_hash")
	require.Contains(t, queries, "-- name: get-registered-portal-contact-by-email")
	require.Contains(t, queries, "ORDER BY (external_user_id IS NULL) DESC, id ASC")
	require.Contains(t, queries, "session_version = session_version + 1")
	require.Contains(t, queries, "api_secret = NULL")
	require.Contains(t, queries, "portal_registered = true")
	require.NotContains(t, queries, "custom_attributes->>'portal_registered'")
	require.Contains(t, queries, "CASE WHEN portal_registered THEN email")
	require.Contains(t, queries, "WHEN users.portal_registered THEN users.email")
	require.Contains(t, queries, "SET owner_user_id = $2")
	require.Contains(t, queries, "ORDER BY id\nFOR UPDATE")
	require.Contains(t, queries, "-- name: delete-merged-visitor")
	require.Contains(t, queries, "-- name: delete-duplicate-visitor-participants")
	require.Contains(t, queries, "enabled IS DISTINCT FROM $8::boolean")
	require.Contains(t, queries, "$7 IS NOT NULL OR $8::boolean = false")
	require.Contains(t, queries, "enabled = COALESCE($8::boolean, enabled)")
	require.NotContains(t, strings.ToLower(queries), "where token = $1")
}

func TestEncodeUserCustomAttributesRemovesAuthenticationState(t *testing.T) {
	t.Parallel()

	encoded, err := encodeUserCustomAttributes(map[string]any{
		"portal_registered": true,
		"tier":              "gold",
	})
	require.NoError(t, err)
	require.JSONEq(t, `{"tier":"gold"}`, string(encoded))

	_, err = encodeUserCustomAttributes(map[string]any{
		"oversized": strings.Repeat("x", maxUserCustomAttributesJSONBytes),
	})
	require.Error(t, err)

	_, err = encodeUserCustomAttributes(map[string]any{
		"safe": map[string]any{"__proto__": map[string]any{"polluted": true}},
	})
	require.Error(t, err)
}
