package main

import (
	"net/http"
	"slices"
	"strconv"
	"strings"

	amodels "github.com/abhinavxd/libredesk/internal/auth/models"
	"github.com/abhinavxd/libredesk/internal/envelope"
	"github.com/abhinavxd/libredesk/internal/image"
	"github.com/abhinavxd/libredesk/internal/user/models"
	realip "github.com/ferluci/fast-realip"
	"github.com/valyala/fasthttp"
	"github.com/zerodha/fastglue"
	"github.com/zerodha/simplesessions/v3"
)

func validateCSRFCookie(r *fastglue.Request, app *App) error {
	method := string(r.RequestCtx.Method())
	if method != "POST" && method != "PUT" && method != "DELETE" {
		return nil
	}

	cookieToken := string(r.RequestCtx.Request.Header.Cookie("csrf_token"))
	hdrToken := string(r.RequestCtx.Request.Header.Peek("X-CSRFTOKEN"))
	if cookieToken == "" || hdrToken == "" || cookieToken != hdrToken {
		app.lo.Warn(
			"csrf token mismatch",
			"method", method,
			"path", string(r.RequestCtx.Path()),
			"ip", realip.FromRequest(r.RequestCtx),
			"user_agent", string(r.RequestCtx.UserAgent()),
			"has_cookie_token", cookieToken != "",
			"has_header_token", hdrToken != "",
			"cookie_token_hash", shortHash(cookieToken),
			"header_token_hash", shortHash(hdrToken),
		)
		return envelope.NewError(envelope.PermissionError, app.i18n.T("auth.csrfTokenMismatch"), nil)
	}

	return nil
}

func shortHash(value string) string {
	hash := sha256Hex(value)
	if len(hash) <= 12 {
		return hash
	}
	return hash[:12]
}

func sessionTypeAllowed(sessUser models.User, allowedTypes []string) bool {
	return sessUser.ID > 0 && slices.Contains(allowedTypes, sessUser.Type)
}

// authenticateAgentUser handles both API key and agent session authentication.
func authenticateAgentUser(r *fastglue.Request, app *App) (models.User, error) {
	var user models.User

	apiKey, apiSecret, err := r.ParseAuthHeader(fastglue.AuthBasic | fastglue.AuthToken)
	if err == nil && len(apiKey) > 0 && len(apiSecret) > 0 {
		user, err = app.user.ValidateAPIKey(string(apiKey), string(apiSecret))
		if err != nil {
			return user, err
		}
		r.RequestCtx.SetUserValue("auth_method", "api_key")
		return user, nil
	}

	if err := validateCSRFCookie(r, app); err != nil {
		return user, err
	}

	sessUser, err := app.auth.ValidateSession(r)
	if err != nil || !sessionTypeAllowed(sessUser, []string{models.UserTypeAgent}) {
		app.lo.Error("error validating agent session", "error", err)
		return user, envelope.NewError(envelope.GeneralError, app.i18n.T("auth.invalidOrExpiredSession"), nil)
	}

	user, err = app.user.GetAgentCachedOrLoad(sessUser.ID)
	if err != nil {
		return user, err
	}

	if !user.Enabled {
		if err := app.auth.DestroySession(r); err != nil {
			app.lo.Error("error destroying session", "error", err)
		}
		return user, envelope.NewError(envelope.PermissionError, app.i18n.T("user.accountDisabled"), nil)
	}

	r.RequestCtx.SetUserValue("auth_method", "session")
	return user, nil
}

// authenticateCustomerUser validates the customer portal session for contacts.
func authenticateCustomerUser(r *fastglue.Request, app *App) (models.User, error) {
	var user models.User

	if err := validateCSRFCookie(r, app); err != nil {
		return user, err
	}

	sessUser, err := app.customerAuth.ValidateSession(r)
	if err != nil || !sessionTypeAllowed(sessUser, []string{models.UserTypeContact}) {
		app.lo.Error("error validating customer session", "error", err)
		return user, envelope.NewError(envelope.GeneralError, app.i18n.T("auth.invalidOrExpiredSession"), nil)
	}

	user, err = app.user.Get(sessUser.ID, "", []string{models.UserTypeContact})
	if err != nil {
		return user, err
	}

	if !user.Enabled {
		if err := app.customerAuth.DestroySession(r); err != nil {
			app.lo.Error("error destroying customer session", "error", err)
		}
		return user, envelope.NewError(envelope.PermissionError, app.i18n.T("user.accountDisabled"), nil)
	}

	r.RequestCtx.SetUserValue("auth_method", "customer_session")
	return user, nil
}

// tryAuth attempts to authenticate an agent but doesn't enforce it.
func tryAuth(handler fastglue.FastRequestHandler) fastglue.FastRequestHandler {
	return func(r *fastglue.Request) error {
		app := r.Context.(*App)

		user, err := authenticateAgentUser(r, app)
		if err != nil {
			return handler(r)
		}

		r.RequestCtx.SetUserValue("user", amodels.User{
			ID:        user.ID,
			Email:     user.Email.String,
			FirstName: user.FirstName,
			LastName:  user.LastName,
			Type:      user.Type,
		})

		return handler(r)
	}
}

// tryCustomerAuth attempts to authenticate a customer contact but doesn't enforce it.
func tryCustomerAuth(handler fastglue.FastRequestHandler) fastglue.FastRequestHandler {
	return func(r *fastglue.Request) error {
		app := r.Context.(*App)

		user, err := authenticateCustomerUser(r, app)
		if err != nil {
			return handler(r)
		}

		r.RequestCtx.SetUserValue("user", amodels.User{
			ID:        user.ID,
			Email:     user.Email.String,
			FirstName: user.FirstName,
			LastName:  user.LastName,
			Type:      user.Type,
		})

		return handler(r)
	}
}

// auth validates agent session/API key and adds the user to the request context.
func auth(handler fastglue.FastRequestHandler) fastglue.FastRequestHandler {
	return func(r *fastglue.Request) error {
		app := r.Context.(*App)

		user, err := authenticateAgentUser(r, app)
		if err != nil {
			if envErr, ok := err.(envelope.Error); ok {
				if envErr.ErrorType == envelope.PermissionError {
					return r.SendErrorEnvelope(http.StatusForbidden, envErr.Message, nil, envelope.PermissionError)
				}
				return r.SendErrorEnvelope(http.StatusUnauthorized, envErr.Message, nil, envelope.GeneralError)
			}
			return sendErrorEnvelope(r, err)
		}

		r.RequestCtx.SetUserValue("user", amodels.User{
			ID:        user.ID,
			Email:     user.Email.String,
			FirstName: user.FirstName,
			LastName:  user.LastName,
			Type:      user.Type,
		})

		return handler(r)
	}
}

// customerAuth validates customer session and adds the contact to the request context.
func customerAuth(handler fastglue.FastRequestHandler) fastglue.FastRequestHandler {
	return func(r *fastglue.Request) error {
		app := r.Context.(*App)

		user, err := authenticateCustomerUser(r, app)
		if err != nil {
			if envErr, ok := err.(envelope.Error); ok {
				if envErr.ErrorType == envelope.PermissionError {
					return r.SendErrorEnvelope(http.StatusForbidden, envErr.Message, nil, envelope.PermissionError)
				}
				return r.SendErrorEnvelope(http.StatusUnauthorized, envErr.Message, nil, envelope.GeneralError)
			}
			return sendErrorEnvelope(r, err)
		}

		r.RequestCtx.SetUserValue("user", amodels.User{
			ID:        user.ID,
			Email:     user.Email.String,
			FirstName: user.FirstName,
			LastName:  user.LastName,
			Type:      user.Type,
		})

		return handler(r)
	}
}

// perm checks if the agent has the required permission to access the endpoint.
func perm(handler fastglue.FastRequestHandler, perm string) fastglue.FastRequestHandler {
	return func(r *fastglue.Request) error {
		app := r.Context.(*App)

		user, err := authenticateAgentUser(r, app)
		if err != nil {
			if envErr, ok := err.(envelope.Error); ok {
				if envErr.ErrorType == envelope.PermissionError {
					return r.SendErrorEnvelope(http.StatusForbidden, envErr.Message, nil, envelope.PermissionError)
				}
				return r.SendErrorEnvelope(http.StatusUnauthorized, envErr.Message, nil, envelope.GeneralError)
			}
			return sendErrorEnvelope(r, err)
		}

		parts := strings.Split(perm, ":")
		if len(parts) != 2 {
			return r.SendErrorEnvelope(http.StatusInternalServerError, app.i18n.T("validation.invalidPermission"), nil, envelope.GeneralError)
		}
		object, action := parts[0], parts[1]
		ok, err := app.authz.Enforce(user, object, action)
		if err != nil {
			app.lo.Error("error checking permission", "error", err)
			return r.SendErrorEnvelope(http.StatusInternalServerError, app.i18n.T("globals.messages.somethingWentWrong"), nil, envelope.GeneralError)
		}
		if !ok {
			return r.SendErrorEnvelope(http.StatusForbidden, app.i18n.T("status.deniedPermission"), nil, envelope.PermissionError)
		}

		r.RequestCtx.SetUserValue("user", amodels.User{
			ID:        user.ID,
			Email:     user.Email.String,
			FirstName: user.FirstName,
			LastName:  user.LastName,
			Type:      user.Type,
		})

		return handler(r)
	}
}

// authPage ensures the agent is logged in; otherwise, redirects to the login page.
func authPage(handler fastglue.FastRequestHandler) fastglue.FastRequestHandler {
	return func(r *fastglue.Request) error {
		app := r.Context.(*App)

		user, err := app.auth.ValidateSession(r)
		if err != nil {
			if err != simplesessions.ErrInvalidSession {
				app.lo.Error("error validating session", "error", err)
				return r.SendErrorEnvelope(http.StatusUnauthorized, app.i18n.T("globals.messages.somethingWentWrong"), nil, envelope.GeneralError)
			}
			if err := app.auth.DestroySession(r); err != nil {
				app.lo.Error("error destroying session", "error", err)
			}
		}

		if sessionTypeAllowed(user, []string{models.UserTypeAgent}) {
			return handler(r)
		}

		nextURI := r.RequestCtx.QueryArgs().Peek("next")
		if len(nextURI) == 0 {
			nextURI = r.RequestCtx.RequestURI()
		}
		return r.RedirectURI("/", fasthttp.StatusFound, map[string]any{
			"next": string(nextURI),
		}, "")
	}
}

// notAuthPage allows access only if the agent is not authenticated.
func notAuthPage(handler fastglue.FastRequestHandler) fastglue.FastRequestHandler {
	return func(r *fastglue.Request) error {
		app := r.Context.(*App)

		user, err := app.auth.ValidateSession(r)
		if err != nil && err != simplesessions.ErrInvalidSession {
			app.lo.Error("error validating session", "error", err)
			return r.SendErrorEnvelope(http.StatusUnauthorized, app.i18n.T("auth.invalidOrExpiredSessionClearCookie"), nil, envelope.GeneralError)
		}

		if sessionTypeAllowed(user, []string{models.UserTypeAgent}) {
			nextURI := string(r.RequestCtx.QueryArgs().Peek("next"))
			if nextURI == "" {
				nextURI = "/inboxes/assigned"
			}
			return r.RedirectURI(nextURI, fasthttp.StatusFound, nil, "")
		}
		return handler(r)
	}
}

// customerAuthPage ensures a customer is logged in; otherwise, redirects to portal login.
func customerAuthPage(handler fastglue.FastRequestHandler) fastglue.FastRequestHandler {
	return func(r *fastglue.Request) error {
		app := r.Context.(*App)

		user, err := app.customerAuth.ValidateSession(r)
		if err != nil && err != simplesessions.ErrInvalidSession {
			app.lo.Error("error validating customer session", "error", err)
		}

		if sessionTypeAllowed(user, []string{models.UserTypeContact}) {
			return handler(r)
		}

		if err == simplesessions.ErrInvalidSession {
			if destroyErr := app.customerAuth.DestroySession(r); destroyErr != nil {
				app.lo.Error("error destroying invalid customer session", "error", destroyErr)
			}
		}

		return r.RedirectURI("/portal/login", fasthttp.StatusFound, map[string]any{
			"next": string(r.RequestCtx.RequestURI()),
		}, "")
	}
}

// customerNotAuthPage allows access only if no customer is authenticated.
func customerNotAuthPage(handler fastglue.FastRequestHandler) fastglue.FastRequestHandler {
	return func(r *fastglue.Request) error {
		app := r.Context.(*App)

		user, err := app.customerAuth.ValidateSession(r)
		if err != nil && err != simplesessions.ErrInvalidSession {
			app.lo.Error("error validating customer session", "error", err)
		}

		if sessionTypeAllowed(user, []string{models.UserTypeContact}) {
			nextURI := string(r.RequestCtx.QueryArgs().Peek("next"))
			if nextURI == "" {
				nextURI = "/portal/tickets"
			}
			return r.RedirectURI(nextURI, fasthttp.StatusFound, nil, "")
		}

		return handler(r)
	}
}

// publicTicketPage conditionally protects /submit-ticket behind customer login.
func publicTicketPage(handler fastglue.FastRequestHandler) fastglue.FastRequestHandler {
	return func(r *fastglue.Request) error {
		if !publicTicketRequireLogin() {
			return handler(r)
		}

		app := r.Context.(*App)

		user, err := app.customerAuth.ValidateSession(r)
		if err != nil && err != simplesessions.ErrInvalidSession {
			app.lo.Error("error validating session for public ticket page", "error", err)
		}

		if sessionTypeAllowed(user, []string{models.UserTypeContact}) {
			return handler(r)
		}

		if err == simplesessions.ErrInvalidSession {
			if destroyErr := app.customerAuth.DestroySession(r); destroyErr != nil {
				app.lo.Error("error destroying invalid session", "error", destroyErr)
			}
		}

		return r.RedirectURI("/portal/login", fasthttp.StatusFound, map[string]any{
			"next": "/portal/tickets/new",
		}, "")
	}
}

// rateLimit applies rate limiting for the given rule name.
func rateLimit(handler fastglue.FastRequestHandler, ruleName string) fastglue.FastRequestHandler {
	return func(r *fastglue.Request) error {
		app := r.Context.(*App)
		if err := app.rateLimit.Check(r.RequestCtx, ruleName); err != nil {
			return err
		}
		return handler(r)
	}
}

// authOrSignedURL allows access if the agent is authenticated OR if URL has valid signature.
func authOrSignedURL(handler fastglue.FastRequestHandler) fastglue.FastRequestHandler {
	return func(r *fastglue.Request) error {
		app := r.Context.(*App)

		user, err := authenticateAgentUser(r, app)
		if err == nil && user.ID > 0 {
			r.RequestCtx.SetUserValue("user", amodels.User{
				ID:        user.ID,
				Email:     user.Email.String,
				FirstName: user.FirstName,
				LastName:  user.LastName,
				Type:      user.Type,
			})
			r.RequestCtx.SetUserValue("auth_method", "session")
			return handler(r)
		}

		validator := app.media.SignedURLValidator()
		if validator == nil {
			return r.SendErrorEnvelope(http.StatusUnauthorized,
				app.i18n.T("auth.invalidOrExpiredSession"), nil, envelope.GeneralError)
		}

		sig := string(r.RequestCtx.QueryArgs().Peek("sig"))
		expStr := string(r.RequestCtx.QueryArgs().Peek("exp"))

		if sig == "" || expStr == "" {
			return r.SendErrorEnvelope(http.StatusUnauthorized,
				app.i18n.T("auth.invalidOrExpiredSession"), nil, envelope.GeneralError)
		}

		exp, err := strconv.ParseInt(expStr, 10, 64)
		if err != nil {
			return r.SendErrorEnvelope(http.StatusBadRequest,
				app.i18n.T("globals.messages.somethingWentWrong"), nil, envelope.InputError)
		}

		uuid := r.RequestCtx.UserValue("uuid").(string)
		signatureUUID := strings.TrimPrefix(uuid, image.ThumbPrefix)

		if !validator(signatureUUID, sig, exp) {
			return r.SendErrorEnvelope(http.StatusForbidden,
				app.i18n.T("media.invalidOrExpiredURL"), nil, envelope.PermissionError)
		}

		r.RequestCtx.SetUserValue("auth_method", "signed_url")
		return handler(r)
	}
}
