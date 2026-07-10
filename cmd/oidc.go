package main

import (
	"strconv"

	"github.com/abhinavxd/libredesk/internal/envelope"
	"github.com/abhinavxd/libredesk/internal/oidc/models"
	"github.com/valyala/fasthttp"
	"github.com/zerodha/fastglue"
)

// handleGetAllOIDC returns all OIDC records
func handleGetAllOIDC(r *fastglue.Request) error {
	app := r.Context.(*App)
	out, err := app.oidc.GetAll()
	if err != nil {
		return sendErrorEnvelope(r, err)
	}
	// Replace secrets with dummy values.
	for i := range out {
		out[i].ClearSecrets()
	}
	return r.SendEnvelope(out)
}

// handleGetOIDC returns an OIDC record by id.
func handleGetOIDC(r *fastglue.Request) error {
	var app = r.Context.(*App)
	id, err := strconv.Atoi(r.RequestCtx.UserValue("id").(string))
	if err != nil || id <= 0 {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, app.i18n.T("globals.messages.somethingWentWrong"), nil, envelope.InputError)
	}
	o, err := app.oidc.Get(id)
	if err != nil {
		return sendErrorEnvelope(r, err)
	}
	o.ClearSecrets()
	return r.SendEnvelope(o)
}

// handleCreateOIDC creates a new OIDC record.
func handleCreateOIDC(r *fastglue.Request) error {
	var (
		app = r.Context.(*App)
		req = models.OIDC{}
	)
	if err := r.Decode(&req, "json"); err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, app.i18n.T("errors.parsingRequest"), nil, envelope.GeneralError)
	}

	// Test OIDC provider URL by performing a discovery.
	if err := app.auth.TestProvider(req.ProviderURL); err != nil {
		return sendErrorEnvelope(r, err)
	}
	app.oidcAuthMu.Lock()
	defer app.oidcAuthMu.Unlock()

	createdOIDC, err := app.oidc.Create(req)
	if err != nil {
		return sendErrorEnvelope(r, err)
	}

	// Reload the auth manager to update the OIDC providers.
	if err := reloadAuth(app); err != nil {
		app.lo.Error("error reloading auth", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, app.i18n.T("globals.messages.somethingWentWrong"), nil, envelope.GeneralError)
	}

	// Clear client secret before returning
	createdOIDC.ClearSecrets()

	return r.SendEnvelope(createdOIDC)
}

// handleUpdateOIDC updates an OIDC record.
func handleUpdateOIDC(r *fastglue.Request) error {
	var (
		app = r.Context.(*App)
		req = models.OIDC{}
	)
	id, err := strconv.Atoi(r.RequestCtx.UserValue("id").(string))
	if err != nil || id == 0 {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, app.i18n.T("globals.messages.somethingWentWrong"), nil, envelope.InputError)
	}

	if err := r.Decode(&req, "json"); err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, app.i18n.T("errors.parsingRequest"), nil, envelope.GeneralError)
	}

	// Disabled providers can be shut off even when their discovery endpoint is
	// unavailable. Enabled configurations must pass discovery first.
	if req.Enabled {
		if err := app.auth.TestProvider(req.ProviderURL); err != nil {
			return sendErrorEnvelope(r, err)
		}
	}

	app.oidcAuthMu.Lock()
	defer app.oidcAuthMu.Unlock()

	// Stop new callbacks before updating the trust boundary. Any callback that
	// already held the read lease completes before this write lease is acquired
	// and is included in the revocation transaction below.
	app.auth.RemoveProvider(id)
	updatedOIDC, revokedUserIDs, err := app.oidc.UpdateAndRevoke(id, req)
	if err != nil {
		if reloadErr := reloadAuth(app); reloadErr != nil {
			app.lo.Error("error restoring auth providers after failed update", "error", reloadErr)
		}
		return sendErrorEnvelope(r, err)
	}
	for _, userID := range revokedUserIDs {
		app.user.InvalidateAgentCache(userID)
		app.wsHub.KickUser(userID)
	}

	// Reload the auth manager to update the OIDC providers.
	if err := reloadAuth(app); err != nil {
		app.lo.Error("error reloading auth", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, app.i18n.T("globals.messages.somethingWentWrong"), nil, envelope.GeneralError)
	}

	// Clear client secret before returning
	updatedOIDC.ClearSecrets()

	return r.SendEnvelope(updatedOIDC)
}

// handleDeleteOIDC deletes an OIDC record.
func handleDeleteOIDC(r *fastglue.Request) error {
	var app = r.Context.(*App)
	id, err := strconv.Atoi(r.RequestCtx.UserValue("id").(string))
	if err != nil || id == 0 {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, app.i18n.T("globals.messages.somethingWentWrong"), nil, envelope.InputError)
	}
	app.oidcAuthMu.Lock()
	defer app.oidcAuthMu.Unlock()

	// Stop new callbacks first. Any callback already holding a read lease has
	// completed session creation before the revocation transaction runs.
	app.auth.RemoveProvider(id)
	revokedUserIDs, err := app.oidc.Delete(id)
	if err != nil {
		if reloadErr := reloadAuth(app); reloadErr != nil {
			app.lo.Error("error restoring auth providers after failed deletion", "error", reloadErr)
		}
		return sendErrorEnvelope(r, err)
	}
	for _, userID := range revokedUserIDs {
		app.user.InvalidateAgentCache(userID)
		app.wsHub.KickUser(userID)
	}
	if err := reloadAuth(app); err != nil {
		app.lo.Error("error reloading auth after deleting provider", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, app.i18n.T("globals.messages.somethingWentWrong"), nil, envelope.GeneralError)
	}
	return r.SendEnvelope(true)
}
