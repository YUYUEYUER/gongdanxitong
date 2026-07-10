package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/abhinavxd/libredesk/internal/envelope"
	"github.com/abhinavxd/libredesk/internal/httputil"
	"github.com/abhinavxd/libredesk/internal/inbox/channel/livechat"
	imodels "github.com/abhinavxd/libredesk/internal/inbox/models"
	umodels "github.com/abhinavxd/libredesk/internal/user/models"
	"github.com/fasthttp/websocket"
	"github.com/valyala/fasthttp"
	"github.com/zerodha/fastglue"
)

const (
	WidgetMsgTypeJoin      = "join"
	WidgetMsgTypeTyping    = "typing"
	WidgetMsgTypePing      = "ping"
	WidgetMsgTypePong      = "pong"
	WidgetMsgTypeError     = "error"
	WidgetMsgTypeJoined    = "joined"
	WidgetMsgTypePageVisit = "page_visit"

	pageVisitRedisKeyPrefix = "page_visits:"
	maxPageVisits           = 20
	pageVisitTTL            = 24 * time.Hour
	wsReadDeadline          = 20 * time.Second
	wsJoinDeadline          = 10 * time.Second
	wsReadLimitBytes        = 64 * 1024
	wsMaxFramesPerMinute    = 120

	// Per-connection minimum intervals between inbound frames of each kind.
	// The HTTP upgrade is rate-limited, but inbound frames aren't, so a single
	// connection can otherwise drive unbounded DB/Redis work and agent fan-out.
	// Values are chosen to be just loose enough that no legitimate frontend
	// cadence is ever throttled.
	wsMinIntervalTyping    = 50 * time.Millisecond
	wsMinIntervalPageVisit = 1 * time.Second
	wsMinIntervalPing      = 1 * time.Second
)

type WidgetMessage struct {
	Type  string          `json:"type"`
	Token string          `json:"token,omitempty"`
	Data  json.RawMessage `json:"data"`
}

type WidgetInboxJoinRequest struct {
	InboxID string `json:"inbox_id"`
}

type WidgetTypingData struct {
	ConversationUUID string `json:"conversation_uuid"`
	IsTyping         bool   `json:"is_typing"`
}

type WidgetPageVisitData struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

// safeConn wraps a WebSocket connection with a mutex for concurrent-safe writes
// and a per-connection rate tracker for inbound frames.
type safeConn struct {
	conn *websocket.Conn
	mu   sync.Mutex

	rateMu           sync.Mutex
	lastAt           map[string]time.Time
	frameWindowStart time.Time
	frameCount       int
}

func (sc *safeConn) allowFrame() bool {
	sc.rateMu.Lock()
	defer sc.rateMu.Unlock()
	now := time.Now()
	if sc.frameWindowStart.IsZero() || now.Sub(sc.frameWindowStart) >= time.Minute {
		sc.frameWindowStart = now
		sc.frameCount = 0
	}
	if sc.frameCount >= wsMaxFramesPerMinute {
		return false
	}
	sc.frameCount++
	return true
}

func (sc *safeConn) WriteJSON(v any) error {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.conn.WriteJSON(v)
}

func (sc *safeConn) WriteMessage(msgType int, data []byte) error {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.conn.WriteMessage(msgType, data)
}

// allow throttles abusive clients that flood typing/page_visit/ping frames.
func (sc *safeConn) allow(kind string, minInterval time.Duration) bool {
	sc.rateMu.Lock()
	defer sc.rateMu.Unlock()
	if sc.lastAt == nil {
		sc.lastAt = make(map[string]time.Time)
	}
	now := time.Now()
	if last, ok := sc.lastAt[kind]; ok && now.Sub(last) < minInterval {
		return false
	}
	sc.lastAt[kind] = now
	return true
}

func isAllowedBeforeWidgetJoin(messageType string) bool {
	return messageType == WidgetMsgTypeJoin
}

func handleWidgetWS(r *fastglue.Request) error {
	var app = r.Context.(*App)
	validatedInbox, err := getWidgetInbox(r)
	if err != nil {
		return err
	}
	parentOrigin, err := getWidgetParentOrigin(r)
	if err != nil {
		return err
	}

	clientIP := app.rateLimit.ClientIP(r.RequestCtx)
	requestHost := string(r.RequestCtx.Host())
	releaseConnection, allowed := widgetActiveConnections.acquire(validatedInbox.UUID, clientIP)
	if !allowed {
		return r.SendErrorEnvelope(fasthttp.StatusServiceUnavailable, app.i18n.T("globals.messages.somethingWentWrong"), nil, envelope.RateLimitError)
	}
	defer releaseConnection()

	if err := widgetUpgrader.Upgrade(r.RequestCtx, func(conn *websocket.Conn) {
		conn.SetReadLimit(wsReadLimitBytes)
		sc := &safeConn{conn: conn}

		var (
			client                *livechat.Client
			liveChat              *livechat.LiveChat
			inboxUUID             string
			userID                int
			sessionToken          string
			joinAttempted         bool
			lastSessionValidation time.Time
		)

		defer func() {
			conn.Close()
			if client != nil && liveChat != nil {
				liveChat.RemoveClient(client)
				client.CloseChannel()
			}
		}()

		joinBy := time.Now().Add(wsJoinDeadline)
	connectionLoop:
		for {
			if userID == 0 {
				conn.SetReadDeadline(joinBy)
			} else {
				conn.SetReadDeadline(time.Now().Add(wsReadDeadline))
			}
			var msg WidgetMessage
			if err := conn.ReadJSON(&msg); err != nil {
				app.lo.Debug("widget websocket connection closed", "error", err)
				break
			}
			if !sc.allowFrame() {
				sendWidgetError(sc, "Message rate limit exceeded")
				break
			}
			if userID == 0 && !isAllowedBeforeWidgetJoin(msg.Type) {
				sendWidgetError(sc, "Authentication required")
				break
			}
			if userID > 0 && time.Since(lastSessionValidation) >= 15*time.Second {
				if err := revalidateWidgetWebSocketSession(app, sessionToken, userID, validatedInbox.UUID, parentOrigin, requestHost, clientIP); err != nil {
					app.lo.Info("closing widget websocket after session revocation", "user_id", userID)
					break
				}
				lastSessionValidation = time.Now()
			}

			switch msg.Type {
			case WidgetMsgTypeJoin:
				if joinAttempted {
					sendWidgetError(sc, "Inbox already joined")
					break connectionLoop
				}
				joinAttempted = true

				joinedClient, joinedLiveChat, joinedInboxUUID, joinedUserID, err := handleInboxJoin(app, sc, msg.Data, msg.Token, clientIP, validatedInbox, parentOrigin)
				if err != nil {
					app.lo.Error("error handling widget join", "error", err)
					sendWidgetError(sc, "Failed to join conversation")
					break connectionLoop
				}
				client = joinedClient
				liveChat = joinedLiveChat
				inboxUUID = joinedInboxUUID
				userID = joinedUserID
				sessionToken = msg.Token
				lastSessionValidation = time.Now()

			case WidgetMsgTypeTyping:
				if userID == 0 || inboxUUID == "" {
					continue
				}
				if !sc.allow(WidgetMsgTypeTyping, wsMinIntervalTyping) {
					continue
				}
				handleWidgetTyping(app, msg.Data, userID)

			case WidgetMsgTypePageVisit:
				if userID > 0 && sc.allow(WidgetMsgTypePageVisit, wsMinIntervalPageVisit) {
					handleWidgetPageVisit(app, msg.Data, userID)
				}

			case WidgetMsgTypePing:
				if !sc.allow(WidgetMsgTypePing, wsMinIntervalPing) {
					continue
				}
				if userID > 0 {
					wasOffline, err := app.user.UpdateLastActive(userID)
					if err != nil {
						app.lo.Error("error updating user last active timestamp", "user_id", userID, "error", err)
					} else if wasOffline {
						app.conversation.BroadcastContactUpdate(userID, map[string]any{"availability_status": "online"})
					}
				}

				if err := sc.WriteJSON(WidgetMessage{Type: WidgetMsgTypePong}); err != nil {
					app.lo.Error("error writing pong to widget client", "error", err)
				}
			}
		}
	}); err != nil {
		app.lo.Error("error upgrading widget websocket connection", "error", err)
	}
	return nil
}

func revalidateWidgetWebSocketSession(app *App, token string, expectedUserID int, inboxUUID, parentOrigin, requestHost, clientIP string) error {
	inbox, err := app.inbox.GetDBRecord(inboxUUID)
	if err != nil || !inbox.Enabled || inbox.Channel != livechat.ChannelLiveChat {
		return fmt.Errorf("widget inbox is no longer active")
	}
	var config livechat.Config
	if err := json.Unmarshal(inbox.Config, &config); err != nil {
		return fmt.Errorf("widget inbox configuration is invalid")
	}
	trustedDomains := config.TrustedDomains
	if len(trustedDomains) == 0 {
		trustedDomains = []string{requestHost}
	}
	if !httputil.IsOriginTrusted(parentOrigin, trustedDomains) {
		return fmt.Errorf("widget parent origin is no longer trusted")
	}
	if len(config.BlockedIPs) > 0 && httputil.IsIPBlocked(clientIP, config.BlockedIPs) {
		return fmt.Errorf("widget client IP is blocked")
	}

	session, err := loadSession(app, token, inbox, config, parentOrigin)
	if err != nil || session.UserID != expectedUserID {
		return fmt.Errorf("widget session is no longer active")
	}
	user, err := app.user.Get(session.UserID, "", []string{umodels.UserTypeContact, umodels.UserTypeVisitor})
	if err != nil || !user.Enabled || user.SessionVersion != session.UserSessionVersion {
		return fmt.Errorf("widget user session is no longer active")
	}
	return nil
}

func handleInboxJoin(app *App, sc *safeConn, data json.RawMessage, token, clientIP string, inbox imodels.Inbox, parentOrigin string) (*livechat.Client, *livechat.LiveChat, string, int, error) {
	var joinData WidgetInboxJoinRequest
	if err := json.Unmarshal(data, &joinData); err != nil {
		return nil, nil, "", 0, fmt.Errorf("invalid join data: %w", err)
	}

	if joinData.InboxID != inbox.UUID {
		return nil, nil, "", 0, fmt.Errorf("inbox does not match validated widget origin")
	}
	if !inbox.Enabled {
		return nil, nil, "", 0, fmt.Errorf("inbox is not enabled")
	}

	var config livechat.Config
	if err := json.Unmarshal(inbox.Config, &config); err == nil {
		if len(config.BlockedIPs) > 0 && httputil.IsIPBlocked(clientIP, config.BlockedIPs) {
			return nil, nil, "", 0, fmt.Errorf("IP address is blocked")
		}
	}

	session, err := loadSession(app, token, inbox, config, parentOrigin)
	if err != nil {
		return nil, nil, "", 0, fmt.Errorf("session token validation failed: %w", err)
	}
	if session.InboxID != inbox.ID {
		return nil, nil, "", 0, fmt.Errorf("session does not belong to this inbox")
	}

	// Verify user exists and is enabled.
	user, err := app.user.Get(session.UserID, "", []string{umodels.UserTypeContact, umodels.UserTypeVisitor})
	if err != nil || !user.Enabled || user.SessionVersion != session.UserSessionVersion {
		return nil, nil, "", 0, fmt.Errorf("user not found or disabled")
	}

	lcInbox, err := app.inbox.Get(inbox.ID)
	if err != nil {
		return nil, nil, "", 0, fmt.Errorf("live chat inbox not found: %w", err)
	}

	liveChat, ok := lcInbox.(*livechat.LiveChat)
	if !ok {
		return nil, nil, "", 0, fmt.Errorf("inbox is not a live chat inbox")
	}

	userIDStr := fmt.Sprintf("%d", user.ID)
	client, err := liveChat.AddClient(userIDStr)
	if err != nil {
		return nil, nil, "", 0, fmt.Errorf("adding client to live chat: %w", err)
	}
	joinSucceeded := false
	defer func() {
		if !joinSucceeded {
			liveChat.RemoveClient(client)
			client.CloseChannel()
		}
	}()

	if err := sc.WriteJSON(WidgetMessage{
		Type: WidgetMsgTypeJoined,
		Data: json.RawMessage(`{"message":"namaste!"}`),
	}); err != nil {
		return nil, nil, "", 0, err
	}

	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				app.lo.Error("panic in widget ws forwarder", "panic", rec)
			}
		}()
		for msgData := range client.Channel {
			if err := sc.WriteMessage(websocket.TextMessage, msgData); err != nil {
				app.lo.Error("error forwarding message to widget client", "error", err)
				return
			}
		}
	}()

	app.lo.Debug("widget client joined live chat", "user_id", userIDStr, "inbox_uuid", joinData.InboxID)

	joinSucceeded = true
	return client, liveChat, joinData.InboxID, user.ID, nil
}

func handleWidgetTyping(app *App, data json.RawMessage, userID int) {
	var typingData WidgetTypingData
	if err := json.Unmarshal(data, &typingData); err != nil || typingData.ConversationUUID == "" {
		return
	}

	// userID was already validated during WS join.
	conversation, err := app.conversation.GetConversation(0, typingData.ConversationUUID, "")
	if err != nil || conversation.ContactID != userID {
		return
	}

	app.conversation.BroadcastTypingToConversation(typingData.ConversationUUID, typingData.IsTyping, false)
}

func sendWidgetError(sc *safeConn, message string) {
	data, _ := json.Marshal(map[string]string{"message": message})
	sc.WriteJSON(WidgetMessage{
		Type: WidgetMsgTypeError,
		Data: data,
	})
}

func handleWidgetPageVisit(app *App, data json.RawMessage, contactID int) {
	var visit WidgetPageVisitData
	if err := json.Unmarshal(data, &visit); err != nil || visit.URL == "" {
		return
	}
	if !sanitizeWidgetPageVisit(&visit) {
		return
	}

	redisCtx := context.Background()
	key := fmt.Sprintf("%s%d", pageVisitRedisKeyPrefix, contactID)

	// Skip if the most recent page visit has the same URL.
	if latest, err := app.redis.LIndex(redisCtx, key, 0).Result(); err == nil {
		var lastVisit map[string]string
		if json.Unmarshal([]byte(latest), &lastVisit) == nil && lastVisit["url"] == visit.URL {
			return
		}
	}

	entry, _ := json.Marshal(map[string]string{
		"url":   visit.URL,
		"title": visit.Title,
		"time":  time.Now().UTC().Format(time.RFC3339),
	})

	pipe := app.redis.Pipeline()
	pipe.LPush(redisCtx, key, string(entry))
	pipe.LTrim(redisCtx, key, 0, maxPageVisits-1)
	pipe.Expire(redisCtx, key, pageVisitTTL)
	lrangeCmd := pipe.LRange(redisCtx, key, 0, maxPageVisits-1)
	pipe.Exec(redisCtx)

	entries, err := lrangeCmd.Result()
	if err != nil {
		return
	}
	pages := make([]map[string]string, 0, len(entries))
	for _, e := range entries {
		var p map[string]string
		if err := json.Unmarshal([]byte(e), &p); err == nil {
			pages = append(pages, p)
		}
	}
	app.conversation.BroadcastContactUpdate(contactID, map[string]any{"page_visits": pages})
}

func sanitizeWidgetPageVisit(visit *WidgetPageVisitData) bool {
	if visit == nil || visit.URL == "" || len(visit.URL) > 2048 {
		return false
	}
	parsedURL, err := url.Parse(visit.URL)
	if err != nil || parsedURL.Hostname() == "" || parsedURL.User != nil ||
		(parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
		return false
	}
	parsedURL.Path = "/"
	parsedURL.RawPath = ""
	parsedURL.RawQuery = ""
	parsedURL.ForceQuery = false
	parsedURL.Fragment = ""
	visit.URL = parsedURL.String()
	visit.Title = ""
	return true
}

func getPageVisitsFromRedis(app *App, contactID int) []map[string]string {
	redisCtx := context.Background()
	key := fmt.Sprintf("%s%d", pageVisitRedisKeyPrefix, contactID)
	entries, err := app.redis.LRange(redisCtx, key, 0, maxPageVisits-1).Result()
	if err != nil {
		return nil
	}
	pages := make([]map[string]string, 0, len(entries))
	for _, e := range entries {
		var p map[string]string
		if err := json.Unmarshal([]byte(e), &p); err == nil {
			pages = append(pages, p)
		}
	}
	return pages
}
