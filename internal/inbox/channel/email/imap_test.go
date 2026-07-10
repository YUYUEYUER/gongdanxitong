package email

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-message/mail"
	"github.com/jhillyerd/enmime"
)

func TestBoundedSearchSequencePageCapsLargeRange(t *testing.T) {
	var all imap.SeqSet
	all.AddRange(1, 1_000_000)

	page := boundedSearchSequencePage(&imap.SearchData{All: all}, 0, maxIMAPMessagesPerPoll)
	if len(page.nums) != maxIMAPMessagesPerPoll {
		t.Fatalf("got %d sequence numbers, want %d", len(page.nums), maxIMAPMessagesPerPoll)
	}
	if page.nums[0] != 1 || page.nums[len(page.nums)-1] != 100 || page.nextCursor != 101 {
		t.Fatalf("unexpected bounded page: first=%d last=%d next=%d", page.nums[0], page.nums[len(page.nums)-1], page.nextCursor)
	}
	if got := page.set.String(); got != "1:100" {
		t.Fatalf("unexpected sequence set %q", got)
	}
}

func TestBoundedSearchSequencePageResumesAndWrapsSparseResults(t *testing.T) {
	var all imap.SeqSet
	all.AddRange(3, 5)
	all.AddRange(10, 12)
	results := &imap.SearchData{All: all}

	page := boundedSearchSequencePage(results, 4, 3)
	if !reflect.DeepEqual(page.nums, []uint32{4, 5, 10}) || page.nextCursor != 11 {
		t.Fatalf("unexpected resumed page: nums=%v next=%d", page.nums, page.nextCursor)
	}

	wrapped := boundedSearchSequencePage(results, 20, 2)
	if !reflect.DeepEqual(wrapped.nums, []uint32{3, 4}) || wrapped.nextCursor != 5 {
		t.Fatalf("unexpected wrapped page: nums=%v next=%d", wrapped.nums, wrapped.nextCursor)
	}
}

func TestStandardSearchCriteriaIsPreBoundedForMillionMessageMailbox(t *testing.T) {
	window := boundedIMAPSearchWindow(1_000_000, 0, maxIMAPMessagesPerPoll, maxIMAPHeadMessagesPoll)
	if window.count != maxIMAPMessagesPerPoll {
		t.Fatalf("search window contains %d sequence numbers, want %d", window.count, maxIMAPMessagesPerPoll)
	}
	if got := window.set.String(); got != "999901:1000000" {
		t.Fatalf("unexpected initial search window %q", got)
	}
	if window.nextCursor != 999900 {
		t.Fatalf("unexpected next backlog cursor %d", window.nextCursor)
	}

	since := time.Unix(1_700_000_000, 0)
	criteria := boundedIMAPSearchCriteria(since, window.set)
	if !criteria.Since.Equal(since) || len(criteria.SeqNum) != 1 {
		t.Fatalf("standard SEARCH criteria is not sequence bounded: %+v", criteria)
	}
	nums, ok := criteria.SeqNum[0].Nums()
	if !ok || len(nums) != maxIMAPMessagesPerPoll {
		t.Fatalf("standard SEARCH can return %d sequence matches, want at most %d", len(nums), maxIMAPMessagesPerPoll)
	}
}

func TestBoundedIMAPSearchWindowKeepsHeadAndRotatesBacklog(t *testing.T) {
	window := boundedIMAPSearchWindow(1_000_000, 999900, maxIMAPMessagesPerPoll, maxIMAPHeadMessagesPoll)
	if got := window.set.String(); got != "999851:999900,999951:1000000" {
		t.Fatalf("unexpected rotating search window %q", got)
	}
	if window.count != maxIMAPMessagesPerPoll || window.nextCursor != 999850 {
		t.Fatalf("unexpected rotating window count=%d next=%d", window.count, window.nextCursor)
	}
}

func TestBoundedIMAPSearchWindowHandlesSmallOrShrunkMailbox(t *testing.T) {
	small := boundedIMAPSearchWindow(42, 0, maxIMAPMessagesPerPoll, maxIMAPHeadMessagesPoll)
	if small.set.String() != "1:42" || small.count != 42 || small.nextCursor != 0 {
		t.Fatalf("unexpected small mailbox window: set=%s count=%d next=%d", small.set.String(), small.count, small.nextCursor)
	}

	shrunk := boundedIMAPSearchWindow(100, 999900, maxIMAPMessagesPerPoll, maxIMAPHeadMessagesPoll)
	if shrunk.set.String() != "1:100" || shrunk.count != 100 {
		t.Fatalf("stale cursor was not reset after mailbox shrink: set=%s count=%d", shrunk.set.String(), shrunk.count)
	}
}

func TestIMAPPollBudgetDoesNotAdvanceOnExhaustion(t *testing.T) {
	budget := newIMAPPollBudget(100)
	if !budget.reserve(60) {
		t.Fatal("expected first reservation to fit")
	}
	if budget.reserve(41) {
		t.Fatal("expected oversized reservation to be rejected")
	}
	if budget.remaining != 40 {
		t.Fatalf("rejected reservation changed budget to %d", budget.remaining)
	}
	if !budget.reserve(40) || budget.remaining != 0 {
		t.Fatalf("expected exact remaining budget to fit, remaining=%d", budget.remaining)
	}
}

func TestIMAPPollBudgetChargesUnknownSizeConservatively(t *testing.T) {
	budget := newIMAPPollBudget(maxRawEmailBytes)
	if !budget.reserve(0) || budget.remaining != 0 {
		t.Fatalf("unknown message size should reserve max raw size, remaining=%d", budget.remaining)
	}
}

func TestIMAPIngressBudgetAggregatesMailboxAcrossSenders(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	inboxLimits := imapIngressLimits{hourMessages: 2, hourBytes: 1_000, dayMessages: 10, dayBytes: 10_000}
	senderLimits := imapIngressLimits{hourMessages: 10, hourBytes: 1_000, dayMessages: 10, dayBytes: 10_000}
	e := &Email{}

	if err := e.reserveIMAPIngressWithLimits("mailbox", "a@example.com", 10, now, inboxLimits, senderLimits); err != nil {
		t.Fatal(err)
	}
	if err := e.reserveIMAPIngressWithLimits("mailbox", "b@example.com", 10, now, inboxLimits, senderLimits); err != nil {
		t.Fatal(err)
	}
	err := e.reserveIMAPIngressWithLimits("mailbox", "c@example.com", 10, now, inboxLimits, senderLimits)
	if !errors.Is(err, errIMAPIngressBudget) || !strings.Contains(err.Error(), "mailbox hourly message count") {
		t.Fatalf("expected mailbox hourly count limit, got %v", err)
	}
}

func TestIMAPIngressBudgetSenderFailureDoesNotConsumeMailboxBudget(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	inboxLimits := imapIngressLimits{hourMessages: 2, hourBytes: 1_000, dayMessages: 10, dayBytes: 10_000}
	senderLimits := imapIngressLimits{hourMessages: 1, hourBytes: 1_000, dayMessages: 10, dayBytes: 10_000}
	e := &Email{}

	if err := e.reserveIMAPIngressWithLimits("mailbox", "flood@example.com", 10, now, inboxLimits, senderLimits); err != nil {
		t.Fatal(err)
	}
	if err := e.reserveIMAPIngressWithLimits("mailbox", "flood@example.com", 10, now, inboxLimits, senderLimits); !errors.Is(err, errIMAPIngressBudget) {
		t.Fatalf("expected sender limit, got %v", err)
	}
	if err := e.reserveIMAPIngressWithLimits("mailbox", "legitimate@example.com", 10, now, inboxLimits, senderLimits); err != nil {
		t.Fatalf("failed sender reservation consumed mailbox budget: %v", err)
	}
}

func TestIMAPIngressBudgetUsesRollingHourlyAndDailyWindows(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	inboxLimits := imapIngressLimits{hourMessages: 100, hourBytes: 100_000, dayMessages: 100, dayBytes: 100_000}
	senderLimits := imapIngressLimits{hourMessages: 2, hourBytes: 100_000, dayMessages: 3, dayBytes: 100_000}
	e := &Email{}

	for i := 0; i < 2; i++ {
		if err := e.reserveIMAPIngressWithLimits("mailbox", "sender@example.com", 10, now, inboxLimits, senderLimits); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.reserveIMAPIngressWithLimits("mailbox", "sender@example.com", 10, now.Add(30*time.Minute), inboxLimits, senderLimits); !errors.Is(err, errIMAPIngressBudget) {
		t.Fatalf("expected rolling hourly limit, got %v", err)
	}
	if err := e.reserveIMAPIngressWithLimits("mailbox", "sender@example.com", 10, now.Add(61*time.Minute), inboxLimits, senderLimits); err != nil {
		t.Fatalf("hourly window did not roll forward: %v", err)
	}
	if err := e.reserveIMAPIngressWithLimits("mailbox", "sender@example.com", 10, now.Add(2*time.Hour), inboxLimits, senderLimits); !errors.Is(err, errIMAPIngressBudget) || !strings.Contains(err.Error(), "daily message count") {
		t.Fatalf("expected rolling daily limit, got %v", err)
	}
	if err := e.reserveIMAPIngressWithLimits("mailbox", "sender@example.com", 10, now.Add(24*time.Hour+time.Second), inboxLimits, senderLimits); err != nil {
		t.Fatalf("daily window did not roll forward: %v", err)
	}
}

func TestIMAPIngressBudgetEnforcesBytesWithoutChargingRejectedAttempt(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	inboxLimits := imapIngressLimits{hourMessages: 10, hourBytes: 1_000, dayMessages: 10, dayBytes: 10_000}
	senderLimits := imapIngressLimits{hourMessages: 10, hourBytes: 100, dayMessages: 10, dayBytes: 1_000}
	e := &Email{}

	if err := e.reserveIMAPIngressWithLimits("mailbox", "sender@example.com", 60, now, inboxLimits, senderLimits); err != nil {
		t.Fatal(err)
	}
	if err := e.reserveIMAPIngressWithLimits("mailbox", "sender@example.com", 41, now, inboxLimits, senderLimits); !errors.Is(err, errIMAPIngressBudget) {
		t.Fatalf("expected sender byte limit, got %v", err)
	}
	if err := e.reserveIMAPIngressWithLimits("mailbox", "sender@example.com", 40, now, inboxLimits, senderLimits); err != nil {
		t.Fatalf("rejected attempt consumed byte budget: %v", err)
	}
}

func TestIMAPIngressBudgetEnforcesRollingDailyBytes(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	inboxLimits := imapIngressLimits{hourMessages: 10, hourBytes: 10_000, dayMessages: 10, dayBytes: 10_000}
	senderLimits := imapIngressLimits{hourMessages: 10, hourBytes: 1_000, dayMessages: 10, dayBytes: 100}
	e := &Email{}

	if err := e.reserveIMAPIngressWithLimits("mailbox", "sender@example.com", 60, now, inboxLimits, senderLimits); err != nil {
		t.Fatal(err)
	}
	err := e.reserveIMAPIngressWithLimits("mailbox", "sender@example.com", 41, now.Add(2*time.Hour), inboxLimits, senderLimits)
	if !errors.Is(err, errIMAPIngressBudget) || !strings.Contains(err.Error(), "sender daily bytes") {
		t.Fatalf("expected sender daily byte limit, got %v", err)
	}
	if err := e.reserveIMAPIngressWithLimits("mailbox", "sender@example.com", 40, now.Add(2*time.Hour), inboxLimits, senderLimits); err != nil {
		t.Fatalf("rejected daily reservation consumed budget: %v", err)
	}
}

func TestIMAPIngressBudgetReservationsAreAtomic(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	inboxLimits := imapIngressLimits{hourMessages: 10, hourBytes: 10_000, dayMessages: 10, dayBytes: 10_000}
	senderLimits := imapIngressLimits{hourMessages: 100, hourBytes: 10_000, dayMessages: 100, dayBytes: 10_000}
	e := &Email{}

	results := make(chan error, 100)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(senderID int) {
			defer wg.Done()
			results <- e.reserveIMAPIngressWithLimits("mailbox", fmt.Sprintf("sender-%d@example.com", senderID), 1, now, inboxLimits, senderLimits)
		}(i)
	}
	wg.Wait()
	close(results)

	accepted := 0
	for err := range results {
		if err == nil {
			accepted++
		} else if !errors.Is(err, errIMAPIngressBudget) {
			t.Fatalf("unexpected reservation error: %v", err)
		}
	}
	if accepted != inboxLimits.hourMessages {
		t.Fatalf("accepted %d concurrent reservations, want %d", accepted, inboxLimits.hourMessages)
	}
}

func TestIMAPIngressBudgetSweepsExpiredSenderKeys(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	limits := imapIngressLimits{hourMessages: 10, hourBytes: 1_000, dayMessages: 10, dayBytes: 10_000}
	e := &Email{}

	if err := e.reserveIMAPIngressWithLimits("mailbox", "expired@example.com", 10, now, limits, limits); err != nil {
		t.Fatal(err)
	}
	if err := e.reserveIMAPIngressWithLimits("mailbox", "current@example.com", 10, now.Add(25*time.Hour), limits, limits); err != nil {
		t.Fatal(err)
	}
	if len(e.imapIngressEvents) != 2 {
		t.Fatalf("expired sender key was not swept, usage keys=%d", len(e.imapIngressEvents))
	}
	for key := range e.imapIngressEvents {
		if strings.Contains(key, "expired@example.com") || strings.Contains(key, "current@example.com") {
			t.Fatalf("raw sender address retained in budget key %q", key)
		}
	}
}

func TestDefaultIMAPIngressLimitsAreConservativeAndUsable(t *testing.T) {
	inbox := defaultIMAPInboxIngressLimits()
	sender := defaultIMAPSenderIngressLimits()
	if sender.hourMessages <= 0 || sender.hourMessages >= inbox.hourMessages {
		t.Fatalf("unexpected sender hourly count default: sender=%d inbox=%d", sender.hourMessages, inbox.hourMessages)
	}
	if sender.dayMessages < sender.hourMessages || sender.dayMessages >= inbox.dayMessages {
		t.Fatalf("unexpected sender daily count default: sender=%d inbox=%d", sender.dayMessages, inbox.dayMessages)
	}
	if sender.hourBytes < maxRawEmailBytes || sender.hourBytes >= inbox.hourBytes {
		t.Fatalf("unexpected sender hourly byte default: sender=%d inbox=%d", sender.hourBytes, inbox.hourBytes)
	}
	if sender.dayBytes < sender.hourBytes || sender.dayBytes >= inbox.dayBytes {
		t.Fatalf("unexpected sender daily byte default: sender=%d inbox=%d", sender.dayBytes, inbox.dayBytes)
	}
}

func TestReadEnvelopeWithLimitRejectsOversizedMessage(t *testing.T) {
	if _, err := readEnvelopeWithLimit(bytes.NewReader([]byte("0123456789")), 4); err == nil {
		t.Fatal("expected oversized message to be rejected")
	}
}

func TestReadEnvelopeWithLimitRejectsMissingBody(t *testing.T) {
	if _, err := readEnvelopeWithLimit(nil, 100); err == nil {
		t.Fatal("expected missing body to be rejected")
	}
}

func TestGuardEmailProcessingRecoversPerMessagePanic(t *testing.T) {
	err := guardEmailProcessing(42, func() error {
		panic("malformed message")
	})
	if err == nil || !strings.Contains(err.Error(), "sequence 42") {
		t.Fatalf("expected isolated panic error, got %v", err)
	}
}

func TestEmail_extractUUIDFromReplyAddress(t *testing.T) {
	e := &Email{}

	testCases := []struct {
		name     string
		address  string
		expected string
	}{
		{
			name:     "Valid reply address with UUID",
			address:  "support+550e8400-e29b-41d4-a716-446655440000@example.com",
			expected: "550e8400-e29b-41d4-a716-446655440000",
		},
		{
			name:     "Reply address with angle brackets",
			address:  "<support+123e4567-e89b-42d3-a456-426614174000@example.com>",
			expected: "123e4567-e89b-42d3-a456-426614174000",
		},
		{
			name:     "No plus sign in address",
			address:  "support@example.com",
			expected: "",
		},
		{
			name:     "Plus sign but no UUID",
			address:  "support+test@example.com",
			expected: "",
		},
		{
			name:     "Invalid UUID format",
			address:  "support+550e8400-e29b-41d4-a716-44665544000X@example.com",
			expected: "550e8400-e29b-41d4-a716-44665544000X", // extractUUIDFromReplyAddress uses simple format check
		},
		{
			name:     "Empty address",
			address:  "",
			expected: "",
		},
		{
			name:     "UUID too short",
			address:  "support+550e8400-e29b-41d4-a716-4466554400@example.com",
			expected: "",
		},
		{
			name:     "UUID too long",
			address:  "support+550e8400-e29b-41d4-a716-4466554400000@example.com",
			expected: "",
		},
		{
			name:     "Multiple plus signs",
			address:  "support+test+550e8400-e29b-41d4-a716-446655440000@example.com",
			expected: "", // "test+550e8400-e29b-41d4-a716-446655440000" is not 36 chars, so validation fails
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := e.extractUUIDFromReplyAddress(tc.address)
			if result != tc.expected {
				t.Errorf("extractUUIDFromReplyAddress(%q) = %q; expected %q", tc.address, result, tc.expected)
			}
		})
	}
}

// TestGoIMAPMessageIDParsing shows how go-imap fails to parse malformed Message-IDs
// and demonstrates the fallback solution.
// go-imap uses mail.Header.MessageID() which strictly follows RFC 5322 and returns
// empty strings for Message-IDs with multiple @ symbols.
//
// This caused emails to be dropped since we require Message-IDs for deduplication.
// References:
// - https://community.mailcow.email/d/701-multiple-at-in-message-id/5
// - https://github.com/emersion/go-message/issues/154#issuecomment-1425634946
func TestGoIMAPMessageIDParsing(t *testing.T) {
	testCases := []struct {
		input            string
		expectedIMAP     string
		expectedFallback string
		name             string
	}{
		{"<normal@example.com>", "normal@example.com", "normal@example.com", "normal message ID"},
		{"<malformed@@example.com>", "", "malformed@@example.com", "double @ - IMAP fails, fallback works"},
		{"<001c01d710db$a8137a50$f83a6ef0$@jones.smith@example.com>", "", "001c01d710db$a8137a50$f83a6ef0$@jones.smith@example.com", "mailcow-style - IMAP fails, fallback works"},
		{"<test@@@domain.com>", "", "test@@@domain.com", "triple @ - IMAP fails, fallback works"},
		{"  <abc123@example.com>  ", "abc123@example.com", "abc123@example.com", "with whitespace - both handle correctly"},
		{"abc123@example.com", "", "abc123@example.com", "no angle brackets - IMAP fails, fallback works"},
		{"", "", "", "empty input"},
		{"<>", "", "", "empty brackets"},
		{"<CAFnQjQFhY8z@mail.example.com@gateway.company.com>", "", "CAFnQjQFhY8z@mail.example.com@gateway.company.com", "gateway-style - IMAP fails, fallback works"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Test go-imap parsing behavior
			var h mail.Header
			h.Set("Message-Id", tc.input)
			imapResult, _ := h.MessageID()

			if imapResult != tc.expectedIMAP {
				t.Errorf("IMAP parsing of %q: expected %q, got %q", tc.input, tc.expectedIMAP, imapResult)
			}

			// Test fallback solution
			if tc.input != "" {
				rawEmail := "From: test@example.com\nMessage-ID: " + tc.input + "\n\nBody"
				envelope, err := enmime.ReadEnvelope(strings.NewReader(rawEmail))
				if err != nil {
					t.Fatal(err)
				}

				fallbackResult := extractMessageIDFromHeaders(envelope)
				if fallbackResult != tc.expectedFallback {
					t.Errorf("Fallback extraction of %q: expected %q, got %q", tc.input, tc.expectedFallback, fallbackResult)
				}

				// Critical check: ensure fallback works when IMAP fails
				if imapResult == "" && tc.expectedFallback != "" && fallbackResult == "" {
					t.Errorf("CRITICAL: Both IMAP and fallback failed for %q - would drop email!", tc.input)
				}
			}
		})
	}
}

// TestEdgeCasesMessageID tests additional edge cases for Message-ID extraction.
func TestEdgeCasesMessageID(t *testing.T) {
	tests := []struct {
		name     string
		email    string
		expected string
	}{
		{
			name: "no Message-ID header",
			email: `From: test@example.com
To: inbox@test.com
Subject: Test

Body`,
			expected: "",
		},
		{
			name: "malformed header syntax",
			email: `From: test@example.com
Message-ID: malformed-no-brackets@@domain.com
To: inbox@test.com

Body`,
			expected: "malformed-no-brackets@@domain.com",
		},
		{
			name: "multiple Message-ID headers (first wins)",
			email: `From: test@example.com
Message-ID: <first@example.com>
Message-ID: <second@@example.com>
To: inbox@test.com

Body`,
			expected: "first@example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			envelope, err := enmime.ReadEnvelope(strings.NewReader(tt.email))
			if err != nil {
				t.Fatal(err)
			}

			result := extractMessageIDFromHeaders(envelope)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}
