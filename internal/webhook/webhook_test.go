package webhook

import (
	"strings"
	"testing"
)

func TestReadLimitedResponseTruncatesAtLimit(t *testing.T) {
	body, truncated, err := readLimitedResponse(strings.NewReader("0123456789"), 4)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "0123" || !truncated {
		t.Fatalf("body=%q truncated=%v", body, truncated)
	}
}

func TestReadLimitedResponseKeepsSmallBody(t *testing.T) {
	body, truncated, err := readLimitedResponse(strings.NewReader("ok"), 4)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "ok" || truncated {
		t.Fatalf("body=%q truncated=%v", body, truncated)
	}
}
