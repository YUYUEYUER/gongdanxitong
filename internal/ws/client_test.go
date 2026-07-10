package ws

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/abhinavxd/libredesk/internal/ws/models"
	"github.com/fasthttp/websocket"
)

type testConversationStore struct {
	authorized bool
	typing     int
}

func (s *testConversationStore) BroadcastTypingToWidgetClientsOnly(string, bool) {
	s.typing++
}

func (s *testConversationStore) FilterAuthorizedListUUIDs(_ int, uuids []string) ([]string, error) {
	if !s.authorized {
		return nil, nil
	}
	return uuids, nil
}

func TestClientInboundRateLimitResetsAfterWindow(t *testing.T) {
	c := &Client{}
	now := time.Unix(1_700_000_000, 0)
	for i := 0; i < maxInboundMessages; i++ {
		if !c.allowIncoming(now) {
			t.Fatalf("message %d unexpectedly rejected", i+1)
		}
	}
	if c.allowIncoming(now) {
		t.Fatal("expected message over limit to be rejected")
	}
	if !c.allowIncoming(now.Add(inboundRateWindow)) {
		t.Fatal("expected new window to allow messages")
	}
}

func TestClientCloseIsIdempotent(t *testing.T) {
	c := &Client{Send: make(chan models.WSMessage)}
	c.close()
	c.close()
	if !c.Closed.Get() {
		t.Fatal("expected client to be closed")
	}
}

func TestClientSessionValidity(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	c := &Client{
		SessionExpiresAt: now.Add(time.Minute),
		ValidateSession:  func() bool { return true },
	}
	if !c.sessionValid(now) {
		t.Fatal("valid, unexpired session was rejected")
	}
	if c.sessionValid(now.Add(time.Minute)) {
		t.Fatal("session must be invalid at its exact expiry")
	}

	c.SessionExpiresAt = now.Add(time.Minute)
	c.ValidateSession = func() bool { return false }
	if c.sessionValid(now) {
		t.Fatal("revoked session was accepted")
	}
}

func TestClientHardExpiryDoesNotWaitForSessionValidation(t *testing.T) {
	validationStarted := make(chan struct{})
	releaseValidation := make(chan struct{})
	serveDone := make(chan struct{})
	var startedOnce sync.Once

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		client := &Client{
			Conn:             conn,
			Send:             make(chan models.WSMessage, 1),
			SessionExpiresAt: time.Now().Add(100 * time.Millisecond),
			ValidateSession: func() bool {
				startedOnce.Do(func() { close(validationStarted) })
				<-releaseValidation
				return true
			},
		}
		client.Serve()
		close(serveDone)
	}))
	defer server.Close()

	peer, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()

	select {
	case <-validationStarted:
	case <-time.After(time.Second):
		t.Fatal("session validation did not start")
	}
	_ = peer.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := peer.ReadMessage(); !websocket.IsCloseError(err, websocket.ClosePolicyViolation) {
		t.Fatalf("expected hard-expiry policy close while validation was blocked, got %v", err)
	}
	close(releaseValidation)
	select {
	case <-serveDone:
	case <-time.After(time.Second):
		t.Fatal("websocket server did not stop after hard expiry")
	}
}

func TestTypingRequiresConversationAccess(t *testing.T) {
	store := &testConversationStore{}
	hub := &Hub{conversationStore: store}
	c := &Client{ID: 7, Hub: hub, Send: make(chan models.WSMessage, 1)}
	payload := map[string]any{"conversation_uuid": "ticket-1", "is_typing": true}

	c.handleTyping(payload)
	if store.typing != 0 {
		t.Fatal("unauthorized typing event was broadcast")
	}

	store.authorized = true
	c.handleTyping(payload)
	if store.typing != 1 {
		t.Fatalf("expected one authorized broadcast, got %d", store.typing)
	}
}
