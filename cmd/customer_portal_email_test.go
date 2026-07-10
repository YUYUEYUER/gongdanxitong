package main

import (
	"context"
	"errors"
	"testing"

	cmodels "github.com/abhinavxd/libredesk/internal/conversation/models"
	inboxsvc "github.com/abhinavxd/libredesk/internal/inbox"
	imodels "github.com/abhinavxd/libredesk/internal/inbox/models"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"
)

type fakeCustomerPortalEmailStore struct {
	inboxes []imodels.Inbox
	email   inboxsvc.Inbox
}

func (s *fakeCustomerPortalEmailStore) GetAll() ([]imodels.Inbox, error) {
	return s.inboxes, nil
}

func (s *fakeCustomerPortalEmailStore) Get(id int) (inboxsvc.Inbox, error) {
	if s.email == nil || s.email.Identifier() != id {
		return nil, inboxsvc.ErrInboxNotFound
	}
	return s.email, nil
}

type fakeCustomerPortalEmailInbox struct {
	id      int
	from    string
	channel string
	sent    []cmodels.OutboundMessage
	sendErr error
}

func (i *fakeCustomerPortalEmailInbox) Close() error                  { return nil }
func (i *fakeCustomerPortalEmailInbox) Identifier() int               { return i.id }
func (i *fakeCustomerPortalEmailInbox) Receive(context.Context) error { return nil }
func (i *fakeCustomerPortalEmailInbox) Name() string                  { return "Customer email" }
func (i *fakeCustomerPortalEmailInbox) FromAddress() string           { return i.from }
func (i *fakeCustomerPortalEmailInbox) FromNameTemplate() string      { return "" }
func (i *fakeCustomerPortalEmailInbox) ReplyToAddress() string        { return "" }
func (i *fakeCustomerPortalEmailInbox) Channel() string               { return i.channel }
func (i *fakeCustomerPortalEmailInbox) Send(msg cmodels.OutboundMessage) error {
	i.sent = append(i.sent, msg)
	return i.sendErr
}

func TestSendCustomerPortalEmailUsesLinkedEmailInbox(t *testing.T) {
	emailInbox := &fakeCustomerPortalEmailInbox{
		id:      22,
		from:    "Support <support@example.com>",
		channel: inboxsvc.ChannelEmail,
	}
	store := &fakeCustomerPortalEmailStore{
		inboxes: []imodels.Inbox{{
			ID:                 11,
			Name:               customerPortalInboxName,
			Channel:            inboxsvc.ChannelLiveChat,
			Enabled:            true,
			LinkedEmailInboxID: null.IntFrom(22),
		}},
		email: emailInbox,
	}

	err := sendCustomerPortalEmail(store, "customer@example.com", "Verify email", "<p>Hello</p>")
	require.NoError(t, err)
	require.Len(t, emailInbox.sent, 1)
	require.Equal(t, "Support <support@example.com>", emailInbox.sent[0].From)
	require.Equal(t, []string{"customer@example.com"}, emailInbox.sent[0].To)
	require.Equal(t, "Verify email", emailInbox.sent[0].Subject)
	require.Equal(t, cmodels.ContentTypeHTML, emailInbox.sent[0].ContentType)
	require.Equal(t, "Hello", emailInbox.sent[0].AltContent)
}

func TestSendCustomerPortalEmailRequiresLinkedEmailInbox(t *testing.T) {
	store := &fakeCustomerPortalEmailStore{inboxes: []imodels.Inbox{{
		Name:    customerPortalInboxName,
		Channel: inboxsvc.ChannelLiveChat,
		Enabled: true,
	}}}

	err := sendCustomerPortalEmail(store, "customer@example.com", "Verify email", "<p>Hello</p>")
	require.ErrorContains(t, err, "not configured")
}

func TestSendCustomerPortalEmailPropagatesProviderFailure(t *testing.T) {
	emailInbox := &fakeCustomerPortalEmailInbox{
		id:      22,
		from:    "Support <support@example.com>",
		channel: inboxsvc.ChannelEmail,
		sendErr: errors.New("provider unavailable"),
	}
	store := &fakeCustomerPortalEmailStore{
		inboxes: []imodels.Inbox{{
			Name:               customerPortalInboxName,
			Channel:            inboxsvc.ChannelLiveChat,
			Enabled:            true,
			LinkedEmailInboxID: null.IntFrom(22),
		}},
		email: emailInbox,
	}

	err := sendCustomerPortalEmail(store, "customer@example.com", "Verify email", "<p>Hello</p>")
	require.ErrorContains(t, err, "provider unavailable")
}
