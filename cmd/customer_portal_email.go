package main

import (
	"fmt"
	"strings"

	cmodels "github.com/abhinavxd/libredesk/internal/conversation/models"
	inboxsvc "github.com/abhinavxd/libredesk/internal/inbox"
	imodels "github.com/abhinavxd/libredesk/internal/inbox/models"
	"github.com/abhinavxd/libredesk/internal/stringutil"
)

type customerPortalEmailInboxStore interface {
	GetAll() ([]imodels.Inbox, error)
	Get(int) (inboxsvc.Inbox, error)
}

func sendCustomerPortalEmail(store customerPortalEmailInboxStore, recipient, subject, content string) error {
	portalInboxes, err := store.GetAll()
	if err != nil {
		return fmt.Errorf("loading customer portal inboxes: %w", err)
	}

	var linkedEmailInboxID int
	for _, portalInbox := range portalInboxes {
		if portalInbox.Enabled &&
			portalInbox.Channel == inboxsvc.ChannelLiveChat &&
			portalInbox.Name == customerPortalInboxName &&
			portalInbox.LinkedEmailInboxID.Valid {
			linkedEmailInboxID = portalInbox.LinkedEmailInboxID.Int
			break
		}
	}
	if linkedEmailInboxID <= 0 {
		return fmt.Errorf("customer portal email inbox is not configured")
	}

	emailInbox, err := store.Get(linkedEmailInboxID)
	if err != nil {
		return fmt.Errorf("loading customer portal email inbox: %w", err)
	}
	if emailInbox.Channel() != inboxsvc.ChannelEmail {
		return fmt.Errorf("customer portal linked inbox is not an email inbox")
	}
	from := strings.TrimSpace(emailInbox.FromAddress())
	if from == "" {
		return fmt.Errorf("customer portal email inbox has no from address")
	}

	if err := emailInbox.Send(cmodels.OutboundMessage{
		From:        from,
		To:          []string{recipient},
		Subject:     subject,
		Content:     content,
		ContentType: cmodels.ContentTypeHTML,
		AltContent:  stringutil.HTML2Text(content),
	}); err != nil {
		return fmt.Errorf("sending customer portal email: %w", err)
	}
	return nil
}
