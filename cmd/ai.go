package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	amodels "github.com/abhinavxd/libredesk/internal/auth/models"
	"github.com/abhinavxd/libredesk/internal/envelope"
	"github.com/valyala/fasthttp"
	"github.com/zerodha/fastglue"
)

const (
	maxAIContentBytes       = 20_000
	maxAIPromptKeyBytes     = 100
	maxAIRequestsPerHour    = 30
	maxConcurrentAIRequests = 4
)

var aiCompletionSlots = make(chan struct{}, maxConcurrentAIRequests)

type aiCompletionReq struct {
	PromptKey string `json:"prompt_key"`
	Content   string `json:"content"`
}

type providerUpdateReq struct {
	Provider string `json:"provider"`
	APIKey   string `json:"api_key"`
}

// handleAICompletion handles AI completion requests
func handleAICompletion(r *fastglue.Request) error {
	var (
		app = r.Context.(*App)
		req = aiCompletionReq{}
	)

	if err := r.Decode(&req, "json"); err != nil {
		return sendErrorEnvelope(r, envelope.NewError(envelope.InputError, app.i18n.T("errors.parsingRequest"), nil))
	}
	if err := validateAICompletionRequest(req); err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, err.Error(), nil, envelope.InputError)
	}

	user, ok := r.RequestCtx.UserValue("user").(amodels.User)
	if !ok || user.ID <= 0 {
		return r.SendErrorEnvelope(fasthttp.StatusUnauthorized, app.i18n.T("status.deniedPermission"), nil, envelope.UnauthorizedError)
	}
	limit, err := app.rateLimit.CheckWindow(r.RequestCtx, fmt.Sprintf("ai_completion:user:%d", user.ID), time.Hour, maxAIRequestsPerHour)
	if err != nil {
		app.lo.Error("AI completion rate limit unavailable", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusServiceUnavailable, app.i18n.T("globals.messages.somethingWentWrong"), nil, envelope.GeneralError)
	}
	if !limit.Allowed {
		r.RequestCtx.Response.Header.Set("Retry-After", strconv.Itoa(int(limit.RetryAfter.Seconds())))
		return r.SendErrorEnvelope(fasthttp.StatusTooManyRequests, "AI request quota exceeded", nil, envelope.InputError)
	}

	select {
	case aiCompletionSlots <- struct{}{}:
		defer func() { <-aiCompletionSlots }()
	default:
		return r.SendErrorEnvelope(fasthttp.StatusTooManyRequests, "AI service is busy", nil, envelope.GeneralError)
	}

	resp, err := app.ai.Completion(req.PromptKey, req.Content)
	if err != nil {
		return sendErrorEnvelope(r, err)
	}
	return r.SendEnvelope(resp)
}

func validateAICompletionRequest(req aiCompletionReq) error {
	if strings.TrimSpace(req.PromptKey) == "" {
		return fmt.Errorf("prompt_key is required")
	}
	if len(req.PromptKey) > maxAIPromptKeyBytes {
		return fmt.Errorf("prompt_key exceeds %d bytes", maxAIPromptKeyBytes)
	}
	if strings.TrimSpace(req.Content) == "" {
		return fmt.Errorf("content is required")
	}
	if len(req.Content) > maxAIContentBytes {
		return fmt.Errorf("content exceeds %d bytes", maxAIContentBytes)
	}
	return nil
}

// handleGetAIPrompts returns AI prompts
func handleGetAIPrompts(r *fastglue.Request) error {
	var (
		app = r.Context.(*App)
	)
	resp, err := app.ai.GetPrompts()
	if err != nil {
		return sendErrorEnvelope(r, err)
	}
	return r.SendEnvelope(resp)
}

// handleUpdateAIProvider updates the AI provider
func handleUpdateAIProvider(r *fastglue.Request) error {
	var (
		app = r.Context.(*App)
		req providerUpdateReq
	)
	if err := r.Decode(&req, "json"); err != nil {
		return sendErrorEnvelope(r, envelope.NewError(envelope.InputError, app.i18n.T("errors.parsingRequest"), nil))
	}
	if err := app.ai.UpdateProvider(req.Provider, req.APIKey); err != nil {
		return sendErrorEnvelope(r, err)
	}
	return r.SendEnvelope("Provider updated successfully")
}
