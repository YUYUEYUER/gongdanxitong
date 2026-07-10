package main

import (
	"context"
	"strings"
	"testing"
	"time"

	umodels "github.com/abhinavxd/libredesk/internal/user/models"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/valyala/fasthttp"
)

type fakeAgentWebSocketUserStore struct {
	user umodels.User
	err  error
}

func (s *fakeAgentWebSocketUserStore) GetAgent(int, string) (umodels.User, error) {
	return s.user, s.err
}

func TestIsSameOriginWebSocket(t *testing.T) {
	tests := []struct {
		name   string
		host   string
		origin string
		want   bool
	}{
		{name: "production same origin", host: "desk.example.com", origin: "https://desk.example.com", want: true},
		{name: "cross origin", host: "desk.example.com", origin: "https://evil.example", want: false},
		{name: "localhost cannot target production", host: "desk.example.com", origin: "http://localhost:5173", want: false},
		{name: "exact localhost dev", host: "localhost:5173", origin: "http://localhost:5173", want: true},
		{name: "insecure production", host: "desk.example.com", origin: "http://desk.example.com", want: false},
		{name: "missing origin", host: "desk.example.com", origin: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &fasthttp.RequestCtx{}
			ctx.Request.Header.SetHost(tt.host)
			if tt.origin != "" {
				ctx.Request.Header.Set("Origin", tt.origin)
			}
			if got := isSameOriginWebSocket(ctx); got != tt.want {
				t.Fatalf("isSameOriginWebSocket() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidAgentSessionID(t *testing.T) {
	valid := strings.Repeat("aB3", 21) + "z"
	if !validAgentSessionID(valid) {
		t.Fatal("expected generated 64-character session ID to be valid")
	}
	for _, invalid := range []string{
		"",
		strings.Repeat("a", agentSessionIDLength-1),
		strings.Repeat("a", agentSessionIDLength-1) + ":",
	} {
		if validAgentSessionID(invalid) {
			t.Fatalf("expected session ID %q to be invalid", invalid)
		}
	}
}

func TestAgentSessionExpiryUsesRemainingRedisTTL(t *testing.T) {
	server := miniredis.RunT(t)
	rd := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = rd.Close() })

	sessionID := strings.Repeat("s", agentSessionIDLength)
	key := agentSessionRedisPrefix + sessionID
	if err := rd.HSet(context.Background(), key, "_ss", "1").Err(); err != nil {
		t.Fatal(err)
	}
	if err := rd.PExpire(context.Background(), key, 2*time.Minute).Err(); err != nil {
		t.Fatal(err)
	}

	now := time.Unix(1_700_000_000, 0)
	expiresAt, err := agentSessionExpiry(context.Background(), rd, sessionID, now)
	if err != nil {
		t.Fatal(err)
	}
	if got := expiresAt.Sub(now); got != 2*time.Minute {
		t.Fatalf("session lifetime = %v, want %v", got, 2*time.Minute)
	}

	if err := rd.Del(context.Background(), key).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := agentSessionExpiry(context.Background(), rd, sessionID, now); err == nil {
		t.Fatal("expected missing Redis session to be rejected")
	}
}

func TestValidateAgentWebSocketSession(t *testing.T) {
	server := miniredis.RunT(t)
	rd := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = rd.Close() })

	sessionID := strings.Repeat("v", agentSessionIDLength)
	key := agentSessionRedisPrefix + sessionID
	expected := umodels.User{ID: 7, Type: umodels.UserTypeAgent, SessionVersion: 3}
	store := &fakeAgentWebSocketUserStore{user: umodels.User{
		ID:             expected.ID,
		Type:           umodels.UserTypeAgent,
		Enabled:        true,
		SessionVersion: expected.SessionVersion,
	}}
	if err := rd.HSet(context.Background(), key, map[string]any{
		"_ss":             "1",
		"id":              expected.ID,
		"type":            umodels.UserTypeAgent,
		"session_version": expected.SessionVersion,
	}).Err(); err != nil {
		t.Fatal(err)
	}

	if err := validateAgentWebSocketSession(context.Background(), rd, store, sessionID, expected); err != nil {
		t.Fatalf("valid session rejected: %v", err)
	}

	store.user.Enabled = false
	if err := validateAgentWebSocketSession(context.Background(), rd, store, sessionID, expected); err == nil {
		t.Fatal("disabled agent must revoke websocket session")
	}
	store.user.Enabled = true
	store.user.SessionVersion++
	if err := validateAgentWebSocketSession(context.Background(), rd, store, sessionID, expected); err == nil {
		t.Fatal("changed user session version must revoke websocket session")
	}
	store.user.SessionVersion = expected.SessionVersion

	if err := rd.HSet(context.Background(), key, "session_version", expected.SessionVersion+1).Err(); err != nil {
		t.Fatal(err)
	}
	if err := validateAgentWebSocketSession(context.Background(), rd, store, sessionID, expected); err == nil {
		t.Fatal("changed Redis session version must revoke websocket session")
	}
	if err := rd.Del(context.Background(), key).Err(); err != nil {
		t.Fatal(err)
	}
	if err := validateAgentWebSocketSession(context.Background(), rd, store, sessionID, expected); err == nil {
		t.Fatal("destroyed Redis session must revoke websocket session")
	}
}
