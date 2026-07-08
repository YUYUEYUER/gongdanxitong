package email

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/abhinavxd/libredesk/internal/conversation/models"
	"github.com/abhinavxd/libredesk/internal/stringutil"
)

const (
	defaultResendAPIURL = "https://api.resend.com/emails"
)

type resendEmailPayload struct {
	From        string             `json:"from"`
	To          []string           `json:"to"`
	Cc          []string           `json:"cc,omitempty"`
	Bcc         []string           `json:"bcc,omitempty"`
	Subject     string             `json:"subject"`
	HTML        string             `json:"html,omitempty"`
	Text        string             `json:"text,omitempty"`
	ReplyTo     string             `json:"reply_to,omitempty"`
	Headers     map[string]string  `json:"headers,omitempty"`
	Attachments []resendAttachment `json:"attachments,omitempty"`
}

type resendAttachment struct {
	Filename    string `json:"filename"`
	Content     string `json:"content"`
	ContentType string `json:"content_type,omitempty"`
	ContentID   string `json:"content_id,omitempty"`
}

type resendErrorResponse struct {
	Name    string `json:"name"`
	Message string `json:"message"`
}

func (e *Email) sendWithResend(m models.OutboundMessage) error {
	if e.resendCfg == nil || e.resendCfg.APIKey == "" {
		return fmt.Errorf("resend API key is not configured")
	}

	payload, err := e.buildResendPayload(m)
	if err != nil {
		return err
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshalling resend payload: %w", err)
	}

	apiURL := strings.TrimSpace(e.resendCfg.APIURL)
	if apiURL == "" {
		apiURL = defaultResendAPIURL
	}

	req, err := http.NewRequest(http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating resend request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+e.resendCfg.APIKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("sending resend request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		return nil
	}

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var resendErr resendErrorResponse
	if err := json.Unmarshal(respBody, &resendErr); err == nil && resendErr.Message != "" {
		return fmt.Errorf("resend API error (%d): %s", resp.StatusCode, resendErr.Message)
	}
	return fmt.Errorf("resend API error (%d): %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
}

func (e *Email) buildResendPayload(m models.OutboundMessage) (resendEmailPayload, error) {
	emailAddress, err := stringutil.ExtractEmail(m.From)
	if err != nil {
		e.lo.Error("failed to extract email address from the 'from' header", "error", err)
		return resendEmailPayload{}, fmt.Errorf("failed to extract email address from 'From' header: %w", err)
	}

	headers := map[string]string{
		headerLibredeskLoopPrevention: emailAddress,
	}
	for key, value := range e.headers {
		headers[key] = value
	}
	if m.InReplyTo != "" {
		headers[headerInReplyTo] = "<" + m.InReplyTo + ">"
	}
	if m.SourceID != "" {
		headers[headerMessageID] = fmt.Sprintf("<%s>", m.SourceID)
	}
	if len(m.References) > 0 {
		var references string
		for _, ref := range m.References {
			references += "<" + ref + "> "
		}
		headers[headerReferences] = strings.TrimSpace(references)
	}
	if m.ConversationUUID != "" {
		headers[headerLibredeskConversationID] = m.ConversationUUID
	}

	payload := resendEmailPayload{
		From:        m.From,
		To:          m.To,
		Cc:          m.CC,
		Bcc:         m.BCC,
		Subject:     m.Subject,
		Headers:     headers,
		Attachments: buildResendAttachments(m),
	}

	if rt := resolveReplyTo(m.ReplyTo, e.replyTo, emailAddress, m.ConversationUUID, e.enablePlusAddressing); rt != "" {
		payload.ReplyTo = rt
	}

	switch m.ContentType {
	case "plain":
		payload.Text = m.Content
	default:
		payload.HTML = m.Content
		if len(m.AltContent) > 0 {
			payload.Text = m.AltContent
		}
	}

	return payload, nil
}

func buildResendAttachments(m models.OutboundMessage) []resendAttachment {
	if len(m.Attachments) == 0 {
		return nil
	}

	attachments := make([]resendAttachment, 0, len(m.Attachments))
	for _, file := range m.Attachments {
		attachment := resendAttachment{
			Filename:    file.Name,
			Content:     base64.StdEncoding.EncodeToString(file.Content),
			ContentType: file.Header.Get("Content-Type"),
			ContentID:   strings.Trim(file.Header.Get("Content-ID"), "<>"),
		}
		attachments = append(attachments, attachment)
	}
	return attachments
}
