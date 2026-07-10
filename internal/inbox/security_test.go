package inbox

import (
	"strings"
	"testing"

	imodels "github.com/abhinavxd/libredesk/internal/inbox/models"
)

func TestNextWidgetSessionVersion(t *testing.T) {
	current := imodels.Inbox{
		Channel:              ChannelLiveChat,
		Enabled:              true,
		WidgetSessionVersion: 5,
	}

	if got := nextWidgetSessionVersion(current, current, false); got != 5 {
		t.Fatalf("display/config-only update changed version: got %d", got)
	}
	if got := nextWidgetSessionVersion(current, current, true); got != 6 {
		t.Fatalf("secret rotation did not increment version: got %d", got)
	}

	disabled := current
	disabled.Enabled = false
	if got := nextWidgetSessionVersion(current, disabled, false); got != 6 {
		t.Fatalf("disabling did not increment version: got %d", got)
	}

	email := current
	email.Channel = ChannelEmail
	if got := nextWidgetSessionVersion(current, email, false); got != 6 {
		t.Fatalf("channel change did not increment version: got %d", got)
	}
}

func TestLivechatToggleAndDeleteIncrementWidgetSessionVersion(t *testing.T) {
	b, err := efs.ReadFile("queries.sql")
	if err != nil {
		t.Fatal(err)
	}
	queries := string(b)
	if got := strings.Count(queries, "WHEN channel = 'livechat' THEN widget_session_version + 1"); got < 2 {
		t.Fatalf("toggle and soft-delete must each revoke livechat sessions; found %d increments", got)
	}
}
