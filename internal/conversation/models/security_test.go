package models

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStripCSATUUIDRemovesPublicSurveyToken(t *testing.T) {
	t.Parallel()

	message := Message{
		Content: "https://support.example.com/csat/survey-secret",
		Meta:    json.RawMessage(`{"is_csat":true,"csat_uuid":"survey-secret","csat_submitted":false}`),
	}
	message.StripCSATUUID()

	var meta map[string]any
	require.NoError(t, json.Unmarshal(message.Meta, &meta))
	require.NotContains(t, meta, "csat_uuid")
	require.Equal(t, true, meta["is_csat"])
	require.NotContains(t, message.Content, "survey-secret")
}
