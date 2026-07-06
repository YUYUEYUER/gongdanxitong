package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"

	amodels "github.com/abhinavxd/libredesk/internal/auth/models"
	cmodels "github.com/abhinavxd/libredesk/internal/conversation/models"
	"github.com/abhinavxd/libredesk/internal/envelope"
	inboxsvc "github.com/abhinavxd/libredesk/internal/inbox"
	"github.com/abhinavxd/libredesk/internal/moderation"
	"github.com/abhinavxd/libredesk/internal/stringutil"
	umodels "github.com/abhinavxd/libredesk/internal/user/models"
	"github.com/redis/go-redis/v9"
	"github.com/valyala/fasthttp"
	"github.com/volatiletech/null/v9"
	"github.com/zerodha/fastglue"
)

const (
	publicTicketCaptchaTTL       = 5 * time.Minute
	publicTicketCaptchaKeyPrefix = "public_ticket_captcha:"
	publicTicketMaxNameLength    = 128
	publicTicketMaxEmailLength   = 254
	publicTicketMaxSubjectLength = 255
	publicTicketMaxContentLength = 10000
	publicTicketMaxOrderNoLength = 128
)

type publicTicketInboxOption struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type publicTicketConfigResponse struct {
	Enabled            bool                      `json:"enabled"`
	DefaultInboxID     int                       `json:"default_inbox_id"`
	Inboxes            []publicTicketInboxOption `json:"inboxes"`
	RequireLogin       bool                      `json:"require_login"`
	RequireOrderNumber bool                      `json:"require_order_number"`
}

type publicTicketCaptchaResponse struct {
	CaptchaToken    string `json:"captcha_token"`
	Challenge       string `json:"challenge"`
	ExpiresInSecond int    `json:"expires_in_seconds"`
}

type publicTicketCreateRequest struct {
	InboxID        int    `json:"inbox_id"`
	Name           string `json:"name"`
	Email          string `json:"email"`
	OrderNumber    string `json:"order_number"`
	Subject        string `json:"subject"`
	Content        string `json:"content"`
	TurnstileToken string `json:"turnstile_token"`
	CaptchaToken   string `json:"captcha_token"`
	CaptchaAnswer  string `json:"captcha_answer"`
}

type publicTicketCreateResponse struct {
	ConversationUUID string `json:"conversation_uuid"`
	ReferenceNumber  string `json:"reference_number"`
}

func handleGetPublicTicketConfig(r *fastglue.Request) error {
	app := r.Context.(*App)

	inboxes, err := getPublicTicketInboxes(app)
	if err != nil {
		return sendErrorEnvelope(r, err)
	}

	resp := publicTicketConfigResponse{
		Enabled:            len(inboxes) > 0,
		Inboxes:            inboxes,
		DefaultInboxID:     0,
		RequireLogin:       publicTicketRequireLogin(),
		RequireOrderNumber: publicTicketRequireOrderNumber(),
	}
	if len(inboxes) > 0 {
		resp.DefaultInboxID = inboxes[0].ID
	}

	return r.SendEnvelope(resp)
}

func handleGetPublicTicketCaptcha(r *fastglue.Request) error {
	app := r.Context.(*App)

	challenge, answer, err := generatePublicTicketCaptcha()
	if err != nil {
		app.lo.Error("error generating public ticket captcha", "error", err)
		return sendErrorEnvelope(r, envelope.NewError(envelope.GeneralError, app.i18n.T("globals.messages.somethingWentWrong"), nil))
	}

	token, err := stringutil.RandomAlphanumeric(32)
	if err != nil {
		app.lo.Error("error generating public ticket captcha token", "error", err)
		return sendErrorEnvelope(r, envelope.NewError(envelope.GeneralError, app.i18n.T("globals.messages.somethingWentWrong"), nil))
	}

	if err := app.redis.Set(app.ctx, publicTicketCaptchaKeyPrefix+token, answer, publicTicketCaptchaTTL).Err(); err != nil {
		app.lo.Error("error storing public ticket captcha", "error", err)
		return sendErrorEnvelope(r, envelope.NewError(envelope.GeneralError, app.i18n.T("globals.messages.somethingWentWrong"), nil))
	}

	return r.SendEnvelope(publicTicketCaptchaResponse{
		CaptchaToken:    token,
		Challenge:       challenge,
		ExpiresInSecond: int(publicTicketCaptchaTTL.Seconds()),
	})
}

func handleCreatePublicTicket(r *fastglue.Request) error {
	var (
		app                 = r.Context.(*App)
		req                 = publicTicketCreateRequest{}
		authenticatedUser   amodels.User
		isAuthenticatedUser bool
	)

	if err := r.Decode(&req, "json"); err != nil {
		app.lo.Error("error decoding public ticket request", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, app.i18n.T("errors.parsingRequest"), nil, envelope.InputError)
	}

	req.Name = strings.TrimSpace(req.Name)
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	req.OrderNumber = strings.TrimSpace(req.OrderNumber)
	req.Subject = strings.TrimSpace(req.Subject)
	req.Content = strings.TrimSpace(req.Content)
	req.TurnstileToken = strings.TrimSpace(req.TurnstileToken)
	req.CaptchaToken = strings.TrimSpace(req.CaptchaToken)
	req.CaptchaAnswer = strings.TrimSpace(req.CaptchaAnswer)

	if user, ok := r.RequestCtx.UserValue("user").(amodels.User); ok && user.ID > 0 {
		authenticatedUser = user
		isAuthenticatedUser = true
		req.Email = strings.ToLower(strings.TrimSpace(user.Email))

		defaultName := strings.TrimSpace(strings.Join([]string{user.FirstName, user.LastName}, " "))
		if defaultName == "" {
			defaultName = user.Email
		}
		if req.Name == "" {
			req.Name = defaultName
		}
	}

	if publicTicketRequireLogin() && !isAuthenticatedUser {
		return r.SendErrorEnvelope(
			fasthttp.StatusUnauthorized,
			app.i18n.T("publicTicket.loginRequiredDescription"),
			nil,
			envelope.UnauthorizedError,
		)
	}

	inboxOptions, err := getPublicTicketInboxes(app)
	if err != nil {
		return sendErrorEnvelope(r, err)
	}

	selectedInboxID, err := resolvePublicTicketInboxID(req.InboxID, inboxOptions, app)
	if err != nil {
		return sendErrorEnvelope(r, err)
	}

	useTurnstile := app.turnstile != nil && app.turnstile.Enabled()
	if err := validatePublicTicketRequest(req, app, isAuthenticatedUser, !useTurnstile); err != nil {
		return sendErrorEnvelope(r, err)
	}

	blocked, err := app.user.IsEmailBlocked(req.Email)
	if err != nil {
		app.lo.Error("error checking blocked status for public ticket email", "email", req.Email, "error", err)
		return sendErrorEnvelope(r, envelope.NewError(envelope.GeneralError, app.i18n.T("globals.messages.somethingWentWrong"), nil))
	}
	if blocked {
		return r.SendErrorEnvelope(fasthttp.StatusForbidden, app.i18n.T("publicTicket.emailBlocked"), nil, envelope.PermissionError)
	}

	if _, found := moderation.FirstBlockedTerm(req.Name, req.Subject, req.Content); found {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, app.i18n.T("publicTicket.profanityBlocked"), nil, envelope.InputError)
	}

	if useTurnstile {
		if err := validateTurnstileToken(r, req.TurnstileToken); err != nil {
			return sendErrorEnvelope(r, err)
		}
	} else {
		if err := validatePublicTicketCaptcha(app, req.CaptchaToken, req.CaptchaAnswer); err != nil {
			return sendErrorEnvelope(r, err)
		}
	}

	firstName, lastName := splitPublicTicketName(req.Name)
	contact := umodels.User{
		Email:            null.StringFrom(req.Email),
		FirstName:        firstName,
		LastName:         lastName,
		CustomAttributes: json.RawMessage(`{}`),
	}

	if err := app.user.CreateContact(&contact); err != nil {
		app.lo.Error("error creating public ticket contact", "email", req.Email, "error", err)
		return sendErrorEnvelope(r, envelope.NewError(envelope.GeneralError, app.i18n.T("globals.messages.somethingWentWrong"), nil))
	}

	if isAuthenticatedUser {
		if err := app.user.UpdateContactBasicInfo(contact.ID, firstName, lastName, req.Email); err != nil {
			app.lo.Error("error updating public ticket contact", "contact_id", contact.ID, "error", err)
			return sendErrorEnvelope(r, envelope.NewError(envelope.GeneralError, app.i18n.T("globals.messages.somethingWentWrong"), nil))
		}
	}

	meta := map[string]any{
		"source": "public_ticket_form",
	}
	if req.OrderNumber != "" {
		meta["order_number"] = req.OrderNumber
	}
	if isAuthenticatedUser {
		meta["reply_mode"] = "in_app"
		meta["authenticated_user_id"] = authenticatedUser.ID
	}

	conversationID, conversationUUID, err := app.conversation.CreateConversation(
		contact.ID,
		selectedInboxID,
		"",
		time.Now(),
		req.Subject,
		true,
		meta,
		nil,
		0,
		0,
	)
	if err != nil {
		app.lo.Error("error creating public ticket conversation", "email", req.Email, "error", err)
		return sendErrorEnvelope(r, envelope.NewError(envelope.GeneralError, app.i18n.T("globals.messages.somethingWentWrong"), nil))
	}

	messageContent := publicTicketMessageContent(req.Content, req.OrderNumber, app)
	if _, err := app.conversation.CreateContactMessage(nil, contact.ID, conversationUUID, messageContent, cmodels.ContentTypeText, true); err != nil {
		app.lo.Error("error creating public ticket message", "conversation_uuid", conversationUUID, "error", err)
		if delErr := app.conversation.DeleteConversation(conversationUUID); delErr != nil {
			app.lo.Error("error rolling back public ticket conversation", "conversation_uuid", conversationUUID, "error", delErr)
		}
		return sendErrorEnvelope(r, envelope.NewError(envelope.GeneralError, app.i18n.T("globals.messages.errorSendingMessage"), nil))
	}

	conversation, err := app.conversation.GetConversation(conversationID, "", "")
	if err != nil {
		app.lo.Error("error fetching public ticket conversation", "conversation_uuid", conversationUUID, "error", err)
		return sendErrorEnvelope(r, envelope.NewError(envelope.GeneralError, app.i18n.T("globals.messages.somethingWentWrong"), nil))
	}

	return r.SendEnvelope(publicTicketCreateResponse{
		ConversationUUID: conversation.UUID,
		ReferenceNumber:  conversation.ReferenceNumber,
	})
}

func getPublicTicketInboxes(app *App) ([]publicTicketInboxOption, error) {
	inboxes, err := app.inbox.GetAll()
	if err != nil {
		return nil, envelope.NewError(envelope.GeneralError, app.i18n.T("globals.messages.somethingWentWrong"), nil)
	}

	options := make([]publicTicketInboxOption, 0, len(inboxes))
	for _, inbox := range inboxes {
		if !inbox.Enabled || inbox.Channel != inboxsvc.ChannelEmail {
			continue
		}

		options = append(options, publicTicketInboxOption{
			ID:   inbox.ID,
			Name: inbox.Name,
		})
	}

	sort.Slice(options, func(i, j int) bool {
		return options[i].Name < options[j].Name
	})

	return options, nil
}

func resolvePublicTicketInboxID(requestedID int, inboxes []publicTicketInboxOption, app *App) (int, error) {
	if len(inboxes) == 0 {
		return 0, envelope.NewErrorWithCode(envelope.GeneralError, fasthttp.StatusServiceUnavailable, app.i18n.T("publicTicket.noInboxConfigured"), nil)
	}

	if requestedID == 0 {
		if len(inboxes) == 1 {
			return inboxes[0].ID, nil
		}
		return 0, envelope.NewError(envelope.InputError, app.i18n.T("publicTicket.selectInbox"), nil)
	}

	for _, inbox := range inboxes {
		if inbox.ID == requestedID {
			return requestedID, nil
		}
	}

	return 0, envelope.NewError(envelope.InputError, app.i18n.T("publicTicket.invalidInbox"), nil)
}

func validatePublicTicketRequest(req publicTicketCreateRequest, app *App, isAuthenticatedUser, requireCaptcha bool) error {
	if req.Name == "" {
		return envelope.NewError(envelope.InputError, app.i18n.T("publicTicket.nameRequired"), nil)
	}
	if req.Email == "" {
		if isAuthenticatedUser {
			return envelope.NewError(envelope.GeneralError, app.i18n.T("globals.messages.somethingWentWrong"), nil)
		}
		return envelope.NewError(envelope.InputError, app.i18n.Ts("globals.messages.required", "name", "{globals.terms.email}"), nil)
	}
	if req.Subject == "" {
		return envelope.NewError(envelope.InputError, app.i18n.T("validation.subjectCannotBeEmpty"), nil)
	}
	if req.Content == "" {
		return envelope.NewError(envelope.InputError, app.i18n.T("validation.messageCannotBeEmpty"), nil)
	}
	if err := validatePublicTicketOrderNumber(req.OrderNumber, app); err != nil {
		return err
	}
	if requireCaptcha && (req.CaptchaToken == "" || req.CaptchaAnswer == "") {
		return envelope.NewError(envelope.InputError, app.i18n.T("publicTicket.captchaRequired"), nil)
	}
	if len(req.Name) > publicTicketMaxNameLength {
		return envelope.NewError(envelope.InputError, app.i18n.Ts("globals.messages.maxLength", "max", strconv.Itoa(publicTicketMaxNameLength)), nil)
	}
	if len(req.Email) > publicTicketMaxEmailLength || !stringutil.ValidEmail(req.Email) {
		return envelope.NewError(envelope.InputError, app.i18n.T("validation.invalidEmail"), nil)
	}
	if len(req.Subject) > publicTicketMaxSubjectLength {
		return envelope.NewError(envelope.InputError, app.i18n.Ts("globals.messages.maxLength", "max", strconv.Itoa(publicTicketMaxSubjectLength)), nil)
	}
	if len(req.Content) > publicTicketMaxContentLength {
		return envelope.NewError(envelope.InputError, app.i18n.Ts("globals.messages.maxLength", "max", strconv.Itoa(publicTicketMaxContentLength)), nil)
	}
	return nil
}

func validatePublicTicketOrderNumber(orderNumber string, app *App) error {
	if publicTicketRequireOrderNumber() && orderNumber == "" {
		return envelope.NewError(envelope.InputError, app.i18n.T("publicTicket.orderNumberRequired"), nil)
	}
	if len(orderNumber) > publicTicketMaxOrderNoLength {
		return envelope.NewError(envelope.InputError, app.i18n.Ts("globals.messages.maxLength", "max", strconv.Itoa(publicTicketMaxOrderNoLength)), nil)
	}
	return nil
}

func publicTicketMessageContent(content, orderNumber string, app *App) string {
	if strings.TrimSpace(orderNumber) == "" {
		return content
	}
	return fmt.Sprintf("%s: %s\n\n%s", app.i18n.T("publicTicket.orderNumberLabel"), orderNumber, content)
}

func validatePublicTicketCaptcha(app *App, token, answer string) error {
	key := publicTicketCaptchaKeyPrefix + token

	expected, err := app.redis.Get(app.ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return envelope.NewError(envelope.InputError, app.i18n.T("publicTicket.captchaExpired"), nil)
		}
		app.lo.Error("error fetching public ticket captcha", "error", err)
		return envelope.NewError(envelope.GeneralError, app.i18n.T("globals.messages.somethingWentWrong"), nil)
	}

	_ = app.redis.Del(app.ctx, key).Err()

	if strings.TrimSpace(strings.ToLower(answer)) != strings.TrimSpace(strings.ToLower(expected)) {
		return envelope.NewError(envelope.InputError, app.i18n.T("publicTicket.captchaInvalid"), nil)
	}

	return nil
}

func splitPublicTicketName(name string) (string, string) {
	fields := strings.Fields(name)
	if len(fields) == 0 {
		return "", ""
	}
	if len(fields) == 1 {
		return fields[0], ""
	}
	return fields[0], strings.Join(fields[1:], " ")
}

func generatePublicTicketCaptcha() (string, string, error) {
	left, err := cryptoRandomInt(2, 10)
	if err != nil {
		return "", "", err
	}

	right, err := cryptoRandomInt(1, 10)
	if err != nil {
		return "", "", err
	}

	useAddition, err := cryptoRandomInt(0, 2)
	if err != nil {
		return "", "", err
	}

	if useAddition == 0 {
		return fmt.Sprintf("%d + %d = ?", left, right), strconv.Itoa(left + right), nil
	}

	if left < right {
		left, right = right, left
	}
	return fmt.Sprintf("%d - %d = ?", left, right), strconv.Itoa(left - right), nil
}

func publicTicketRequireLogin() bool {
	if !ko.Exists("app.public_ticket_require_login") {
		return true
	}
	return ko.Bool("app.public_ticket_require_login")
}

func publicTicketRequireOrderNumber() bool {
	if !ko.Exists("app.public_ticket_require_order_number") {
		return false
	}
	return ko.Bool("app.public_ticket_require_order_number")
}

func cryptoRandomInt(minInclusive, maxExclusive int) (int, error) {
	if minInclusive >= maxExclusive {
		return 0, fmt.Errorf("invalid random range")
	}

	n, err := rand.Int(rand.Reader, big.NewInt(int64(maxExclusive-minInclusive)))
	if err != nil {
		return 0, err
	}

	return int(n.Int64()) + minInclusive, nil
}
