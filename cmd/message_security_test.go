package main

import (
	"strings"
	"testing"

	cmodels "github.com/abhinavxd/libredesk/internal/conversation/models"
	"github.com/abhinavxd/libredesk/internal/envelope"
	"github.com/abhinavxd/libredesk/internal/ratelimit"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"
)

func TestValidateMessageRequestBudgets(t *testing.T) {
	app := testSecurityApp(t)

	t.Run("normalizes and deduplicates", func(t *testing.T) {
		req := messageReq{
			Message:     "hello",
			Attachments: []int{2, 2, 3},
			To:          []string{" PERSON@example.com "},
			CC:          []string{"person@example.com", "second@example.com"},
			Mentions: []cmodels.MentionInput{
				{Type: cmodels.MentionTypeAgent, ID: 4},
				{Type: cmodels.MentionTypeAgent, ID: 4},
			},
		}
		require.NoError(t, validateMessageRequest(&req, app))
		require.Equal(t, []int{2, 3}, req.Attachments)
		require.Equal(t, []string{"person@example.com"}, req.To)
		require.Equal(t, []string{"second@example.com"}, req.CC)
		require.Len(t, req.Mentions, 1)
	})

	for name, req := range map[string]messageReq{
		"empty":             {},
		"oversized content": {Message: strings.Repeat("x", maxAgentMessageBytes+1)},
		"invalid attachment": {
			Message: "hello", Attachments: []int{0},
		},
		"invalid recipient": {
			Message: "hello", To: []string{"not-an-email"},
		},
		"invalid mention": {
			Message: "hello", Mentions: []cmodels.MentionInput{{Type: "unknown", ID: 1}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := validateMessageRequest(&req, app)
			requireEnvelopeError(t, err, envelope.InputError, fasthttp.StatusBadRequest)
		})
	}

	recipients := make([]string, maxAgentMessageRecipients+1)
	for i := range recipients {
		recipients[i] = "user" + strings.Repeat("x", i%3) + string(rune('a'+i%26)) + "-" + strings.Repeat("0", i/26) + "@example.com"
	}
	err := validateMessageRequest(&messageReq{Message: "hello", To: recipients}, app)
	requireEnvelopeError(t, err, envelope.InputError, fasthttp.StatusBadRequest)
}

func TestValidateCreateConversationResourceBudget(t *testing.T) {
	app := testSecurityApp(t)
	req := createConversationRequest{
		Email:            " Person@Example.com ",
		FirstName:        " First ",
		LastName:         " Last ",
		Subject:          " Subject ",
		Content:          "hello",
		Attachments:      []int{1, 1, 2},
		CustomAttributes: map[string]any{"tier": "gold"},
	}
	require.NoError(t, validateCreateConversationResourceBudget(&req, app))
	require.Equal(t, "person@example.com", req.Email)
	require.Equal(t, []int{1, 2}, req.Attachments)

	req.CustomAttributes = map[string]any{"tier": strings.Repeat("x", maxCustomAttributesJSONBytes)}
	err := validateCreateConversationResourceBudget(&req, app)
	requireEnvelopeError(t, err, envelope.InputError, fasthttp.StatusBadRequest)
}

func TestSanitizeContactCustomAttributesRejectsPortalState(t *testing.T) {
	app := testSecurityApp(t)
	attributes := map[string]any{
		"portal_registered": true,
		"tier":              "gold",
	}
	require.NoError(t, sanitizeContactCustomAttributes(attributes, app))
	require.NotContains(t, attributes, "portal_registered")
	require.Equal(t, "gold", attributes["tier"])
}

func TestCustomAttributesRejectPrototypePollutionKeys(t *testing.T) {
	app := testSecurityApp(t)
	attributes := map[string]any{
		"safe": map[string]any{
			"constructor": map[string]any{"prototype": map[string]any{"polluted": true}},
		},
	}
	require.Error(t, validateCustomAttributesResourceBudget(attributes, app))
}

func TestCheckAgentWriteRateLimit(t *testing.T) {
	app := testSecurityApp(t)
	server := miniredis.RunT(t)
	app.rateLimit = ratelimit.New(redis.NewClient(&redis.Options{Addr: server.Addr()}))
	for range maxAgentWritesPerHour {
		require.NoError(t, checkAgentWriteRateLimit(app, 42))
	}
	err := checkAgentWriteRateLimit(app, 42)
	requireEnvelopeError(t, err, envelope.RateLimitError, fasthttp.StatusTooManyRequests)

	server.Close()
	err = checkAgentWriteRateLimit(app, 7)
	requireEnvelopeError(t, err, envelope.GeneralError, fasthttp.StatusServiceUnavailable)
}

func TestValidateAgentWriteBodySize(t *testing.T) {
	app := testSecurityApp(t)
	require.NoError(t, validateAgentWriteBodySize(make([]byte, maxAgentWriteBodyBytes), app))
	requireEnvelopeError(t, validateAgentWriteBodySize(make([]byte, maxAgentWriteBodyBytes+1), app), envelope.InputError, fasthttp.StatusBadRequest)
}
