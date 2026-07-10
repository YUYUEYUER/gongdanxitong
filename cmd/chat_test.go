package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/abhinavxd/libredesk/internal/inbox/channel/livechat"
	imodels "github.com/abhinavxd/libredesk/internal/inbox/models"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestValidWidgetJWTInputBounds(t *testing.T) {
	if validWidgetJWTInput("") {
		t.Fatal("empty JWT must be rejected")
	}
	if !validWidgetJWTInput(strings.Repeat("a", maxWidgetJWTBytes)) {
		t.Fatal("JWT at the size limit must be accepted")
	}
	if validWidgetJWTInput(strings.Repeat("a", maxWidgetJWTBytes+1)) {
		t.Fatal("oversized JWT must be rejected before parsing")
	}
}

func TestWidgetSessionBoundToInboxVersion(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	app := &App{redis: client}
	inbox := imodels.Inbox{
		ID:                   42,
		Channel:              livechat.ChannelLiveChat,
		Enabled:              true,
		WidgetSessionVersion: 7,
	}

	const origin = "https://shop.example.com"
	token, err := generateSessionToken(app, 99, 5, inbox, false, "customer-1", origin, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	session, err := loadSession(app, token, inbox, livechat.Config{SessionDuration: "1h"}, origin)
	if err != nil {
		t.Fatal(err)
	}
	if session.InboxSessionVersion != inbox.WidgetSessionVersion {
		t.Fatalf("got session version %d, want %d", session.InboxSessionVersion, inbox.WidgetSessionVersion)
	}
	if session.UserSessionVersion != 5 {
		t.Fatalf("got user session version %d, want 5", session.UserSessionVersion)
	}

	rotated := inbox
	rotated.WidgetSessionVersion++
	if _, err := loadSession(app, token, rotated, livechat.Config{SessionDuration: "1h"}, origin); err == nil {
		t.Fatal("session signed before inbox secret rotation must be rejected")
	}
	if server.Exists(widgetSessionPrefix + token) {
		t.Fatal("stale session should be removed after a version mismatch")
	}
}

func TestWidgetSessionDoesNotSurviveDisableAndReenable(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	app := &App{redis: client}
	inbox := imodels.Inbox{
		ID:                   42,
		Channel:              livechat.ChannelLiveChat,
		Enabled:              true,
		WidgetSessionVersion: 3,
	}
	const origin = "https://shop.example.com"
	token, err := generateSessionToken(app, 99, 2, inbox, true, "", origin, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	disabled := inbox
	disabled.Enabled = false
	disabled.WidgetSessionVersion++
	if _, err := loadSession(app, token, disabled, livechat.Config{}, origin); err == nil {
		t.Fatal("disabled inbox must reject widget sessions")
	}

	reenabled := disabled
	reenabled.Enabled = true
	reenabled.WidgetSessionVersion++
	if _, err := loadSession(app, token, reenabled, livechat.Config{}, origin); err == nil {
		t.Fatal("reenabling an inbox must not reactivate the old session")
	}
}

func TestWidgetSessionWithoutVersionIsRejected(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	app := &App{redis: client}
	inbox := imodels.Inbox{
		ID:                   42,
		Channel:              livechat.ChannelLiveChat,
		Enabled:              true,
		WidgetSessionVersion: 1,
	}
	key := widgetSessionPrefix + "legacy-token"
	if err := client.HSet(context.Background(), key, map[string]any{
		"user_id":    "99",
		"inbox_id":   "42",
		"is_visitor": "false",
	}).Err(); err != nil {
		t.Fatal(err)
	}

	if _, err := loadSession(app, "legacy-token", inbox, livechat.Config{}, "https://shop.example.com"); err == nil {
		t.Fatal("legacy session without inbox version must be rejected")
	}
	if server.Exists(key) {
		t.Fatal("legacy session should be removed")
	}
}

func TestWidgetSessionBoundToParentOrigin(t *testing.T) {
	server := miniredis.RunT(t)
	app := &App{redis: redis.NewClient(&redis.Options{Addr: server.Addr()})}
	inbox := imodels.Inbox{ID: 42, Channel: livechat.ChannelLiveChat, Enabled: true, WidgetSessionVersion: 1}
	token, err := generateSessionToken(app, 99, 1, inbox, false, "customer", "https://shop.example.com", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loadSession(app, token, inbox, livechat.Config{}, "https://admin.example.com"); err == nil {
		t.Fatal("widget session must not cross trusted parent origins")
	}
	if server.Exists(widgetSessionPrefix + token) {
		t.Fatal("origin-mismatched session should be revoked")
	}
}

func TestDeletedWidgetSessionFailsRevalidationLookup(t *testing.T) {
	server := miniredis.RunT(t)
	app := &App{redis: redis.NewClient(&redis.Options{Addr: server.Addr()})}
	inbox := imodels.Inbox{ID: 42, Channel: livechat.ChannelLiveChat, Enabled: true, WidgetSessionVersion: 1}
	const origin = "https://shop.example.com"
	token, err := generateSessionToken(app, 99, 1, inbox, false, "customer", origin, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := deleteSessionToken(app, token); err != nil {
		t.Fatal(err)
	}
	if _, err := loadSession(app, token, inbox, livechat.Config{}, origin); err == nil {
		t.Fatal("revoked Redis bearer must fail the websocket revalidation lookup")
	}
}

func TestGetSessionDurationBounds(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want time.Duration
	}{
		{name: "empty defaults", want: defaultSessionTTL},
		{name: "invalid defaults", raw: "invalid", want: defaultSessionTTL},
		{name: "below minimum defaults", raw: "30m", want: defaultSessionTTL},
		{name: "valid duration", raw: "12h", want: 12 * time.Hour},
		{name: "maximum accepted", raw: "720h", want: maxSessionTTL},
		{name: "above maximum clamped", raw: "2160h", want: maxSessionTTL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getSessionDuration(livechat.Config{SessionDuration: tt.raw})
			if got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDeleteWidgetSessionTokenReportsRevocation(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	app := &App{redis: client}
	token := "session-token"
	key := widgetSessionPrefix + token
	if err := client.Set(context.Background(), key, "value", time.Hour).Err(); err != nil {
		t.Fatal(err)
	}

	if err := deleteSessionToken(app, token); err != nil {
		t.Fatal(err)
	}
	if exists := server.Exists(key); exists {
		t.Fatal("session token should be deleted")
	}

	server.Close()
	if err := deleteSessionToken(app, token); err == nil {
		t.Fatal("Redis failure must be reported")
	}
}
