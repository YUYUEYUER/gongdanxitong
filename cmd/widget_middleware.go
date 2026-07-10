package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/abhinavxd/libredesk/internal/envelope"
	"github.com/abhinavxd/libredesk/internal/httputil"
	"github.com/abhinavxd/libredesk/internal/inbox/channel/livechat"
	imodels "github.com/abhinavxd/libredesk/internal/inbox/models"
	umodels "github.com/abhinavxd/libredesk/internal/user/models"
	"github.com/google/uuid"
	"github.com/valyala/fasthttp"
	"github.com/zerodha/fastglue"
)

const (
	ctxWidgetContactID = "widget_contact_id"
	ctxWidgetIsVisitor = "widget_is_visitor"
	ctxWidgetInbox     = "widget_inbox"
	ctxWidgetConfig    = "widget_config"
	ctxWidgetOrigin    = "widget_parent_origin"
	ctxWidgetToken     = "widget_session_token"

	hdrWidgetInboxID      = "X-Libredesk-Inbox-ID"
	hdrWidgetVisitorToken = "X-Libredesk-Visitor-Token"
	hdrClearVisitorToken  = "X-Libredesk-Clear-Visitor"
)

// validateWidgetInbox middleware validates the inbox from the request header or query param,
// checks IP/domain restrictions, and sets inbox + config in context.
func validateWidgetInbox(next func(*fastglue.Request) error) func(*fastglue.Request) error {
	return func(r *fastglue.Request) error {
		app := r.Context.(*App)

		inboxUUID := string(r.RequestCtx.Request.Header.Peek(hdrWidgetInboxID))
		if inboxUUID == "" {
			inboxUUID = string(r.RequestCtx.QueryArgs().Peek("inbox_id"))
		}
		if inboxUUID == "" {
			return r.SendErrorEnvelope(fasthttp.StatusBadRequest, app.i18n.Ts("globals.messages.required", "name", "{globals.terms.inbox}"), nil, envelope.InputError)
		}

		// Require a UUID here so widget callers cannot enumerate inboxes by numeric ID.
		if _, err := uuid.Parse(inboxUUID); err != nil {
			return r.SendErrorEnvelope(fasthttp.StatusBadRequest, app.i18n.T("validation.notFoundInbox"), nil, envelope.InputError)
		}

		inbox, err := app.inbox.GetDBRecord(inboxUUID)
		if err != nil {
			app.lo.Error("error fetching inbox", "inbox_uuid", inboxUUID, "error", err)
			return r.SendErrorEnvelope(fasthttp.StatusBadRequest, app.i18n.T("validation.notFoundInbox"), nil, envelope.InputError)
		}
		if !inbox.Enabled || inbox.Channel != livechat.ChannelLiveChat {
			return r.SendErrorEnvelope(fasthttp.StatusBadRequest, app.i18n.T("validation.notFoundInbox"), nil, envelope.InputError)
		}

		var config livechat.Config
		if err := json.Unmarshal(inbox.Config, &config); err != nil {
			app.lo.Error("error parsing live chat config", "error", err)
			return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, app.i18n.T("globals.messages.somethingWentWrong"), nil, envelope.GeneralError)
		}
		parentOrigin, ok := validateWidgetParentOrigin(r.RequestCtx, config)
		if !ok {
			return r.SendErrorEnvelope(fasthttp.StatusForbidden, app.i18n.T("status.deniedPermission"), nil, envelope.PermissionError)
		}

		if len(config.BlockedIPs) > 0 {
			clientIP := app.rateLimit.ClientIP(r.RequestCtx)
			if httputil.IsIPBlocked(clientIP, config.BlockedIPs) {
				return r.SendErrorEnvelope(fasthttp.StatusForbidden, app.i18n.T("widget.ipBlocked"), nil, envelope.PermissionError)
			}
		}

		r.RequestCtx.SetUserValue(ctxWidgetInbox, inbox)
		r.RequestCtx.SetUserValue(ctxWidgetConfig, config)
		r.RequestCtx.SetUserValue(ctxWidgetOrigin, parentOrigin)
		return next(r)
	}
}

// widgetAuth middleware validates the session token from the Authorization header
// using Redis lookup. Wraps validateWidgetInbox for inbox validation.
// For /conversations/init without a token, allows visitor creation.
func widgetAuth(next func(*fastglue.Request) error) func(*fastglue.Request) error {
	return validateWidgetInbox(func(r *fastglue.Request) error {
		app := r.Context.(*App)
		inbox, err := getWidgetInbox(r)
		if err != nil {
			return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, app.i18n.T("globals.messages.somethingWentWrong"), nil, envelope.GeneralError)
		}
		config, err := getWidgetConfig(r)
		if err != nil {
			return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, app.i18n.T("globals.messages.somethingWentWrong"), nil, envelope.GeneralError)
		}

		authHeader := strings.TrimSpace(string(r.RequestCtx.Request.Header.Peek("Authorization")))
		token := ""
		if strings.HasPrefix(authHeader, "Bearer ") {
			token = strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
		} else if authHeader != "" {
			return r.SendErrorEnvelope(fasthttp.StatusUnauthorized, app.i18n.T("globals.terms.unAuthorized"), nil, envelope.UnauthorizedError)
		}
		if token == "" {
			token = getWidgetTokenCookie(r.RequestCtx, inbox.UUID, widgetSessionCookie)
		}

		// For init endpoint, allow requests without token (visitor creation).
		if token == "" && strings.HasSuffix(string(r.RequestCtx.Path()), "/conversations/init") {
			return next(r)
		}

		if token == "" || len(token) > 256 {
			return r.SendErrorEnvelope(fasthttp.StatusUnauthorized, app.i18n.T("globals.terms.unAuthorized"), nil, envelope.UnauthorizedError)
		}

		parentOrigin, err := getWidgetParentOrigin(r)
		if err != nil {
			return r.SendErrorEnvelope(fasthttp.StatusForbidden, app.i18n.T("status.deniedPermission"), nil, envelope.PermissionError)
		}
		session, err := loadSession(app, token, inbox, config, parentOrigin)
		if err != nil {
			clearWidgetTokenCookie(r.RequestCtx, inbox.UUID, widgetSessionCookie)
			return r.SendErrorEnvelope(fasthttp.StatusUnauthorized, app.i18n.T("globals.terms.unAuthorized"), nil, envelope.UnauthorizedError)
		}

		// Verify session belongs to this inbox.
		if session.InboxID != inbox.ID {
			return r.SendErrorEnvelope(fasthttp.StatusUnauthorized, app.i18n.T("globals.terms.unAuthorized"), nil, envelope.UnauthorizedError)
		}

		// Verify user exists, is enabled, and is a contact or visitor.
		u, err := app.user.Get(session.UserID, "", []string{umodels.UserTypeContact, umodels.UserTypeVisitor})
		if err != nil || !u.Enabled || u.SessionVersion != session.UserSessionVersion {
			_ = deleteSessionToken(app, token)
			return r.SendErrorEnvelope(fasthttp.StatusUnauthorized, app.i18n.T("globals.terms.unAuthorized"), nil, envelope.UnauthorizedError)
		}

		r.RequestCtx.SetUserValue(ctxWidgetContactID, session.UserID)
		r.RequestCtx.SetUserValue(ctxWidgetIsVisitor, session.IsVisitor)
		r.RequestCtx.SetUserValue(ctxWidgetToken, token)
		// Migrate a legacy parent-page bearer token, and keep the partitioned
		// cookie lifetime aligned with the Redis session's sliding expiry.
		cookieTTL := getSessionDuration(config)
		if session.IsVisitor {
			cookieTTL = defaultSessionTTL
		}
		setWidgetTokenCookie(r.RequestCtx, inbox.UUID, token, widgetSessionCookie, cookieTTL)

		// Merge visitor to contact if visitor token is provided.
		visitorToken := string(r.RequestCtx.Request.Header.Peek(hdrWidgetVisitorToken))
		if visitorToken == "" {
			visitorToken = getWidgetTokenCookie(r.RequestCtx, inbox.UUID, widgetVisitorCookie)
		}
		if visitorToken != "" && session.ExternalUserID != "" && session.UserID > 0 {
			visitorSession, vErr := loadSession(app, visitorToken, inbox, config, parentOrigin)
			if vErr == nil && visitorSession.IsVisitor && visitorSession.UserID > 0 && visitorSession.UserID != session.UserID && visitorSession.InboxID == inbox.ID {
				visitorUser, userErr := app.user.Get(visitorSession.UserID, "", []string{umodels.UserTypeVisitor})
				if userErr != nil || !visitorUser.Enabled || visitorUser.SessionVersion != visitorSession.UserSessionVersion {
					_ = deleteSessionToken(app, visitorToken)
					return r.SendErrorEnvelope(fasthttp.StatusUnauthorized, app.i18n.T("globals.terms.unAuthorized"), nil, envelope.UnauthorizedError)
				}
				if err := app.user.MergeVisitorToContact(visitorSession.UserID, session.UserID); err != nil {
					app.lo.Error("error merging visitor to contact", "visitor_id", visitorSession.UserID, "contact_id", session.UserID, "error", err)
				} else {
					app.lo.Info("merged visitor to contact", "visitor_id", visitorSession.UserID, "contact_id", session.UserID)
					if err := deleteSessionToken(app, visitorToken); err != nil {
						app.lo.Error("error revoking merged visitor session", "error", err)
						return r.SendErrorEnvelope(fasthttp.StatusServiceUnavailable, app.i18n.T("globals.messages.somethingWentWrong"), nil, envelope.GeneralError)
					}
					clearWidgetTokenCookie(r.RequestCtx, inbox.UUID, widgetVisitorCookie)
					r.RequestCtx.Response.Header.Set(hdrClearVisitorToken, "true")
				}
			}
		}

		return next(r)
	})
}

// getWidgetContactID extracts contact ID from request context.
func getWidgetContactID(r *fastglue.Request) (int, error) {
	val := r.RequestCtx.UserValue(ctxWidgetContactID)
	if val == nil {
		return 0, fmt.Errorf("widget middleware not applied: missing contact ID in context")
	}
	contactID, ok := val.(int)
	if !ok {
		return 0, fmt.Errorf("invalid contact ID type in context")
	}
	return contactID, nil
}

// getWidgetIsVisitor extracts the visitor flag from request context.
func getWidgetIsVisitor(r *fastglue.Request) bool {
	val := r.RequestCtx.UserValue(ctxWidgetIsVisitor)
	if val == nil {
		return true
	}
	v, ok := val.(bool)
	if !ok {
		return true
	}
	return v
}

// getWidgetSessionToken returns the authenticated token placed in context by
// widgetAuth. It is exposed only to the isolated widget frame so the token can
// remain in memory for Authorization headers and WebSocket joins.
func getWidgetSessionToken(r *fastglue.Request) (string, error) {
	val := r.RequestCtx.UserValue(ctxWidgetToken)
	token, ok := val.(string)
	if !ok || token == "" {
		return "", fmt.Errorf("widget middleware not applied: missing session token")
	}
	return token, nil
}

// getWidgetInbox extracts inbox model from request context.
func getWidgetInbox(r *fastglue.Request) (imodels.Inbox, error) {
	val := r.RequestCtx.UserValue(ctxWidgetInbox)
	if val == nil {
		return imodels.Inbox{}, fmt.Errorf("widget middleware not applied: missing inbox in context")
	}
	inbox, ok := val.(imodels.Inbox)
	if !ok {
		return imodels.Inbox{}, fmt.Errorf("invalid inbox type in context")
	}
	return inbox, nil
}

// getWidgetConfig extracts parsed livechat config from request context.
func getWidgetConfig(r *fastglue.Request) (livechat.Config, error) {
	val := r.RequestCtx.UserValue(ctxWidgetConfig)
	if val == nil {
		return livechat.Config{}, fmt.Errorf("widget middleware not applied: missing config in context")
	}
	config, ok := val.(livechat.Config)
	if !ok {
		return livechat.Config{}, fmt.Errorf("invalid config type in context")
	}
	return config, nil
}

func getWidgetParentOrigin(r *fastglue.Request) (string, error) {
	value := r.RequestCtx.UserValue(ctxWidgetOrigin)
	origin, ok := value.(string)
	if !ok || origin == "" {
		return "", fmt.Errorf("widget middleware not applied: missing parent origin")
	}
	return origin, nil
}
