package main

import (
	"testing"
	"time"
)

func TestWidgetWebSocketFrameBudget(t *testing.T) {
	conn := &safeConn{}
	for i := 0; i < wsMaxFramesPerMinute; i++ {
		if !conn.allowFrame() {
			t.Fatalf("frame %d should be allowed", i+1)
		}
	}
	if conn.allowFrame() {
		t.Fatal("frame above the per-minute budget should be rejected")
	}

	conn.frameWindowStart = time.Now().Add(-time.Minute)
	if !conn.allowFrame() {
		t.Fatal("budget should reset after a minute")
	}
}

func TestWidgetWebSocketRequiresJoinAsFirstFrame(t *testing.T) {
	t.Parallel()

	for _, messageType := range []string{WidgetMsgTypePing, WidgetMsgTypeTyping, WidgetMsgTypePageVisit, ""} {
		if isAllowedBeforeWidgetJoin(messageType) {
			t.Fatalf("unauthenticated %q frame must be rejected", messageType)
		}
	}
	if !isAllowedBeforeWidgetJoin(WidgetMsgTypeJoin) {
		t.Fatal("join must be the only allowed unauthenticated frame")
	}
	if wsJoinDeadline <= 0 || wsJoinDeadline >= wsReadDeadline {
		t.Fatalf("unauthenticated join deadline must be positive and shorter than the session deadline")
	}
}

func TestSanitizeWidgetPageVisitKeepsOnlyOrigin(t *testing.T) {
	visit := WidgetPageVisitData{
		URL:   "https://shop.example.com/reset/secret-token?code=oauth#private",
		Title: "Order for Alice",
	}
	if !sanitizeWidgetPageVisit(&visit) {
		t.Fatal("valid HTTPS origin should be accepted")
	}
	if visit.URL != "https://shop.example.com/" || visit.Title != "" {
		t.Fatalf("unexpected sanitized visit: %#v", visit)
	}

	for _, raw := range []string{"javascript:alert(1)", "https://user:pass@example.com/private", "//missing-scheme.example/path"} {
		candidate := WidgetPageVisitData{URL: raw}
		if sanitizeWidgetPageVisit(&candidate) {
			t.Fatalf("unsafe page URL should be rejected: %s", raw)
		}
	}
}
