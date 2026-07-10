package csat

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpdateQueryIsSingleUse(t *testing.T) {
	t.Parallel()

	b, err := efs.ReadFile("queries.sql")
	require.NoError(t, err)
	require.Contains(t, string(b), "WHERE uuid = $1 AND response_timestamp IS NULL")
}

func TestCreateCSATUsesConversationUniqueConflict(t *testing.T) {
	t.Parallel()

	queries, err := efs.ReadFile("queries.sql")
	require.NoError(t, err)
	queryText := string(queries)
	require.Contains(t, queryText, "ON CONFLICT (conversation_id) DO NOTHING")
	require.NotContains(t, queryText, "WHERE NOT EXISTS")
}
