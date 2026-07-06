package main

import (
	"strings"
	"time"

	amodels "github.com/abhinavxd/libredesk/internal/auth/models"
	cmodels "github.com/abhinavxd/libredesk/internal/conversation/models"
	"github.com/abhinavxd/libredesk/internal/envelope"
	inboxsvc "github.com/abhinavxd/libredesk/internal/inbox"
	notifier "github.com/abhinavxd/libredesk/internal/notification"
	tmpl "github.com/abhinavxd/libredesk/internal/template"
	turnstilesvc "github.com/abhinavxd/libredesk/internal/turnstile"
	"github.com/abhinavxd/libredesk/internal/user"
	umodels "github.com/abhinavxd/libredesk/internal/user/models"
	realip "github.com/ferluci/fast-realip"
	"github.com/valyala/fasthttp"
	"github.com/zerodha/fastglue"
)

type customerRegisterRequest struct {
	FirstName           string `json:"first_name"`
	LastName            string `json:"last_name"`
	Email               string `json:"email"`
	Password            string `json:"password"`
	CFTurnstileResponse string `json:"cf-turnstile-response"`
	TurnstileToken      string `json:"turnstile_token"`
}

type customerLoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type customerForgotPasswordRequest struct {
	Email          string `json:"email"`
	TurnstileToken string `json:"turnstile_token"`
}

type customerResetPasswordRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

type customerTicketCreateRequest struct {
	InboxID     int    `json:"inbox_id"`
	OrderNumber string `json:"order_number"`
	Subject     string `json:"subject"`
	Content     string `json:"content"`
	Attachments []int  `json:"attachments"`
}

type customerTicketMessageRequest struct {
	Message     string `json:"message"`
	Attachments []int  `json:"attachments"`
}

type customerTicketConfigResponse struct {
	Enabled        bool   `json:"enabled"`
	DefaultInboxID int    `json:"default_inbox_id"`
	InboxName      string `json:"inbox_name"`
}

type customerAuthResponse struct {
	ID        int    `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
}

type customerTicketDetailResponse struct {
	Conversation cmodels.Conversation `json:"conversation"`
	Messages     []cmodels.Message    `json:"messages"`
}

const customerPortalInboxName = "客户门户"

func handleCustomerTicketConfig(r *fastglue.Request) error {
	app := r.Context.(*App)

	inboxID, inboxName, err := resolveCustomerPortalInbox(app)
	if err != nil {
		return sendErrorEnvelope(r, err)
	}

	return r.SendEnvelope(customerTicketConfigResponse{
		Enabled:        inboxID > 0,
		DefaultInboxID: inboxID,
		InboxName:      inboxName,
	})
}

func handleCustomerRegister(r *fastglue.Request) error {
	app := r.Context.(*App)
	req := customerRegisterRequest{}

	if err := requireJSONPost(r.RequestCtx, app); err != nil {
		return sendErrorEnvelope(r, err)
	}
	if err := validateCSRFCookie(r, app); err != nil {
		return sendErrorEnvelope(r, err)
	}

	if err := r.Decode(&req, "json"); err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, app.i18n.T("errors.parsingRequest"), nil, envelope.InputError)
	}

	normalizeCustomerRegisterRequest(&req)
	if err := validateCustomerRegisterFields(app, req); err != nil {
		return sendErrorEnvelope(r, err)
	}

	ip := realip.FromRequest(r.RequestCtx)
	userAgent := string(r.RequestCtx.UserAgent())
	if err := checkCustomerRegisterRateLimit(r.RequestCtx, app, ip, userAgent, req); err != nil {
		return sendErrorEnvelope(r, err)
	}

	if err := validateTurnstileToken(r, req.turnstileResponse(), turnstilesvc.WithExpectedAction(turnstileActionCustomerRegister)); err != nil {
		app.lo.Warn(
			"customer register turnstile failed",
			"ip", ip,
			"user_agent", userAgent,
			"email_hash", sha256Hex(req.Email),
			"error", err,
		)
		return sendErrorEnvelope(r, err)
	}

	customer, err := app.user.RegisterCustomerContact(req.FirstName, req.LastName, req.Email, req.Password)
	if err != nil {
		return sendErrorEnvelope(r, err)
	}

	if err := app.customerAuth.SaveSession(amodels.User{
		ID:        customer.ID,
		Email:     customer.Email.String,
		FirstName: customer.FirstName,
		LastName:  customer.LastName,
		Type:      customer.Type,
	}, r); err != nil {
		app.lo.Error("error saving customer session", "error", err)
		return sendErrorEnvelope(r, envelope.NewError(envelope.GeneralError, app.i18n.T("globals.messages.somethingWentWrong"), nil))
	}
	if err := app.customerAuth.SetCSRFCookie(r); err != nil {
		app.lo.Error("error setting csrf cookie", "error", err)
		return sendErrorEnvelope(r, envelope.NewError(envelope.GeneralError, app.i18n.T("globals.messages.somethingWentWrong"), nil))
	}
	if err := app.user.UpdateLastLoginAt(customer.ID); err != nil {
		return sendErrorEnvelope(r, err)
	}

	return r.SendEnvelope(toCustomerAuthResponse(customer))
}

func handleCustomerLogin(r *fastglue.Request) error {
	app := r.Context.(*App)
	req := customerLoginRequest{}

	if err := r.Decode(&req, "json"); err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, app.i18n.T("errors.parsingRequest"), nil, envelope.InputError)
	}
	if req.Email == "" || req.Password == "" {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, app.i18n.T("globals.messages.badRequest"), nil, envelope.InputError)
	}

	customer, err := app.user.VerifyContactPassword(req.Email, []byte(req.Password))
	if err != nil {
		return sendErrorEnvelope(r, err)
	}

	if !customer.Enabled {
		return sendErrorEnvelope(r, envelope.NewError(envelope.PermissionError, app.i18n.T("user.accountDisabled"), nil))
	}

	if err := app.customerAuth.SaveSession(amodels.User{
		ID:        customer.ID,
		Email:     customer.Email.String,
		FirstName: customer.FirstName,
		LastName:  customer.LastName,
		Type:      customer.Type,
	}, r); err != nil {
		app.lo.Error("error saving customer session", "error", err)
		return sendErrorEnvelope(r, envelope.NewError(envelope.GeneralError, app.i18n.T("globals.messages.somethingWentWrong"), nil))
	}
	if err := app.customerAuth.SetCSRFCookie(r); err != nil {
		app.lo.Error("error setting csrf cookie", "error", err)
		return sendErrorEnvelope(r, envelope.NewError(envelope.GeneralError, app.i18n.T("globals.messages.somethingWentWrong"), nil))
	}
	if err := app.user.UpdateLastLoginAt(customer.ID); err != nil {
		return sendErrorEnvelope(r, err)
	}

	return r.SendEnvelope(toCustomerAuthResponse(customer))
}

func handleCustomerLogout(r *fastglue.Request) error {
	app := r.Context.(*App)

	if err := app.customerAuth.DestroySession(r); err != nil {
		app.lo.Error("error destroying customer session", "error", err)
		return sendErrorEnvelope(r, envelope.NewError(envelope.GeneralError, app.i18n.T("globals.messages.somethingWentWrong"), nil))
	}

	return r.SendEnvelope(true)
}

func handleGetCurrentCustomer(r *fastglue.Request) error {
	app := r.Context.(*App)
	auser := r.RequestCtx.UserValue("user").(amodels.User)

	customer, err := app.user.Get(auser.ID, "", []string{umodels.UserTypeContact})
	if err != nil {
		return sendErrorEnvelope(r, err)
	}

	return r.SendEnvelope(toCustomerAuthResponse(customer))
}

func handleCustomerForgotPassword(r *fastglue.Request) error {
	app := r.Context.(*App)
	req := customerForgotPasswordRequest{}

	if err := r.Decode(&req, "json"); err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, app.i18n.T("errors.parsingRequest"), nil, envelope.InputError)
	}
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	if req.Email == "" {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, app.i18n.Ts("globals.messages.empty", "name", "`email`"), nil, envelope.InputError)
	}
	req.TurnstileToken = strings.TrimSpace(req.TurnstileToken)
	if err := validateTurnstileToken(r, req.TurnstileToken); err != nil {
		return sendErrorEnvelope(r, err)
	}

	customer, err := app.user.GetContactByEmail(strings.TrimSpace(strings.ToLower(req.Email)))
	if err != nil {
		return r.SendEnvelope(true)
	}
	if !user.IsCustomerPortalRegistered(customer) {
		return r.SendEnvelope(true)
	}

	token, err := app.user.SetResetPasswordToken(customer.ID)
	if err != nil {
		return sendErrorEnvelope(r, err)
	}

	content, err := app.tmpl.RenderInMemoryTemplate(tmpl.TmplResetPassword, map[string]string{
		"ResetToken": token,
	})
	if err != nil {
		app.lo.Error("error rendering customer reset template", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, app.i18n.T("globals.messages.errorSendingPasswordResetEmail"), nil, envelope.GeneralError)
	}

	if err := app.notifier.Send(notifier.Message{
		RecipientEmails: []string{customer.Email.String},
		Subject:         "Customer password reset",
		Content:         content,
		Provider:        notifier.ProviderEmail,
	}); err != nil {
		app.lo.Error("error sending customer password reset email", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, app.i18n.T("globals.messages.errorSendingPasswordResetEmail"), nil, envelope.GeneralError)
	}

	return r.SendEnvelope(true)
}

func handleCustomerResetPassword(r *fastglue.Request) error {
	app := r.Context.(*App)
	req := customerResetPasswordRequest{}

	if err := r.Decode(&req, "json"); err != nil {
		return sendErrorEnvelope(r, envelope.NewError(envelope.InputError, app.i18n.T("errors.parsingRequest"), nil))
	}
	if req.Password == "" {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, app.i18n.Ts("globals.messages.empty", "name", "{globals.terms.password}"), nil, envelope.InputError)
	}

	id, err := app.user.ResetPassword(req.Token, req.Password)
	if err != nil {
		return sendErrorEnvelope(r, err)
	}

	customer, err := app.user.Get(id, "", []string{umodels.UserTypeContact})
	if err != nil {
		return sendErrorEnvelope(r, err)
	}
	if err := app.user.SaveCustomAttributes(customer.ID, map[string]any{"portal_registered": true}, false); err != nil {
		return sendErrorEnvelope(r, err)
	}

	return r.SendEnvelope(true)
}

func handleCustomerListTickets(r *fastglue.Request) error {
	app := r.Context.(*App)
	auser := r.RequestCtx.UserValue("user").(amodels.User)
	page, pageSize := getPagination(r)

	conversations, total, err := app.conversation.GetCustomerConversations(auser.ID, page, pageSize)
	if err != nil {
		return sendErrorEnvelope(r, err)
	}

	return r.SendEnvelope(envelope.PageResults{
		Results:    conversations,
		Total:      total,
		PerPage:    pageSize,
		TotalPages: calcTotalPages(total, pageSize),
		Page:       page,
	})
}

func handleCustomerGetTicket(r *fastglue.Request) error {
	app := r.Context.(*App)
	auser := r.RequestCtx.UserValue("user").(amodels.User)
	uuid := r.RequestCtx.UserValue("uuid").(string)

	conversation, err := app.conversation.GetConversation(0, uuid, "")
	if err != nil {
		return sendErrorEnvelope(r, err)
	}
	if conversation.ContactID != auser.ID {
		return r.SendErrorEnvelope(fasthttp.StatusForbidden, app.i18n.T("status.deniedPermission"), nil, envelope.PermissionError)
	}

	private := false
	messages, _, err := app.conversation.GetConversationMessages(uuid, 1, 500, &private, []string{cmodels.MessageIncoming, cmodels.MessageOutgoing})
	if err != nil {
		return sendErrorEnvelope(r, err)
	}
	for i := range messages {
		for j := range messages[i].Attachments {
			att := messages[i].Attachments[j]
			messages[i].Attachments[j].URL = app.media.GetURL(att.UUID, att.ContentType, att.Name)
		}
	}
	reverseMessages(messages)

	return r.SendEnvelope(customerTicketDetailResponse{
		Conversation: conversation,
		Messages:     messages,
	})
}

func handleCustomerCreateTicket(r *fastglue.Request) error {
	app := r.Context.(*App)
	auser := r.RequestCtx.UserValue("user").(amodels.User)
	req := customerTicketCreateRequest{}

	if err := r.Decode(&req, "json"); err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, app.i18n.T("errors.parsingRequest"), nil, envelope.InputError)
	}

	req.OrderNumber = strings.TrimSpace(req.OrderNumber)
	req.Subject = strings.TrimSpace(req.Subject)
	req.Content = strings.TrimSpace(req.Content)
	if req.Subject == "" {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, app.i18n.T("validation.subjectCannotBeEmpty"), nil, envelope.InputError)
	}
	if req.Content == "" {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, app.i18n.T("validation.messageCannotBeEmpty"), nil, envelope.InputError)
	}
	if err := validatePublicTicketOrderNumber(req.OrderNumber, app); err != nil {
		return sendErrorEnvelope(r, err)
	}

	media, err := getUnassociatedMedia(app, req.Attachments, auser.ID)
	if err != nil {
		return sendErrorEnvelope(r, err)
	}

	inboxID, _, err := resolveCustomerPortalInbox(app)
	if err != nil {
		return sendErrorEnvelope(r, err)
	}

	meta := map[string]any{
		"source":                "customer_portal",
		"reply_mode":            "in_app",
		"authenticated_user_id": auser.ID,
	}
	if req.OrderNumber != "" {
		meta["order_number"] = req.OrderNumber
	}

	conversationID, conversationUUID, err := app.conversation.CreateConversation(
		auser.ID,
		inboxID,
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
		return sendErrorEnvelope(r, envelope.NewError(envelope.GeneralError, app.i18n.T("globals.messages.somethingWentWrong"), nil))
	}

	messageContent := publicTicketMessageContent(req.Content, req.OrderNumber, app)
	if _, err := app.conversation.CreateContactMessage(media, auser.ID, conversationUUID, messageContent, cmodels.ContentTypeText, true); err != nil {
		_ = app.conversation.DeleteConversation(conversationUUID)
		return sendErrorEnvelope(r, envelope.NewError(envelope.GeneralError, app.i18n.T("globals.messages.errorSendingMessage"), nil))
	}

	conversation, err := app.conversation.GetConversation(conversationID, "", "")
	if err != nil {
		return sendErrorEnvelope(r, err)
	}

	return r.SendEnvelope(publicTicketCreateResponse{
		ConversationUUID: conversation.UUID,
		ReferenceNumber:  conversation.ReferenceNumber,
	})
}

func handleCustomerReplyTicket(r *fastglue.Request) error {
	app := r.Context.(*App)
	auser := r.RequestCtx.UserValue("user").(amodels.User)
	uuid := r.RequestCtx.UserValue("uuid").(string)
	req := customerTicketMessageRequest{}

	if err := r.Decode(&req, "json"); err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, app.i18n.T("errors.parsingRequest"), nil, envelope.InputError)
	}
	req.Message = strings.TrimSpace(req.Message)
	if req.Message == "" {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, app.i18n.T("validation.messageCannotBeEmpty"), nil, envelope.InputError)
	}

	media, err := getUnassociatedMedia(app, req.Attachments, auser.ID)
	if err != nil {
		return sendErrorEnvelope(r, err)
	}

	conversation, err := app.conversation.GetConversation(0, uuid, "")
	if err != nil {
		return sendErrorEnvelope(r, err)
	}
	if conversation.ContactID != auser.ID {
		return r.SendErrorEnvelope(fasthttp.StatusForbidden, app.i18n.T("status.deniedPermission"), nil, envelope.PermissionError)
	}

	message, err := app.conversation.CreateContactMessage(media, auser.ID, uuid, req.Message, cmodels.ContentTypeText, false)
	if err != nil {
		return sendErrorEnvelope(r, envelope.NewError(envelope.GeneralError, app.i18n.T("globals.messages.errorSendingMessage"), nil))
	}

	for i := range message.Attachments {
		att := message.Attachments[i]
		message.Attachments[i].URL = app.media.GetURL(att.UUID, att.ContentType, att.Name)
	}

	return r.SendEnvelope(message)
}

func toCustomerAuthResponse(customer umodels.User) customerAuthResponse {
	return customerAuthResponse{
		ID:        customer.ID,
		FirstName: customer.FirstName,
		LastName:  customer.LastName,
		Email:     customer.Email.String,
	}
}

func calcTotalPages(total, pageSize int) int {
	if pageSize <= 0 {
		return 0
	}
	return (total + pageSize - 1) / pageSize
}

func reverseMessages(messages []cmodels.Message) {
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
}

func resolveCustomerPortalInbox(app *App) (int, string, error) {
	inboxes, err := app.inbox.GetAll()
	if err != nil {
		return 0, "", envelope.NewError(envelope.GeneralError, app.i18n.T("globals.messages.somethingWentWrong"), nil)
	}

	for _, inbox := range inboxes {
		if !inbox.Enabled {
			continue
		}
		if inbox.Channel == inboxsvc.ChannelLiveChat && inbox.Name == customerPortalInboxName {
			return inbox.ID, inbox.Name, nil
		}
	}

	return 0, "", envelope.NewErrorWithCode(
		envelope.GeneralError,
		fasthttp.StatusServiceUnavailable,
		app.i18n.T("publicTicket.noInboxConfigured"),
		nil,
	)
}
