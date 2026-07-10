package email

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/abhinavxd/libredesk/internal/attachment"
	"github.com/abhinavxd/libredesk/internal/conversation/models"
	imodels "github.com/abhinavxd/libredesk/internal/inbox/models"
	"github.com/abhinavxd/libredesk/internal/stringutil"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/jhillyerd/enmime"
	"github.com/volatiletech/null/v9"
)

const (
	defaultReadInterval       = time.Duration(5 * time.Minute)
	defaultScanInboxSince     = time.Duration(48 * time.Hour)
	maxRawEmailBytes          = int64(25 << 20)
	maxEmailBodyBytes         = 2 << 20
	maxEmailSubjectBytes      = 1_000
	maxEmailAttachments       = 25
	maxEmailAttachment        = 10 << 20
	maxEmailAttachmentsTotal  = 20 << 20
	maxIMAPMessagesPerPoll    = 100
	maxIMAPHeadMessagesPoll   = 50
	maxIMAPBytesPerPoll       = int64(100 << 20)
	maxIMAPHeaderBytes        = int64(64 << 10)
	maxIMAPInboxMessagesHour  = 500
	maxIMAPInboxBytesHour     = int64(512 << 20)
	maxIMAPInboxMessagesDay   = 5_000
	maxIMAPInboxBytesDay      = int64(2 << 30)
	maxIMAPSenderMessagesHour = 30
	maxIMAPSenderBytesHour    = int64(50 << 20)
	maxIMAPSenderMessagesDay  = 200
	maxIMAPSenderBytesDay     = int64(250 << 20)
	imapIngressSweepInterval  = time.Hour
)

var (
	errIMAPPollByteBudget = errors.New("IMAP poll byte budget exhausted")
	errIMAPIngressBudget  = errors.New("IMAP rolling ingress budget exhausted")
)

// ReadIncomingMessages reads and processes incoming messages from an IMAP server based on the provided configuration.
func (e *Email) ReadIncomingMessages(ctx context.Context, cfg imodels.IMAPConfig) error {
	readInterval, err := time.ParseDuration(cfg.ReadInterval)
	if err != nil {
		e.lo.Warn("could not parse IMAP read interval, using the default read interval of 5 minutes", "interval", cfg.ReadInterval, "inbox_id", e.Identifier(), "error", err)
		readInterval = defaultReadInterval
	}

	scanInboxSince, err := time.ParseDuration(cfg.ScanInboxSince)
	if err != nil {
		e.lo.Warn("could not parse IMAP scan inbox since duration, using the default value of 48 hours", "interval", cfg.ScanInboxSince, "inbox_id", e.Identifier(), "error", err)
		scanInboxSince = defaultScanInboxSince
	}

	readTicker := time.NewTicker(readInterval)
	defer readTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-readTicker.C:
			// If the ticker interval is too short, it may trigger while the previous `processMailbox` call is still running,
			// leading to overlapping executions or delays in handling context cancellation, check if the context is already done.
			if ctx.Err() != nil {
				return nil
			}

			if err := e.processMailbox(ctx, scanInboxSince, cfg); err != nil && err != context.Canceled {
				e.lo.Error("error searching emails", "error", err)
			}
			e.lo.Info("email search complete", "mailbox", cfg.Mailbox, "inbox_id", e.Identifier())
		}
	}
}

// processMailbox processes emails in the specified mailbox.
func (e *Email) processMailbox(ctx context.Context, scanInboxSince time.Duration, cfg imodels.IMAPConfig) error {
	var (
		client *imapclient.Client
		err    error
	)

	address := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	imapOptions := &imapclient.Options{
		TLSConfig: &tls.Config{
			InsecureSkipVerify: cfg.TLSSkipVerify,
		},
	}
	switch cfg.TLSType {
	case "none":
		client, err = imapclient.DialInsecure(address, imapOptions)
	case "starttls":
		client, err = imapclient.DialStartTLS(address, imapOptions)
	case "tls":
		client, err = imapclient.DialTLS(address, imapOptions)
	default:
		return fmt.Errorf("unknown IMAP TLS type: %q", cfg.TLSType)
	}
	if err != nil {
		return fmt.Errorf("failed to connect to IMAP server: %w", err)
	}

	defer client.Logout()

	// Authenticate based on auth type
	if e.authType == imodels.AuthTypeOAuth2 && e.oauth != nil {
		// Refresh OAuth token if needed
		oauthConfig, _, err := e.refreshOAuthIfNeeded()
		if err != nil {
			return err
		}

		// Use XOAUTH2 authentication
		saslClient := &xoauth2IMAPClient{
			username: cfg.Username,
			token:    oauthConfig.AccessToken,
		}
		if err := client.Authenticate(saslClient); err != nil {
			return fmt.Errorf("error authenticating with OAuth to IMAP server: %w", err)
		}
	} else {
		if err := client.Login(cfg.Username, cfg.Password).Wait(); err != nil {
			return fmt.Errorf("error logging in to the IMAP server: %w", err)
		}
	}

	selected, err := client.Select(cfg.Mailbox, &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		return fmt.Errorf("error selecting mailbox: %w", err)
	}

	// Scan emails since the specified duration.
	since := time.Now().Add(-scanInboxSince)

	e.lo.Info("searching emails", "since", since, "mailbox", cfg.Mailbox, "inbox_id", e.Identifier())

	cursorKey := imapCursorKey(cfg)
	cursor := e.getIMAPCursor(cursorKey)
	window := boundedIMAPSearchWindow(selected.NumMessages, cursor, maxIMAPMessagesPerPoll, maxIMAPHeadMessagesPoll)
	if window.count == 0 {
		return nil
	}

	// Apply the bound to SEARCH itself, not just to the later FETCH.
	searchResults, err := e.searchMessages(client, since, window.set)
	if err != nil {
		return fmt.Errorf("error searching messages: %w", err)
	}

	nextCursor, err := e.fetchAndProcessMessages(ctx, client, searchResults, e.Identifier(), cursor, window.nextCursor, cursorKey)
	e.setIMAPCursor(cursorKey, nextCursor)
	return err
}

// searchMessages uses a sequence-number constraint that is already bounded.
// Standard SEARCH is intentionally used so servers without ESEARCH have the
// same bounded response size.
func (e *Email) searchMessages(client *imapclient.Client, since time.Time, seqSet imap.SeqSet) (*imap.SearchData, error) {
	return client.Search(boundedIMAPSearchCriteria(since, seqSet), nil).Wait()
}

func boundedIMAPSearchCriteria(since time.Time, seqSet imap.SeqSet) *imap.SearchCriteria {
	return &imap.SearchCriteria{
		Since:  since,
		SeqNum: []imap.SeqSet{seqSet},
	}
}

type imapSearchWindow struct {
	set        imap.SeqSet
	count      int
	nextCursor uint32
}

// boundedIMAPSearchWindow always includes the newest messages and uses the
// remaining capacity to walk older mail backwards. This keeps new delivery
// responsive while eventually revisiting failures and historical backlog.
func boundedIMAPSearchWindow(numMessages, cursor uint32, limit, headLimit int) imapSearchWindow {
	if numMessages == 0 || limit <= 0 {
		return imapSearchWindow{}
	}
	if uint64(limit) > uint64(numMessages) {
		limit = int(numMessages)
	}
	if headLimit <= 0 {
		headLimit = 1
	}
	if headLimit > limit {
		headLimit = limit
	}

	headStart := numMessages - uint32(headLimit) + 1
	var set imap.SeqSet
	set.AddRange(headStart, numMessages)
	count := headLimit
	nextCursor := uint32(0)

	backlogCapacity := limit - headLimit
	if backlogCapacity > 0 && headStart > 1 {
		maxBacklogCursor := headStart - 1
		if cursor == 0 || cursor > maxBacklogCursor {
			cursor = maxBacklogCursor
		}
		backlogStart := uint32(1)
		if cursor >= uint32(backlogCapacity) {
			backlogStart = cursor - uint32(backlogCapacity) + 1
		}
		set.AddRange(backlogStart, cursor)
		count += int(cursor - backlogStart + 1)
		if backlogStart > 1 {
			nextCursor = backlogStart - 1
		} else {
			nextCursor = maxBacklogCursor
		}
	}

	return imapSearchWindow{set: set, count: count, nextCursor: nextCursor}
}

type imapSequencePage struct {
	set        imap.SeqSet
	nums       []uint32
	nextCursor uint32
}

// boundedSearchSequencePage returns a bounded, ascending page without
// expanding the complete SEARCH result into a slice.
func boundedSearchSequencePage(searchResults *imap.SearchData, cursor uint32, limit int) imapSequencePage {
	if searchResults == nil || limit <= 0 {
		return imapSequencePage{}
	}

	var ranges imap.SeqSet
	if all, ok := searchResults.All.(imap.SeqSet); ok && len(all) > 0 {
		ranges = all
	}
	if len(ranges) == 0 || ranges.Dynamic() {
		return imapSequencePage{}
	}

	collect := func(start uint32) []uint32 {
		nums := make([]uint32, 0, limit)
		for _, seqRange := range ranges {
			if seqRange.Start == 0 || seqRange.Stop == 0 || (start > 0 && seqRange.Stop < start) {
				continue
			}
			first := seqRange.Start
			if start > first {
				first = start
			}
			for seqNum := first; seqNum <= seqRange.Stop && len(nums) < limit; seqNum++ {
				nums = append(nums, seqNum)
				if seqNum == ^uint32(0) {
					break
				}
			}
			if len(nums) == limit {
				break
			}
		}
		return nums
	}

	nums := collect(cursor)
	if len(nums) == 0 && cursor > 0 {
		nums = collect(0)
	}
	if len(nums) == 0 {
		return imapSequencePage{}
	}

	var set imap.SeqSet
	set.AddNum(nums...)
	nextCursor := nums[len(nums)-1] + 1
	if nextCursor == 0 {
		nextCursor = 1
	}
	return imapSequencePage{set: set, nums: nums, nextCursor: nextCursor}
}

type imapPollBudget struct {
	remaining int64
}

type imapIngressEvent struct {
	at   time.Time
	size int64
}

type imapIngressLimits struct {
	hourMessages int
	hourBytes    int64
	dayMessages  int
	dayBytes     int64
}

func defaultIMAPInboxIngressLimits() imapIngressLimits {
	return imapIngressLimits{
		hourMessages: maxIMAPInboxMessagesHour,
		hourBytes:    maxIMAPInboxBytesHour,
		dayMessages:  maxIMAPInboxMessagesDay,
		dayBytes:     maxIMAPInboxBytesDay,
	}
}

func defaultIMAPSenderIngressLimits() imapIngressLimits {
	return imapIngressLimits{
		hourMessages: maxIMAPSenderMessagesHour,
		hourBytes:    maxIMAPSenderBytesHour,
		dayMessages:  maxIMAPSenderMessagesDay,
		dayBytes:     maxIMAPSenderBytesDay,
	}
}

func newIMAPPollBudget(limit int64) imapPollBudget {
	if limit < 0 {
		limit = 0
	}
	return imapPollBudget{remaining: limit}
}

func (b *imapPollBudget) reserve(messageSize int64) bool {
	if !b.canReserve(messageSize) {
		return false
	}
	b.remaining -= normalizedIMAPMessageSize(messageSize)
	return true
}

func (b *imapPollBudget) canReserve(messageSize int64) bool {
	return b != nil && normalizedIMAPMessageSize(messageSize) <= b.remaining
}

func normalizedIMAPMessageSize(messageSize int64) int64 {
	if messageSize <= 0 {
		return maxRawEmailBytes
	}
	return messageSize
}

func (e *Email) reserveIMAPIngress(mailboxKey, sender string, messageSize int64, now time.Time) error {
	return e.reserveIMAPIngressWithLimits(
		mailboxKey,
		sender,
		messageSize,
		now,
		defaultIMAPInboxIngressLimits(),
		defaultIMAPSenderIngressLimits(),
	)
}

func (e *Email) reserveIMAPIngressWithLimits(mailboxKey, sender string, messageSize int64, now time.Time, inboxLimits, senderLimits imapIngressLimits) error {
	messageSize = normalizedIMAPMessageSize(messageSize)
	mailboxUsageKey := "mailbox:" + mailboxKey
	senderHash := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(sender))))
	senderUsageKey := fmt.Sprintf("sender:%s:%x", mailboxKey, senderHash)

	e.imapIngressMu.Lock()
	defer e.imapIngressMu.Unlock()
	if e.imapIngressEvents == nil {
		e.imapIngressEvents = make(map[string][]imapIngressEvent)
	}
	e.sweepIMAPIngressEventsLocked(now)

	if err := checkIMAPIngressUsage(e.imapIngressEvents[mailboxUsageKey], now, messageSize, inboxLimits, "mailbox"); err != nil {
		return err
	}
	if err := checkIMAPIngressUsage(e.imapIngressEvents[senderUsageKey], now, messageSize, senderLimits, "sender"); err != nil {
		return err
	}

	event := imapIngressEvent{at: now, size: messageSize}
	e.imapIngressEvents[mailboxUsageKey] = append(e.imapIngressEvents[mailboxUsageKey], event)
	e.imapIngressEvents[senderUsageKey] = append(e.imapIngressEvents[senderUsageKey], event)
	return nil
}

func checkIMAPIngressUsage(events []imapIngressEvent, now time.Time, messageSize int64, limits imapIngressLimits, scope string) error {
	hourCutoff := now.Add(-time.Hour)
	dayCutoff := now.Add(-24 * time.Hour)
	var hourMessages, dayMessages int
	var hourBytes, dayBytes int64
	for _, event := range events {
		if event.at.After(dayCutoff) {
			dayMessages++
			dayBytes += event.size
		}
		if event.at.After(hourCutoff) {
			hourMessages++
			hourBytes += event.size
		}
	}

	if limits.hourMessages <= 0 || hourMessages >= limits.hourMessages {
		return fmt.Errorf("%w: %s hourly message count", errIMAPIngressBudget, scope)
	}
	if exceedsIMAPByteLimit(hourBytes, messageSize, limits.hourBytes) {
		return fmt.Errorf("%w: %s hourly bytes", errIMAPIngressBudget, scope)
	}
	if limits.dayMessages <= 0 || dayMessages >= limits.dayMessages {
		return fmt.Errorf("%w: %s daily message count", errIMAPIngressBudget, scope)
	}
	if exceedsIMAPByteLimit(dayBytes, messageSize, limits.dayBytes) {
		return fmt.Errorf("%w: %s daily bytes", errIMAPIngressBudget, scope)
	}
	return nil
}

func exceedsIMAPByteLimit(used, additional, limit int64) bool {
	return limit <= 0 || additional > limit || used > limit-additional
}

func (e *Email) sweepIMAPIngressEventsLocked(now time.Time) {
	if !e.imapIngressLastSweep.IsZero() && !now.Before(e.imapIngressLastSweep) && now.Sub(e.imapIngressLastSweep) < imapIngressSweepInterval {
		return
	}
	cutoff := now.Add(-24 * time.Hour)
	for key, events := range e.imapIngressEvents {
		kept := events[:0]
		for _, event := range events {
			if event.at.After(cutoff) {
				kept = append(kept, event)
			}
		}
		clear(events[len(kept):])
		if len(kept) == 0 {
			delete(e.imapIngressEvents, key)
			continue
		}
		e.imapIngressEvents[key] = kept
	}
	e.imapIngressLastSweep = now
}

func imapCursorKey(cfg imodels.IMAPConfig) string {
	return fmt.Sprintf("%s:%d/%s/%s", strings.ToLower(strings.TrimSpace(cfg.Host)), cfg.Port, strings.ToLower(strings.TrimSpace(cfg.Username)), strings.TrimSpace(cfg.Mailbox))
}

func (e *Email) getIMAPCursor(key string) uint32 {
	e.imapCursorMu.Lock()
	defer e.imapCursorMu.Unlock()
	return e.imapCursors[key]
}

func (e *Email) setIMAPCursor(key string, cursor uint32) {
	e.imapCursorMu.Lock()
	defer e.imapCursorMu.Unlock()
	if e.imapCursors == nil {
		e.imapCursors = make(map[string]uint32)
	}
	e.imapCursors[key] = cursor
}

// fetchAndProcessMessages fetches and processes messages based on the search results.
func (e *Email) fetchAndProcessMessages(ctx context.Context, client *imapclient.Client, searchResults *imap.SearchData, inboxID int, retryCursor, nextCursor uint32, mailboxKey string) (uint32, error) {
	page := boundedSearchSequencePage(searchResults, 0, maxIMAPMessagesPerPoll)
	if len(page.nums) == 0 {
		// No results found
		e.lo.Debug("no messages found in search results", "inbox_id", inboxID)
		return nextCursor, nil
	}
	e.lo.Debug("processing bounded IMAP page", "count", len(page.nums), "first", page.nums[0], "last", page.nums[len(page.nums)-1], "inbox_id", inboxID)

	// Fetch envelope and headers needed for auto-reply detection.
	fetchOptions := &imap.FetchOptions{
		Envelope:   true,
		RFC822Size: true,
		BodySection: []*imap.FetchItemBodySection{
			{
				Specifier: imap.PartSpecifierHeader,
				Partial: &imap.SectionPartial{
					Offset: 0,
					Size:   maxIMAPHeaderBytes,
				},
				HeaderFields: []string{
					headerAutoSubmitted,
					headerAutoreply,
					headerLibredeskLoopPrevention,
					headerMessageID,
				},
			},
		},
	}

	// Collect messages to process later.
	type msgData struct {
		env                *imap.Envelope
		seqNum             uint32
		autoReply          bool
		isLoop             bool
		extractedMessageID string
		size               int64
	}
	var messages []msgData

	fetchCmd := client.Fetch(page.set, fetchOptions)
	fetchClosed := false
	defer func() {
		if !fetchClosed {
			_ = fetchCmd.Close()
		}
	}()

	// Extract the inbox email address.
	inboxEmail, err := stringutil.ExtractEmail(e.FromAddress())
	if err != nil {
		e.lo.Error("failed to extract email address from the 'From' header", "error", err)
		return retryCursor, fmt.Errorf("failed to extract email address from 'From' header: %w", err)
	}
	if inboxEmail == "" {
		e.lo.Error("inbox email address is empty, cannot process messages", "inbox_id", e.Identifier())
		return retryCursor, fmt.Errorf("inbox (%d) email address is empty, cannot process messages", e.Identifier())
	}
	for {
		// Check for context cancellation before fetching the next message.
		select {
		case <-ctx.Done():
			return retryCursor, ctx.Err()
		default:
		}

		// Fetch the next message.
		msg := fetchCmd.Next()
		if msg == nil {
			// No more messages to process.
			break
		}

		var (
			env                *imap.Envelope
			autoReply          bool
			isLoop             bool
			extractedMessageID string
			size               int64
		)
		// Process all fetch items for the current message.
		for {
			// Check for context cancellation before processing the next item.
			select {
			case <-ctx.Done():
				return retryCursor, ctx.Err()
			default:
			}

			// Fetch the next item in the message.
			item := msg.Next()
			if item == nil {
				// No message items left to process.
				break
			}

			// Body section.
			if bs, ok := item.(imapclient.FetchItemDataBodySection); ok && bs.Literal != nil {
				var parsedEnvelope *enmime.Envelope
				err := guardEmailProcessing(msg.SeqNum, func() error {
					var parseErr error
					parsedEnvelope, parseErr = enmime.ReadEnvelope(bs.Literal)
					return parseErr
				})
				if err != nil {
					e.lo.Error("error reading envelope", "error", err)
					continue
				}
				if parsedEnvelope == nil {
					e.lo.Warn("skipping message with empty parsed envelope", "seq_num", msg.SeqNum, "inbox_id", e.Identifier())
					continue
				}
				if isAutoReply(parsedEnvelope) {
					autoReply = true
				}
				if isLoopMessage(parsedEnvelope, inboxEmail) {
					isLoop = true
				}

				// Extract Message-Id from raw headers as fallback for problematic Message IDs
				extractedMessageID = extractMessageIDFromHeaders(parsedEnvelope)
			}

			// Envelope.
			if ed, ok := item.(imapclient.FetchItemDataEnvelope); ok {
				env = ed.Envelope
			}
			if sd, ok := item.(imapclient.FetchItemDataRFC822Size); ok {
				size = sd.Size
			}
		}

		// Skip if we couldn't get the envelope.
		if env == nil {
			e.lo.Warn("skipping message without envelope", "seq_num", msg.SeqNum, "inbox_id", e.Identifier())
			continue
		}
		if size > maxRawEmailBytes {
			e.lo.Warn("skipping oversized email", "seq_num", msg.SeqNum, "inbox_id", e.Identifier(), "size", size, "max_size", maxRawEmailBytes)
			continue
		}

		messages = append(messages, msgData{env: env, seqNum: msg.SeqNum, autoReply: autoReply, isLoop: isLoop, extractedMessageID: extractedMessageID, size: size})
	}
	if err := fetchCmd.Close(); err != nil {
		fetchClosed = true
		return retryCursor, fmt.Errorf("fetching IMAP message headers: %w", err)
	}
	fetchClosed = true

	budget := newIMAPPollBudget(maxIMAPBytesPerPoll)

	// Now process the bounded collection of messages.
	for _, msgData := range messages {
		// Check for context cancellation before processing each message.
		select {
		case <-ctx.Done():
			return msgData.seqNum, ctx.Err()
		default:
		}

		// Skip if this is an auto-reply message.
		if msgData.autoReply {
			e.lo.Info("skipping auto-reply message", "subject", msgData.env.Subject, "message_id", msgData.env.MessageID)
			continue
		}

		// Skip if this message is a loop prevention message.
		if msgData.isLoop {
			e.lo.Info("skipping message with loop prevention header", "subject", msgData.env.Subject, "message_id", msgData.env.MessageID)
			continue
		}

		// Process the envelope.
		err := guardEmailProcessing(msgData.seqNum, func() error {
			return e.processEnvelope(ctx, client, msgData.env, msgData.seqNum, inboxID, msgData.extractedMessageID, msgData.size, mailboxKey, &budget)
		})
		if errors.Is(err, errIMAPPollByteBudget) {
			e.lo.Info("IMAP poll byte budget exhausted", "inbox_id", inboxID, "next_seq_num", msgData.seqNum, "byte_budget", maxIMAPBytesPerPoll)
			return msgData.seqNum, nil
		}
		if errors.Is(err, errIMAPIngressBudget) {
			e.lo.Warn("IMAP rolling ingress budget exhausted", "inbox_id", inboxID, "seq_num", msgData.seqNum, "error", err)
			continue
		}
		if err != nil && err != context.Canceled {
			e.lo.Error("error processing envelope", "error", err)
		}
		// Advance past per-message failures. They are retried when the bounded
		// cursor wraps, so one malformed message cannot permanently stall mail.
	}

	return nextCursor, nil
}

// processEnvelope processes a single email envelope.
func (e *Email) processEnvelope(ctx context.Context, client *imapclient.Client, env *imap.Envelope, seqNum uint32, inboxID int, extractedMessageID string, messageSize int64, mailboxKey string, budget *imapPollBudget) error {
	if len(env.From) == 0 {
		e.lo.Warn("no sender received for email", "message_id", env.MessageID)
		return nil
	}
	var fromAddress = strings.ToLower(env.From[0].Addr())

	// Determine final Message ID - prefer IMAP-parsed, fallback to raw header extraction
	messageID := env.MessageID
	if messageID == "" {
		messageID = extractedMessageID
		if messageID != "" {
			e.lo.Debug("using raw header Message-ID as fallback for malformed ID", "message_id", messageID, "subject", env.Subject, "from", fromAddress)
		}
	}

	// Drop message if we still don't have a valid Message ID
	if messageID == "" {
		e.lo.Error("dropping message: no valid Message-ID found in IMAP parsing or raw headers", "subject", env.Subject, "from", fromAddress)
		return nil
	}

	// Check if the message already exists in the database; if it does, ignore it.
	exists, err := e.messageStore.MessageExists(messageID)
	if err != nil {
		e.lo.Error("error checking if message exists", "message_id", messageID)
		return fmt.Errorf("checking if message exists in DB: %w", err)
	}
	if exists {
		return nil
	}

	// Check if any contact with this email is blocked, if so, ignore the message.
	if blocked, err := e.userStore.IsEmailBlocked(fromAddress); err != nil {
		e.lo.Error("error checking if email is blocked", "email", fromAddress, "error", err)
		return fmt.Errorf("checking if email is blocked: %w", err)
	} else if blocked {
		e.lo.Info("contact email is blocked dropping incoming email", "email", fromAddress)
		return nil
	}
	if budget == nil || !budget.canReserve(messageSize) {
		return errIMAPPollByteBudget
	}
	if err := e.reserveIMAPIngress(mailboxKey, fromAddress, messageSize, time.Now()); err != nil {
		return err
	}
	if !budget.reserve(messageSize) {
		return errIMAPPollByteBudget
	}

	e.lo.Debug("processing new incoming message", "message_id", messageID, "subject", env.Subject, "from", fromAddress, "inbox_id", inboxID)

	// Make contact.
	firstName, lastName := getContactName(env.From[0])
	contact := models.IncomingContact{
		FirstName: firstName,
		LastName:  lastName,
		Email:     null.StringFrom(fromAddress),
	}

	// Lowercase and set the `to`, `cc`, `from` and `bcc` addresses in message meta.
	var ccAddr = make([]string, 0, len(env.Cc))
	var toAddr = make([]string, 0, len(env.To))
	var bccAddr = make([]string, 0, len(env.Bcc))
	var fromAddr = make([]string, 0, len(env.From))
	for _, cc := range env.Cc {
		if cc.Addr() != "" {
			ccAddr = append(ccAddr, strings.ToLower(cc.Addr()))
		}
	}
	for _, to := range env.To {
		if to.Addr() != "" {
			toAddr = append(toAddr, strings.ToLower(to.Addr()))
		}
	}
	for _, bcc := range env.Bcc {
		if bcc.Addr() != "" {
			bccAddr = append(bccAddr, strings.ToLower(bcc.Addr()))
		}
	}
	for _, from := range env.From {
		if from.Addr() != "" {
			fromAddr = append(fromAddr, strings.ToLower(from.Addr()))
		}
	}

	meta, err := json.Marshal(map[string]interface{}{
		"from":    fromAddr,
		"cc":      ccAddr,
		"bcc":     bccAddr,
		"to":      toAddr,
		"subject": env.Subject,
	})
	if err != nil {
		e.lo.Error("error marshalling meta", "error", err)
		return fmt.Errorf("marshalling meta: %w", err)
	}
	incomingMsg := models.IncomingMessage{
		Channel:  ChannelEmail,
		InboxID:  inboxID,
		Contact:  contact,
		Subject:  env.Subject,
		SourceID: null.StringFrom(messageID),
		Meta:     meta,
	}

	// Fetch full message body.
	fetchOptions := &imap.FetchOptions{
		BodySection: []*imap.FetchItemBodySection{{}},
	}
	seqSet := imap.SeqSet{}
	seqSet.AddNum(seqNum)

	fullFetchCmd := client.Fetch(seqSet, fetchOptions)
	fullFetchClosed := false
	defer func() {
		if !fullFetchClosed {
			_ = fullFetchCmd.Close()
		}
	}()
	fullMsg := fullFetchCmd.Next()
	if fullMsg == nil {
		err := fullFetchCmd.Close()
		fullFetchClosed = true
		if err != nil {
			return fmt.Errorf("fetching full IMAP message: %w", err)
		}
		return nil
	}

	// Fetch full message.
	for {
		// Check for context cancellation before processing the next item.
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		fullFetchItem := fullMsg.Next()
		if fullFetchItem == nil {
			return nil
		}

		if fullItem, ok := fullFetchItem.(imapclient.FetchItemDataBodySection); ok {
			e.lo.Debug("fetching full message body", "message_id", messageID)
			return e.processFullMessage(fullItem, incomingMsg)
		}
	}
}

// processFullMessage processes the full message and enqueues it for inserting into the database.
func (e *Email) processFullMessage(item imapclient.FetchItemDataBodySection, incomingMsg models.IncomingMessage) error {
	envelope, err := readEnvelopeWithLimit(item.Literal, maxRawEmailBytes)
	if err != nil {
		e.lo.Error("error parsing email envelope", "error", err, "message_id", incomingMsg.SourceID.String)
		if envelope != nil {
			for _, envelopeErr := range envelope.Errors {
				e.lo.Error("error parsing email envelope", "error", envelopeErr.Error(), "message_id", incomingMsg.SourceID.String)
			}
		}
		return fmt.Errorf("parsing email envelope: %w", err)
	}
	if err := validateEnvelopeResources(envelope); err != nil {
		return err
	}
	if len(incomingMsg.Subject) > maxEmailSubjectBytes {
		return fmt.Errorf("email subject exceeds %d bytes", maxEmailSubjectBytes)
	}

	// Log any envelope errors.
	for _, err := range envelope.Errors {
		e.lo.Error("error parsing email envelope", "error", err.Error(), "message_id", incomingMsg.SourceID.String)
	}

	// Extract all HTML content by traversing the tree
	var allHTML strings.Builder
	if envelope.Root != nil {
		htmlParts := extractAllHTMLParts(envelope.Root)
		if len(htmlParts) > 0 {
			allHTML.WriteString("<div>")
			for _, part := range htmlParts {
				allHTML.WriteString(part)
			}
			allHTML.WriteString("</div>")
		}
	}

	// Set message content - prioritize combined HTML
	if allHTML.Len() > 0 {
		incomingMsg.Content = allHTML.String()
		incomingMsg.ContentType = models.ContentTypeHTML
	} else if len(envelope.HTML) > 0 {
		incomingMsg.Content = envelope.HTML
		incomingMsg.ContentType = models.ContentTypeHTML
	} else if len(envelope.Text) > 0 {
		incomingMsg.Content = envelope.Text
		incomingMsg.ContentType = models.ContentTypeText
	}
	if len(incomingMsg.Content) > maxEmailBodyBytes {
		return fmt.Errorf("email body exceeds %d bytes", maxEmailBodyBytes)
	}

	// Clean headers
	inReplyTo := strings.ReplaceAll(strings.ReplaceAll(envelope.GetHeader("In-Reply-To"), "<", ""), ">", "")
	references := strings.Fields(envelope.GetHeader("References"))
	for i, ref := range references {
		references[i] = strings.Trim(strings.TrimSpace(ref), " <>")
	}

	incomingMsg.InReplyTo = inReplyTo
	incomingMsg.References = references

	// Extract conversation UUID from plus-addressed recipient (e.g., inbox+conv-{uuid}@domain)
	incomingMsg.ConversationUUIDFromReplyTo = extractConversationUUIDFromRecipient(envelope)
	if incomingMsg.ConversationUUIDFromReplyTo != "" {
		e.lo.Debug("extracted conversation UUID from plus-addressed recipient",
			"conversation_uuid", incomingMsg.ConversationUUIDFromReplyTo,
			"message_id", incomingMsg.SourceID.String)
	}

	// Process attachments
	for _, att := range envelope.Attachments {
		incomingMsg.Attachments = append(incomingMsg.Attachments, attachment.Attachment{
			Name:        att.FileName,
			Content:     att.Content,
			ContentType: att.ContentType,
			ContentID:   att.ContentID,
			Size:        len(att.Content),
			Disposition: attachment.DispositionAttachment,
		})
	}

	// Process inlines - treat ones without ContentID as regular attachments
	for _, inline := range envelope.Inlines {
		disposition := attachment.DispositionInline
		if inline.ContentID == "" {
			disposition = attachment.DispositionAttachment
		}

		incomingMsg.Attachments = append(incomingMsg.Attachments, attachment.Attachment{
			Name:        inline.FileName,
			Content:     inline.Content,
			ContentType: inline.ContentType,
			ContentID:   inline.ContentID,
			Size:        len(inline.Content),
			Disposition: disposition,
		})
	}

	incomingMsg.Content = stringutil.SanitizeUTF8(incomingMsg.Content)
	incomingMsg.Subject = stringutil.SanitizeUTF8(incomingMsg.Subject)
	incomingMsg.Contact.FirstName = stringutil.SanitizeUTF8(incomingMsg.Contact.FirstName)
	incomingMsg.Contact.LastName = stringutil.SanitizeUTF8(incomingMsg.Contact.LastName)

	e.lo.Debug("enqueuing incoming email message", "message_id", incomingMsg.SourceID.String,
		"attachments", len(envelope.Attachments), "inline_attachments", len(envelope.Inlines))

	if err := e.messageStore.EnqueueIncoming(incomingMsg); err != nil {
		return err
	}
	return nil
}

func readEnvelopeWithLimit(r io.Reader, limit int64) (*enmime.Envelope, error) {
	if r == nil {
		return nil, fmt.Errorf("email body is missing")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("email size limit must be positive")
	}
	raw, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, fmt.Errorf("reading email body: %w", err)
	}
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("email exceeds %d bytes", limit)
	}
	return enmime.ReadEnvelope(bytes.NewReader(raw))
}

func validateEnvelopeResources(envelope *enmime.Envelope) error {
	if envelope == nil {
		return fmt.Errorf("email envelope is missing")
	}
	count := len(envelope.Attachments) + len(envelope.Inlines)
	if count > maxEmailAttachments {
		return fmt.Errorf("email has too many attachments: %d", count)
	}
	total := 0
	check := func(size int) error {
		if size > maxEmailAttachment {
			return fmt.Errorf("email attachment exceeds %d bytes", maxEmailAttachment)
		}
		if total > maxEmailAttachmentsTotal-size {
			return fmt.Errorf("email attachments exceed %d bytes", maxEmailAttachmentsTotal)
		}
		total += size
		return nil
	}
	for _, att := range envelope.Attachments {
		if err := check(len(att.Content)); err != nil {
			return err
		}
	}
	for _, inline := range envelope.Inlines {
		if err := check(len(inline.Content)); err != nil {
			return err
		}
	}
	return nil
}

func guardEmailProcessing(seqNum uint32, process func() error) (err error) {
	defer func() {
		if recover() != nil {
			err = fmt.Errorf("panic while processing email sequence %d", seqNum)
		}
	}()
	return process()
}

// getContactName extracts the contact's first and last name from the IMAP address.
func getContactName(imapAddr imap.Address) (string, string) {
	from := strings.TrimSpace(imapAddr.Name)
	names := strings.Fields(from)
	if len(names) == 0 {
		return imapAddr.Host, ""
	}
	if len(names) == 1 {
		return names[0], ""
	}
	return names[0], names[1]
}

// isAutoReply checks if a given email envelope indicates an auto-reply message.
func isAutoReply(envelope *enmime.Envelope) bool {
	if as := strings.ToLower(strings.TrimSpace(envelope.GetHeader("Auto-Submitted"))); as != "" && as != "no" {
		return true
	}
	if strings.TrimSpace(envelope.GetHeader("X-Autoreply")) != "" {
		return true
	}
	return false
}

// isLoopMessage returns true if the email is a loop prevention message. i.e., it has the `X-Libredesk-Loop-Prevention` header with the inbox email address.
func isLoopMessage(envelope *enmime.Envelope, inboxEmailaddress string) bool {
	loopHeader := envelope.GetHeader(headerLibredeskLoopPrevention)
	if loopHeader == "" {
		return false
	}
	return strings.EqualFold(loopHeader, inboxEmailaddress)
}

// extractAllHTMLParts extracts all HTML parts from the given enmime part by traversing the tree.
func extractAllHTMLParts(part *enmime.Part) []string {
	var htmlParts []string

	// Check current part
	if strings.HasPrefix(part.ContentType, "text/html") && len(part.Content) > 0 {
		htmlParts = append(htmlParts, string(part.Content))
	}

	// Process children recursively
	for child := part.FirstChild; child != nil; child = child.NextSibling {
		childParts := extractAllHTMLParts(child)
		htmlParts = append(htmlParts, childParts...)
	}

	return htmlParts
}

// extractUUIDFromReplyAddress extracts a UUID from the reply address if present.
// The UUID is expected to be in the format "username+<UUID>@domain" within the email address.
// Returns an empty string if the UUID is not found or invalid.
func (e *Email) extractUUIDFromReplyAddress(address string) string {
	// Remove angle brackets if present
	address = strings.Trim(address, "<>")

	// Check if it contains +
	if !strings.Contains(address, "+") {
		return ""
	}

	// Extract the part between + and @
	parts := strings.Split(address, "@")
	if len(parts) != 2 {
		return ""
	}

	// Get the UUID
	uuid := strings.SplitN(parts[0], "+", 2)[1]
	if uuid == "" {
		return ""
	}

	// Validate UUID format (36 chars with hyphens at specific positions)
	if len(uuid) == 36 &&
		uuid[8] == '-' &&
		uuid[13] == '-' &&
		uuid[18] == '-' &&
		uuid[23] == '-' {
		return uuid
	}

	return ""
}

// extractMessageIDFromHeaders extracts and cleans the Message-ID from email headers.
// This function handles problematic Message IDs by extracting them from raw headers
// and cleaning them of angle brackets and whitespace.
func extractMessageIDFromHeaders(envelope *enmime.Envelope) string {
	if rawMessageID := envelope.GetHeader(headerMessageID); rawMessageID != "" {
		return strings.TrimSpace(strings.Trim(rawMessageID, "<>"))
	}
	return ""
}

// extractConversationUUIDFromRecipient extracts conversation UUID from plus-addressed recipient.
// Checks Delivered-To, X-Original-To, and To headers for plus-addressing pattern.
// e.g., support+conv-abc123-def456@company.com → abc123-def456
func extractConversationUUIDFromRecipient(envelope *enmime.Envelope) string {
	headers := []string{"Delivered-To", "X-Original-To", "To"}
	for _, h := range headers {
		addr := envelope.GetHeader(h)
		if uuid := stringutil.ExtractConvUUID(addr); uuid != "" {
			return uuid
		}
	}
	return ""
}
