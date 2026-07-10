package main

import (
	"strconv"

	amodels "github.com/abhinavxd/libredesk/internal/auth/models"
	"github.com/abhinavxd/libredesk/internal/envelope"
	"github.com/abhinavxd/libredesk/internal/stringutil"
	umodels "github.com/abhinavxd/libredesk/internal/user/models"
	"github.com/valyala/fasthttp"
	"github.com/zerodha/fastglue"
)

var (
	oidcStateSessKey = "oidc_state"
	oidcNextSessKey  = "oidc_next"
)

// handleOIDCLogin redirects to the OIDC provider for login.
func handleOIDCLogin(r *fastglue.Request) error {
	var (
		app             = r.Context.(*App)
		providerID, err = strconv.Atoi(r.RequestCtx.UserValue("id").(string))
	)
	if err != nil {
		app.lo.Error("error parsing provider id", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, app.i18n.T("globals.messages.somethingWentWrong"), nil, envelope.GeneralError)
	}

	// Set a state and save it in the session, to prevent CSRF attacks.
	state, err := stringutil.RandomAlphanumeric(32)
	if err != nil {
		app.lo.Error("error generating state", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, app.i18n.T("globals.messages.somethingWentWrong"), nil, envelope.GeneralError)
	}

	sessionValues := map[string]any{
		oidcStateSessKey: state,
		// For redirecting after login
		oidcNextSessKey: safeNextPath(string(r.RequestCtx.QueryArgs().Peek("next")), "/"),
	}

	if err = app.auth.SetSessionValues(r, sessionValues); err != nil {
		app.lo.Error("error saving state in session", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, app.i18n.T("globals.messages.somethingWentWrong"), nil, envelope.GeneralError)
	}

	authURL, err := app.auth.LoginURL(providerID, state)
	if err != nil {
		return sendErrorEnvelope(r, err)
	}
	return r.Redirect(authURL, fasthttp.StatusFound, nil, "")
}

// handleOIDCCallback receives the redirect callback from the OIDC provider and completes the handshake.
func handleOIDCCallback(r *fastglue.Request) error {
	var (
		app             = r.Context.(*App)
		code            = string(r.RequestCtx.QueryArgs().Peek("code"))
		state           = string(r.RequestCtx.QueryArgs().Peek("state"))
		providerID, err = strconv.Atoi(string(r.RequestCtx.UserValue("id").(string)))
		ip              = requestClientIP(app, r.RequestCtx)
	)
	if err != nil {
		app.lo.Error("error parsing provider id", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, app.i18n.T("globals.messages.somethingWentWrong"), nil, envelope.GeneralError)
	}

	// Compare the state from the session with the state from the query.
	sessionState, err := app.auth.GetSessionValue(r, oidcStateSessKey)
	if err != nil {
		app.lo.Error("error getting state from session", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, app.i18n.T("globals.messages.somethingWentWrong"), nil, envelope.GeneralError)
	}
	if state != sessionState {
		return r.SendErrorEnvelope(fasthttp.StatusForbidden, app.i18n.T("globals.messages.somethingWentWrong"), nil, envelope.GeneralError)
	}
	nextParam, _ := app.auth.GetSessionValue(r, oidcNextSessKey)
	redirectURL := "/"
	if nextStr, ok := nextParam.(string); ok && nextStr != "" {
		redirectURL = safeNextPath(nextStr, "/")
	}
	// State is single-use and the authenticated session must not reuse the
	// pre-authentication session identifier.
	if err := app.auth.DestroySession(r); err != nil {
		app.lo.Error("error consuming OIDC pre-auth session", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, app.i18n.T("globals.messages.somethingWentWrong"), nil, envelope.GeneralError)
	}

	// Hold a provider read lease through identity resolution and session
	// creation. Deletion takes the write side before revoking linked users.
	app.oidcAuthMu.RLock()
	defer app.oidcAuthMu.RUnlock()

	_, claims, err := app.auth.ExchangeOIDCToken(r.RequestCtx, providerID, code)
	if err != nil {
		app.lo.Error("error exchanging oidc token", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError,
			app.i18n.T("globals.messages.somethingWentWrong"), nil, envelope.GeneralError)
	}

	// Resolve a stable issuer+subject identity. Only the first login is allowed
	// to link by a verified email address.
	identityUserID, found, err := app.oidc.ResolveUserIdentity(claims.Issuer, claims.Sub)
	if err != nil {
		return sendErrorEnvelope(r, err)
	}
	var user umodels.User
	if found {
		user, err = app.user.GetAgent(identityUserID, "")
	} else {
		user, err = app.user.GetAgent(0, claims.Email)
		if err == nil {
			boundUserID, bindErr := app.oidc.BindUserIdentity(providerID, claims.Issuer, claims.Sub, user.ID, claims.Email)
			if bindErr != nil {
				return sendErrorEnvelope(r, bindErr)
			}
			if boundUserID != user.ID {
				return r.SendErrorEnvelope(fasthttp.StatusForbidden, app.i18n.T("globals.messages.somethingWentWrong"), nil, envelope.PermissionError)
			}
		}
	}
	if err != nil {
		return sendErrorEnvelope(r, err)
	}
	if !user.Enabled {
		return r.SendErrorEnvelope(fasthttp.StatusForbidden, app.i18n.T("user.accountDisabled"), nil, envelope.PermissionError)
	}

	if err := app.auth.SaveSession(amodels.User{
		ID:             user.ID,
		Email:          user.Email.String,
		FirstName:      user.FirstName,
		LastName:       user.LastName,
		Type:           user.Type,
		SessionVersion: user.SessionVersion,
	}, r); err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError,
			app.i18n.T("globals.messages.somethingWentWrong"), nil, envelope.GeneralError)
	}

	// Update last login time.
	if err := app.user.UpdateLastLoginAt(user.ID); err != nil {
		return sendErrorEnvelope(r, err)
	}

	app.user.InvalidateAgentCache(user.ID)

	// Insert activity log.
	if err := app.activityLog.Login(user.ID, user.Email.String, ip); err != nil {
		app.lo.Error("error creating login activity log", "error", err)
	}

	return r.RedirectURI(redirectURL, fasthttp.StatusFound, nil, "")
}
