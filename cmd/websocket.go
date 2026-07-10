package main

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	amodels "github.com/abhinavxd/libredesk/internal/auth/models"
	"github.com/abhinavxd/libredesk/internal/envelope"
	umodels "github.com/abhinavxd/libredesk/internal/user/models"
	"github.com/abhinavxd/libredesk/internal/ws"
	wsmodels "github.com/abhinavxd/libredesk/internal/ws/models"
	"github.com/fasthttp/websocket"
	"github.com/redis/go-redis/v9"
	"github.com/valyala/fasthttp"
	"github.com/zerodha/fastglue"
)

const (
	agentSessionCookieName         = "libredesk_session"
	agentSessionRedisPrefix        = "session:"
	agentSessionIDLength           = 64
	agentSessionRevalidateInterval = 15 * time.Second
	agentSessionValidationTimeout  = 5 * time.Second
)

type agentWebSocketUserStore interface {
	GetAgent(id int, email string) (umodels.User, error)
}

// ErrHandler is a custom error handler.
func ErrHandler(ctx *fasthttp.RequestCtx, status int, reason error) {
	fmt.Printf("error status %d: %s", status, reason)
}

// agentUpgrader: same-origin only, with exact loopback origins allowed for dev.
var agentUpgrader = websocket.FastHTTPUpgrader{
	ReadBufferSize:  8192,
	WriteBufferSize: 8192,
	CheckOrigin:     isSameOriginWebSocket,
	Error:           ErrHandler,
}

// Widget JavaScript runs inside the same-origin iframe. The embedding parent
// is validated separately by validateWidgetInbox before the upgrade.
var widgetUpgrader = websocket.FastHTTPUpgrader{
	ReadBufferSize:  8192,
	WriteBufferSize: 8192,
	CheckOrigin:     isSameOriginWebSocket,
	Error:           ErrHandler,
}

func isSameOriginWebSocket(ctx *fasthttp.RequestCtx) bool {
	origin := strings.TrimSpace(string(ctx.Request.Header.Peek("Origin")))
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	if !strings.EqualFold(u.Host, string(ctx.Request.Host())) {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	return u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1")
}

func validAgentSessionID(sessionID string) bool {
	if len(sessionID) != agentSessionIDLength {
		return false
	}
	for _, r := range sessionID {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func agentSessionExpiry(ctx context.Context, rd *redis.Client, sessionID string, now time.Time) (time.Time, error) {
	if rd == nil || !validAgentSessionID(sessionID) {
		return time.Time{}, fmt.Errorf("invalid agent session")
	}
	ttl, err := rd.PTTL(ctx, agentSessionRedisPrefix+sessionID).Result()
	if err != nil {
		return time.Time{}, fmt.Errorf("get agent session expiry: %w", err)
	}
	if ttl <= 0 {
		return time.Time{}, fmt.Errorf("agent session is expired")
	}
	return now.Add(ttl), nil
}

func validateAgentWebSocketSession(ctx context.Context, rd *redis.Client, users agentWebSocketUserStore, sessionID string, expected umodels.User) error {
	if rd == nil || users == nil || !validAgentSessionID(sessionID) || expected.ID <= 0 || expected.SessionVersion < 1 {
		return fmt.Errorf("invalid agent websocket session state")
	}

	values, err := rd.HMGet(ctx, agentSessionRedisPrefix+sessionID, "_ss", "id", "type", "session_version").Result()
	if err != nil {
		return fmt.Errorf("load agent websocket session: %w", err)
	}
	if len(values) != 4 || values[0] == nil || values[1] == nil || values[2] == nil || values[3] == nil {
		return fmt.Errorf("agent websocket session was revoked")
	}
	userID, err := strconv.Atoi(fmt.Sprint(values[1]))
	if err != nil {
		return fmt.Errorf("invalid agent websocket session user")
	}
	sessionVersion, err := strconv.ParseInt(fmt.Sprint(values[3]), 10, 64)
	if err != nil {
		return fmt.Errorf("invalid agent websocket session version")
	}
	if userID != expected.ID || fmt.Sprint(values[2]) != umodels.UserTypeAgent || sessionVersion != expected.SessionVersion {
		return fmt.Errorf("agent websocket session identity changed")
	}

	user, err := users.GetAgent(expected.ID, "")
	if err != nil {
		return fmt.Errorf("reload agent websocket user: %w", err)
	}
	if user.ID != expected.ID || user.Type != umodels.UserTypeAgent || !user.Enabled || user.SessionVersion != expected.SessionVersion {
		return fmt.Errorf("agent websocket user was revoked")
	}
	return nil
}

// handleWS handles the websocket connection.
func handleWS(r *fastglue.Request, hub *ws.Hub) error {
	var (
		auser = r.RequestCtx.UserValue("user").(amodels.User)
		app   = r.Context.(*App)
	)
	if authMethod, ok := r.RequestCtx.UserValue("auth_method").(string); !ok || authMethod != "session" {
		return r.SendErrorEnvelope(fasthttp.StatusUnauthorized, app.i18n.T("auth.invalidOrExpiredSession"), nil, envelope.GeneralError)
	}
	sessionUser, err := app.auth.ValidateSession(r)
	if err != nil || sessionUser.ID != auser.ID || sessionUser.Type != umodels.UserTypeAgent || sessionUser.SessionVersion < 1 {
		return r.SendErrorEnvelope(fasthttp.StatusUnauthorized, app.i18n.T("auth.invalidOrExpiredSession"), nil, envelope.GeneralError)
	}
	sessionID := strings.TrimSpace(string(r.RequestCtx.Request.Header.Cookie(agentSessionCookieName)))
	validationCtx, cancel := context.WithTimeout(r.RequestCtx, agentSessionValidationTimeout)
	sessionExpiresAt, err := agentSessionExpiry(validationCtx, app.redis, sessionID, time.Now())
	if err == nil {
		err = validateAgentWebSocketSession(validationCtx, app.redis, app.user, sessionID, sessionUser)
	}
	cancel()
	if err == nil && !time.Now().Before(sessionExpiresAt) {
		err = fmt.Errorf("agent session expired during websocket validation")
	}
	if err != nil {
		app.lo.Warn("rejecting websocket with invalid agent session", "user_id", auser.ID, "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusUnauthorized, app.i18n.T("auth.invalidOrExpiredSession"), nil, envelope.GeneralError)
	}

	clientIP := app.rateLimit.ClientIP(r.RequestCtx)
	releaseConnection, allowed := agentActiveConnections.acquire(strconv.Itoa(auser.ID), clientIP)
	if !allowed {
		return r.SendErrorEnvelope(fasthttp.StatusServiceUnavailable, app.i18n.T("globals.messages.somethingWentWrong"), nil, envelope.RateLimitError)
	}
	defer releaseConnection()
	err = agentUpgrader.Upgrade(r.RequestCtx, func(conn *websocket.Conn) {
		c := ws.Client{
			ID:                        auser.ID,
			Hub:                       hub,
			Conn:                      conn,
			Send:                      make(chan wsmodels.WSMessage, 128),
			SessionExpiresAt:          sessionExpiresAt,
			SessionValidationInterval: agentSessionRevalidateInterval,
			ValidateSession: func() bool {
				ctx := app.ctx
				if ctx == nil {
					ctx = context.Background()
				}
				ctx, cancel := context.WithTimeout(ctx, agentSessionValidationTimeout)
				defer cancel()
				if err := validateAgentWebSocketSession(ctx, app.redis, app.user, sessionID, sessionUser); err != nil {
					app.lo.Info("closing websocket after agent session revocation", "user_id", auser.ID, "error", err)
					return false
				}
				return true
			},
		}
		hub.AddClient(&c)
		go c.Listen()
		c.Serve()
	})
	if err != nil {
		app.lo.Error("error upgrading tcp connection", "user_id", auser.ID, "error", err)
	}
	return nil
}
