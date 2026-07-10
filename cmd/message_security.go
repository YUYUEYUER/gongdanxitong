package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	cmodels "github.com/abhinavxd/libredesk/internal/conversation/models"
	"github.com/abhinavxd/libredesk/internal/envelope"
	"github.com/abhinavxd/libredesk/internal/jsonutil"
	"github.com/abhinavxd/libredesk/internal/stringutil"
	"github.com/valyala/fasthttp"
)

const (
	maxAgentWriteBodyBytes       = 512 * 1024
	maxAgentMessageBytes         = 100 * 1024
	maxAgentMessageAttachments   = 10
	maxAgentMessageRecipients    = 50
	maxAgentMessageMentions      = 50
	maxAgentMessageEchoIDBytes   = 128
	maxConversationSubjectBytes  = 255
	maxContactNameBytes          = 140
	maxContactEmailBytes         = 320
	maxExternalUserIDBytes       = 512
	maxCustomAttributeCount      = 100
	maxCustomAttributesJSONBytes = 64 * 1024
	maxCustomAttributeKeyBytes   = 128
	maxAgentWritesPerHour        = 500
	agentWriteRateLimitWindow    = time.Hour
)

func validateAgentWriteBodySize(body []byte, app *App) error {
	if len(body) > maxAgentWriteBodyBytes {
		return badAgentWriteInput(app)
	}
	return nil
}

func validateMessageRequest(req *messageReq, app *App) error {
	if len(req.Message) > maxAgentMessageBytes || len(req.Attachments) > maxAgentMessageAttachments ||
		len(req.Mentions) > maxAgentMessageMentions || len(req.EchoID) > maxAgentMessageEchoIDBytes {
		return badAgentWriteInput(app)
	}
	if strings.TrimSpace(req.Message) == "" && len(req.Attachments) == 0 {
		return badAgentWriteInput(app)
	}

	attachments, ok := uniquePositiveIDs(req.Attachments)
	if !ok || len(attachments) > maxAgentMessageAttachments {
		return badAgentWriteInput(app)
	}
	req.Attachments = attachments

	seenRecipients := make(map[string]struct{})
	var valid bool
	req.To, valid = normalizeRecipients(req.To, seenRecipients)
	if !valid {
		return envelope.NewError(envelope.InputError, app.i18n.T("validation.invalidEmail"), nil)
	}
	req.CC, valid = normalizeRecipients(req.CC, seenRecipients)
	if !valid {
		return envelope.NewError(envelope.InputError, app.i18n.T("validation.invalidEmail"), nil)
	}
	req.BCC, valid = normalizeRecipients(req.BCC, seenRecipients)
	if !valid || len(seenRecipients) > maxAgentMessageRecipients {
		return badAgentWriteInput(app)
	}

	mentions := make([]cmodels.MentionInput, 0, len(req.Mentions))
	seenMentions := make(map[string]struct{}, len(req.Mentions))
	for _, mention := range req.Mentions {
		if mention.ID <= 0 || (mention.Type != cmodels.MentionTypeAgent && mention.Type != cmodels.MentionTypeTeam) {
			return badAgentWriteInput(app)
		}
		key := fmt.Sprintf("%s:%d", mention.Type, mention.ID)
		if _, ok := seenMentions[key]; ok {
			continue
		}
		seenMentions[key] = struct{}{}
		mentions = append(mentions, mention)
	}
	req.Mentions = mentions
	return nil
}

func validateCreateConversationResourceBudget(req *createConversationRequest, app *App) error {
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	req.FirstName = strings.TrimSpace(req.FirstName)
	req.LastName = strings.TrimSpace(req.LastName)
	req.ExternalUserID = strings.TrimSpace(req.ExternalUserID)
	req.Subject = strings.TrimSpace(req.Subject)

	if len(req.Content) > maxAgentMessageBytes || len(req.Subject) > maxConversationSubjectBytes ||
		len(req.FirstName) > maxContactNameBytes || len(req.LastName) > maxContactNameBytes ||
		len(req.Email) > maxContactEmailBytes || len(req.ExternalUserID) > maxExternalUserIDBytes ||
		len(req.Attachments) > maxAgentMessageAttachments || len(req.CustomAttributes) > maxCustomAttributeCount {
		return badAgentWriteInput(app)
	}
	attachments, ok := uniquePositiveIDs(req.Attachments)
	if !ok || len(attachments) > maxAgentMessageAttachments {
		return badAgentWriteInput(app)
	}
	req.Attachments = attachments

	return validateCustomAttributesResourceBudget(req.CustomAttributes, app)
}

func validateCustomAttributesResourceBudget(attributes map[string]any, app *App) error {
	if err := jsonutil.ValidateSafeObjectKeys(attributes, jsonutil.DefaultMaxObjectDepth); err != nil {
		return badAgentWriteInput(app)
	}
	if len(attributes) > maxCustomAttributeCount {
		return badAgentWriteInput(app)
	}
	for key := range attributes {
		if len(key) == 0 || len(key) > maxCustomAttributeKeyBytes {
			return badAgentWriteInput(app)
		}
	}
	encoded, err := json.Marshal(attributes)
	if err != nil || len(encoded) > maxCustomAttributesJSONBytes {
		return badAgentWriteInput(app)
	}
	return nil
}

func sanitizeContactCustomAttributes(attributes map[string]any, app *App) error {
	delete(attributes, "portal_registered")
	return validateCustomAttributesResourceBudget(attributes, app)
}

func checkAgentWriteRateLimit(app *App, userID int) error {
	if app.rateLimit == nil {
		return agentWriteRateLimitUnavailable(app)
	}
	ctx := app.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	result, err := app.rateLimit.CheckWindow(
		ctx,
		fmt.Sprintf("rate_limit:agent_write:user:%d", userID),
		agentWriteRateLimitWindow,
		maxAgentWritesPerHour,
	)
	if err != nil {
		app.lo.Error("agent write rate limit unavailable", "user_id", userID, "error", err)
		return agentWriteRateLimitUnavailable(app)
	}
	if !result.Allowed {
		return envelope.NewError(envelope.RateLimitError, app.i18n.T("globals.messages.tooManyRequests"), nil)
	}
	return nil
}

func normalizeRecipients(values []string, seen map[string]struct{}) ([]string, bool) {
	out := make([]string, 0, len(values))
	for _, raw := range values {
		address := strings.ToLower(strings.TrimSpace(raw))
		if len(address) == 0 || len(address) > maxContactEmailBytes || !stringutil.ValidEmail(address) {
			return nil, false
		}
		if _, ok := seen[address]; ok {
			continue
		}
		seen[address] = struct{}{}
		out = append(out, address)
	}
	return out, true
}

func uniquePositiveIDs(values []int) ([]int, bool) {
	out := make([]int, 0, len(values))
	seen := make(map[int]struct{}, len(values))
	for _, id := range values {
		if id <= 0 {
			return nil, false
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out, true
}

func badAgentWriteInput(app *App) error {
	return envelope.NewError(envelope.InputError, app.i18n.T("globals.messages.badRequest"), nil)
}

func agentWriteRateLimitUnavailable(app *App) error {
	return envelope.NewErrorWithCode(
		envelope.GeneralError,
		fasthttp.StatusServiceUnavailable,
		app.i18n.T("globals.messages.somethingWentWrong"),
		nil,
	)
}
