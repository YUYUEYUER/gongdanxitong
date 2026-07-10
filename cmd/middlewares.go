package main

import (
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	amodels "github.com/abhinavxd/libredesk/internal/auth/models"
	"github.com/abhinavxd/libredesk/internal/envelope"
	"github.com/abhinavxd/libredesk/internal/image"
	"github.com/abhinavxd/libredesk/internal/user/models"
	"github.com/valyala/fasthttp"
	"github.com/zerodha/fastglue"
)

const maxPublicJSONBodyBytes = 128 * 1024

func requestBodyLimit(handler fastglue.FastRequestHandler, maxBytes int) fastglue.FastRequestHandler {
	return func(r *fastglue.Request) error {
		if maxBytes <= 0 || len(r.RequestCtx.PostBody()) > maxBytes {
			app := r.Context.(*App)
			return r.SendErrorEnvelope(fasthttp.StatusRequestEntityTooLarge, app.i18n.T("globals.messages.badRequest"), nil, envelope.InputError)
		}
		return handler(r)
	}
}

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
			"ip", requestClientIP(app, r.RequestCtx),
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

func requestClientIP(app *App, ctx *fasthttp.RequestCtx) string {
	if app.rateLimit != nil {
		return app.rateLimit.ClientIP(ctx)
	}
	return ctx.RemoteIP().String()
}

func safeNextPath(raw, fallback string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsAny(raw, "\\\r\n") || strings.HasPrefix(raw, "//") {
		return fallback
	}
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.User != nil || !strings.HasPrefix(parsed.Path, "/") {
		return fallback
	}
	decodedPath, err := url.PathUnescape(parsed.EscapedPath())
	if err != nil || strings.HasPrefix(decodedPath, "//") || strings.Contains(decodedPath, "\\") {
		return fallback
	}
	return parsed.RequestURI()
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

func validateAgentSessionUser(r *fastglue.Request, app *App) (models.User, error) {
	sessUser, err := app.auth.ValidateSession(r)
	if err != nil || !sessionTypeAllowed(sessUser, []string{models.UserTypeAgent}) {
		return models.User{}, envelope.NewError(envelope.GeneralError, app.i18n.T("auth.invalidOrExpiredSession"), nil)
	}

	user, err := app.user.GetAgentCachedOrLoad(sessUser.ID)
	if err != nil {
		return user, err
	}
	if sessUser.SessionVersion != user.SessionVersion {
		if destroyErr := app.auth.DestroySession(r); destroyErr != nil {
			app.lo.Error("error destroying revoked agent session", "error", destroyErr)
		}
		return models.User{}, envelope.NewError(envelope.GeneralError, app.i18n.T("auth.invalidOrExpiredSession"), nil)
	}
	if !user.Enabled {
		if destroyErr := app.auth.DestroySession(r); destroyErr != nil {
			app.lo.Error("error destroying disabled agent session", "error", destroyErr)
		}
		return user, envelope.NewError(envelope.PermissionError, app.i18n.T("user.accountDisabled"), nil)
	}
	return user, nil
}

func validateCustomerSessionUser(r *fastglue.Request, app *App) (models.User, error) {
	sessUser, err := app.customerAuth.ValidateSession(r)
	if err != nil || !sessionTypeAllowed(sessUser, []string{models.UserTypeContact}) {
		return models.User{}, envelope.NewError(envelope.GeneralError, app.i18n.T("auth.invalidOrExpiredSession"), nil)
	}

	user, err := app.user.Get(sessUser.ID, "", []string{models.UserTypeContact})
	if err != nil {
		return user, err
	}
	if sessUser.SessionVersion != user.SessionVersion {
		if destroyErr := app.customerAuth.DestroySession(r); destroyErr != nil {
			app.lo.Error("error destroying revoked customer session", "error", destroyErr)
		}
		return models.User{}, envelope.NewError(envelope.GeneralError, app.i18n.T("auth.invalidOrExpiredSession"), nil)
	}
	if !user.Enabled {
		if destroyErr := app.customerAuth.DestroySession(r); destroyErr != nil {
			app.lo.Error("error destroying disabled customer session", "error", destroyErr)
		}
		return user, envelope.NewError(envelope.PermissionError, app.i18n.T("user.accountDisabled"), nil)
	}
	return user, nil
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

	user, err = validateAgentSessionUser(r, app)
	if err != nil {
		app.lo.Error("error validating agent session", "error", err)
		return user, envelope.NewError(envelope.GeneralError, app.i18n.T("auth.invalidOrExpiredSession"), nil)
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

	user, err := validateCustomerSessionUser(r, app)
	if err != nil {
		app.lo.Error("error validating customer session", "error", err)
		return user, envelope.NewError(envelope.GeneralError, app.i18n.T("auth.invalidOrExpiredSession"), nil)
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

		if _, err := validateAgentSessionUser(r, app); err == nil {
			return handler(r)
		}

		nextURI := safeNextPath(string(r.RequestCtx.QueryArgs().Peek("next")), string(r.RequestCtx.Path()))
		return r.RedirectURI("/", fasthttp.StatusFound, map[string]any{
			"next": nextURI,
		}, "")
	}
}

// notAuthPage allows access only if the agent is not authenticated.
func notAuthPage(handler fastglue.FastRequestHandler) fastglue.FastRequestHandler {
	return func(r *fastglue.Request) error {
		app := r.Context.(*App)

		if _, err := validateAgentSessionUser(r, app); err == nil {
			nextURI := safeNextPath(string(r.RequestCtx.QueryArgs().Peek("next")), "/inboxes/assigned")
			return r.RedirectURI(nextURI, fasthttp.StatusFound, nil, "")
		}
		return handler(r)
	}
}

// customerAuthPage ensures a customer is logged in; otherwise, redirects to portal login.
func customerAuthPage(handler fastglue.FastRequestHandler) fastglue.FastRequestHandler {
	return func(r *fastglue.Request) error {
		app := r.Context.(*App)

		if _, err := validateCustomerSessionUser(r, app); err == nil {
			return handler(r)
		}

		return r.RedirectURI("/portal/login", fasthttp.StatusFound, map[string]any{
			"next": safeNextPath(string(r.RequestCtx.RequestURI()), "/portal/tickets"),
		}, "")
	}
}

// customerNotAuthPage allows access only if no customer is authenticated.
func customerNotAuthPage(handler fastglue.FastRequestHandler) fastglue.FastRequestHandler {
	return func(r *fastglue.Request) error {
		app := r.Context.(*App)

		if _, err := validateCustomerSessionUser(r, app); err == nil {
			nextURI := safeNextPath(string(r.RequestCtx.QueryArgs().Peek("next")), "/portal/tickets")
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

		if _, err := validateCustomerSessionUser(r, app); err == nil {
			return handler(r)
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
		if !validMediaSignature(validator, uuid, sig, exp) {
			return r.SendErrorEnvelope(http.StatusForbidden,
				app.i18n.T("media.invalidOrExpiredURL"), nil, envelope.PermissionError)
		}

		r.RequestCtx.SetUserValue("auth_method", "signed_url")
		return handler(r)
	}
}

func validMediaSignature(validator func(name, sig string, exp int64) bool, name, sig string, exp int64) bool {
	if validator == nil || name == "" {
		return false
	}
	if validator(name, sig, exp) {
		return true
	}
	// Compatibility with thumbnail links emitted before thumbnail names were
	// signed independently.
	return strings.HasPrefix(name, image.ThumbPrefix) && validator(strings.TrimPrefix(name, image.ThumbPrefix), sig, exp)
}
