package email

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cmodels "github.com/abhinavxd/libredesk/internal/conversation/models"
	imodels "github.com/abhinavxd/libredesk/internal/inbox/models"
	"github.com/stretchr/testify/require"
)

func TestNewWithResendDoesNotRequireSMTP(t *testing.T) {
	inbox, err := New(nil, nil, Opts{
		Config: imodels.Config{
			From:             "Support <support@example.com>",
			OutboundProvider: imodels.OutboundProviderResend,
			Resend: &imodels.ResendConfig{
				APIKey: "re_test",
			},
		},
	})

	require.NoError(t, err)
	require.Equal(t, imodels.OutboundProviderResend, inbox.outboundProvider)
	require.Empty(t, inbox.smtpPools)
}

func TestSendWithResendPostsEmailPayload(t *testing.T) {
	var got resendEmailPayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "Bearer re_test", r.Header.Get("Authorization"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"email-id"}`))
	}))
	defer server.Close()

	inbox := &Email{
		outboundProvider:     imodels.OutboundProviderResend,
		resendCfg:            &imodels.ResendConfig{APIKey: "re_test", APIURL: server.URL},
		resendHTTPClient:     server.Client(),
		replyTo:              "support@example.com",
		enablePlusAddressing: true,
		headers:              map[string]string{"X-Custom": "yes"},
	}

	err := inbox.Send(cmodels.OutboundMessage{
		From:             "Support <support@example.com>",
		To:               []string{"customer@example.com"},
		CC:               []string{"cc@example.com"},
		Subject:          "Ticket reply",
		Content:          "<p>Hello</p>",
		ContentType:      cmodels.ContentTypeHTML,
		ConversationUUID: "abc-123",
		SourceID:         "source@example.com",
		InReplyTo:        "previous@example.com",
		References:       []string{"first@example.com", "previous@example.com"},
	})

	require.NoError(t, err)
	require.Equal(t, "Support <support@example.com>", got.From)
	require.Equal(t, []string{"customer@example.com"}, got.To)
	require.Equal(t, []string{"cc@example.com"}, got.Cc)
	require.Equal(t, "Ticket reply", got.Subject)
	require.Equal(t, "<p>Hello</p>", got.HTML)
	require.Equal(t, "support+conv-abc-123@example.com", got.ReplyTo)
	require.Equal(t, "yes", got.Headers["X-Custom"])
	require.Equal(t, "<source@example.com>", got.Headers[headerMessageID])
	require.Equal(t, "<previous@example.com>", got.Headers[headerInReplyTo])
	require.Equal(t, "<first@example.com> <previous@example.com>", got.Headers[headerReferences])
	require.Equal(t, "abc-123", got.Headers[headerLibredeskConversationID])
}

func TestNewResendHTTPClientRequiresHTTPS(t *testing.T) {
	if _, err := newResendHTTPClient("http://api.example.com/emails"); err == nil {
		t.Fatal("expected HTTP URL to be rejected")
	}
	if _, err := newResendHTTPClient("https://api.example.com/emails"); err != nil {
		t.Fatalf("expected public HTTPS URL to be accepted: %v", err)
	}
}

func TestResendHTTPClientBlocksLoopbackAtDial(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("loopback request reached server")
	}))
	defer server.Close()

	client, err := newResendHTTPClient(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Get(server.URL)
	if err == nil || !strings.Contains(err.Error(), "ssrf: connection to reserved address") {
		t.Fatalf("expected SSRF guard rejection, got %v", err)
	}
}
