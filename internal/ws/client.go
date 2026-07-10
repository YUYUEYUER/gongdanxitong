package ws

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/abhinavxd/libredesk/internal/ws/models"
	"github.com/fasthttp/websocket"
)

// SafeBool is a thread-safe boolean.
type SafeBool struct {
	flag bool
	mu   sync.RWMutex
}

// Set sets the value of the SafeBool.
func (b *SafeBool) Set(value bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.flag = value
}

// Get returns the value of the SafeBool.
func (b *SafeBool) Get() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.flag
}

// Client is a single connected WS user.
type Client struct {
	// Client ID.
	ID int

	// Hub.
	Hub *Hub

	// WebSocket connection.
	Conn *websocket.Conn

	// To prevent pushes to the channel.
	Closed SafeBool

	// Buffered channel of outbound ws messages.
	Send chan models.WSMessage

	// SessionExpiresAt is the absolute upper bound for this connection.
	SessionExpiresAt time.Time

	// ValidateSession rechecks mutable authentication state while connected.
	ValidateSession func() bool

	// SessionValidationInterval controls how often ValidateSession runs.
	SessionValidationInterval time.Duration

	closeOnce       sync.Once
	sendMu          sync.RWMutex
	rateMu          sync.Mutex
	rateWindowStart time.Time
	rateCount       int
}

const (
	maxInboundMessageBytes           = 64 << 10
	maxInboundMessages               = 120
	inboundRateWindow                = time.Minute
	pongWait                         = 60 * time.Second
	pingPeriod                       = 30 * time.Second
	writeWait                        = 10 * time.Second
	defaultSessionValidationInterval = 15 * time.Second
)

// Serve handles heartbeats and sending messages to the client.
func (c *Client) Serve() {
	if c.sessionExpired(time.Now()) {
		c.writeSessionClose("session expired")
		_ = c.Conn.Close()
		return
	}

	heartBeatTicker := time.NewTicker(pingPeriod)
	defer heartBeatTicker.Stop()
	defer c.Conn.Close()

	var (
		expiryTimer      *time.Timer
		expiry           <-chan time.Time
		validationTicker *time.Ticker
		validation       <-chan time.Time
		validationResult = make(chan bool, 1)
		validationActive bool
	)
	if !c.SessionExpiresAt.IsZero() {
		expiryTimer = time.NewTimer(time.Until(c.SessionExpiresAt))
		expiry = expiryTimer.C
		defer expiryTimer.Stop()
	}
	if c.ValidateSession != nil {
		interval := c.SessionValidationInterval
		if interval <= 0 {
			interval = defaultSessionValidationInterval
		}
		validationTicker = time.NewTicker(interval)
		validation = validationTicker.C
		defer validationTicker.Stop()
	}
	startValidation := func() {
		if c.ValidateSession == nil || validationActive {
			return
		}
		validationActive = true
		go func() {
			validationResult <- c.sessionValid(time.Now())
		}()
	}
	startValidation()

	for {
		select {
		case <-expiry:
			c.writeSessionClose("session expired")
			return
		case valid := <-validationResult:
			validationActive = false
			if !valid {
				c.writeSessionClose("session revoked")
				return
			}
		case <-validation:
			startValidation()
		case <-heartBeatTicker.C:
			_ = c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case msg, ok := <-c.Send:
			if !ok {
				return
			}
			_ = c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.Conn.WriteMessage(msg.MessageType, msg.Data); err != nil {
				return
			}
		}
	}
}

func (c *Client) sessionExpired(now time.Time) bool {
	return !c.SessionExpiresAt.IsZero() && !now.Before(c.SessionExpiresAt)
}

func (c *Client) sessionValid(now time.Time) bool {
	if c.sessionExpired(now) {
		return false
	}
	return c.ValidateSession == nil || c.ValidateSession()
}

func (c *Client) writeSessionClose(reason string) {
	_ = c.Conn.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.ClosePolicyViolation, reason),
		time.Now().Add(writeWait),
	)
}

// Listen is a block method that listens for incoming messages from the client.
func (c *Client) Listen() {
	c.Conn.SetReadLimit(maxInboundMessageBytes)
	_ = c.Conn.SetReadDeadline(time.Now().Add(pongWait))
	c.Conn.SetPongHandler(func(string) error {
		return c.Conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		msgType, msg, err := c.Conn.ReadMessage()
		if err != nil {
			break
		}
		_ = c.Conn.SetReadDeadline(time.Now().Add(pongWait))

		if msgType == websocket.TextMessage {
			if !c.allowIncoming(time.Now()) {
				c.SendError("message rate limit exceeded")
				break
			}
			c.processIncomingMessage(msg)
		} else {
			c.Hub.RemoveClient(c)
			c.close()
			return
		}
	}
	c.Hub.RemoveClient(c)
	c.close()
}

func (c *Client) allowIncoming(now time.Time) bool {
	c.rateMu.Lock()
	defer c.rateMu.Unlock()
	if c.rateWindowStart.IsZero() || now.Sub(c.rateWindowStart) >= inboundRateWindow {
		c.rateWindowStart = now
		c.rateCount = 0
	}
	c.rateCount++
	return c.rateCount <= maxInboundMessages
}

// processIncomingMessage processes incoming messages from the client.
func (c *Client) processIncomingMessage(data []byte) {
	if string(data) == "ping" {
		if _, err := c.Hub.userStore.UpdateLastActive(c.ID); err != nil {
			c.Hub.lo.Error("UpdateLastActive failed", "client_id", c.ID, "error", err)
		}
		c.SendMessage([]byte("pong"), websocket.TextMessage)
		return
	}

	// Try to parse as JSON message
	var msg models.Message
	if err := json.Unmarshal(data, &msg); err != nil {
		c.SendError("invalid message format")
		return
	}

	switch msg.Type {
	case models.MessageTypeConversationSubscribe:
		c.handleConversationSubscribe(msg.Data)
	case models.MessageTypeListSubscribeReplace:
		c.handleListSubscribe(msg.Data)
	case models.MessageTypeTyping:
		c.handleTyping(msg.Data)
	default:
		c.SendError("unknown message type")
	}
}

const maxListSubUUIDs = 500

func (c *Client) handleListSubscribe(data interface{}) {
	dataBytes, err := json.Marshal(data)
	if err != nil {
		c.SendError("invalid list_subscribe payload")
		return
	}
	var payload struct {
		UUIDs []string `json:"uuids"`
	}
	if err := json.Unmarshal(dataBytes, &payload); err != nil {
		c.SendError("invalid list_subscribe payload")
		return
	}
	if len(payload.UUIDs) > maxListSubUUIDs {
		payload.UUIDs = payload.UUIDs[:maxListSubUUIDs]
	}
	authorized, err := c.Hub.conversationStore.FilterAuthorizedListUUIDs(c.ID, payload.UUIDs)
	if err != nil {
		return
	}
	c.Hub.SubscribeListReplace(c, authorized)
}

// handleConversationSubscribe registers the open-conversation sub; authz is enforced because content (not just typing) flows through it.
func (c *Client) handleConversationSubscribe(data interface{}) {
	dataBytes, err := json.Marshal(data)
	if err != nil {
		c.SendError("invalid subscription data")
		return
	}

	var subscribeMsg models.ConversationSubscribe
	if err := json.Unmarshal(dataBytes, &subscribeMsg); err != nil {
		c.SendError("invalid subscription format")
		return
	}

	if subscribeMsg.ConversationUUID == "" {
		c.SendError("conversation_uuid is required")
		return
	}

	// Authz: silently reject if the agent can't read this conversation.
	authorized, err := c.Hub.conversationStore.FilterAuthorizedListUUIDs(c.ID, []string{subscribeMsg.ConversationUUID})
	if err != nil || len(authorized) == 0 {
		return
	}

	c.Hub.SubscribeOpenConv(c, subscribeMsg.ConversationUUID)
}

// handleTyping handles typing indicator messages.
func (c *Client) handleTyping(data interface{}) {
	// Convert the data to JSON and then unmarshal to TypingMessage
	dataBytes, err := json.Marshal(data)
	if err != nil {
		c.SendError("invalid typing data")
		return
	}

	var typingMsg models.TypingMessage
	if err := json.Unmarshal(dataBytes, &typingMsg); err != nil {
		c.SendError("invalid typing format")
		return
	}

	if typingMsg.ConversationUUID == "" {
		c.SendError("conversation_uuid is required for typing")
		return
	}

	authorized, err := c.Hub.conversationStore.FilterAuthorizedListUUIDs(c.ID, []string{typingMsg.ConversationUUID})
	if err != nil || len(authorized) == 0 {
		return
	}

	c.Hub.BroadcastTypingToConversation(typingMsg.ConversationUUID, typingMsg)
}

// close closes the client connection.
func (c *Client) close() {
	c.closeOnce.Do(func() {
		c.sendMu.Lock()
		defer c.sendMu.Unlock()
		c.Closed.Set(true)
		close(c.Send)
	})
}

// SendError sends an error message to client.
func (c *Client) SendError(msg string) {
	out := models.Message{
		Type: models.MessageTypeError,
		Data: msg,
	}
	b, _ := json.Marshal(out)

	if !c.enqueue(models.WSMessage{Data: b, MessageType: websocket.TextMessage}) {
		c.Hub.lo.Warn("client send channel full, could not send error message", "client_id", c.ID)
		c.Hub.RemoveClient(c)
		c.close()
	}
}

// SendMessage sends a message to client.
func (c *Client) SendMessage(b []byte, typ byte) {
	c.enqueue(models.WSMessage{Data: b, MessageType: int(typ)})
}

func (c *Client) enqueue(msg models.WSMessage) bool {
	c.sendMu.RLock()
	defer c.sendMu.RUnlock()
	if c.Closed.Get() {
		return false
	}
	select {
	case c.Send <- msg:
		return true
	default:
		return false
	}
}
