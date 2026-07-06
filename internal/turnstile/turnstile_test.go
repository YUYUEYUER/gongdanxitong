package turnstile

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zerodha/logf"
)

func TestVerifierVerifySuccess(t *testing.T) {
	lo := logf.New(logf.Opts{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if got := r.Form.Get("secret"); got != "secret" {
			t.Fatalf("unexpected secret: %q", got)
		}
		if got := r.Form.Get("response"); got != "token" {
			t.Fatalf("unexpected response token: %q", got)
		}
		if got := r.Form.Get("remoteip"); got != "127.0.0.1" {
			t.Fatalf("unexpected remote ip: %q", got)
		}

		_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
	}))
	defer srv.Close()

	v := New(true, "site", "secret", &lo, WithVerifyURL(srv.URL))
	v.client = srv.Client()

	if err := v.Verify(context.Background(), "token", "127.0.0.1"); err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
}

func TestVerifierVerifyMissingToken(t *testing.T) {
	lo := logf.New(logf.Opts{})
	v := New(true, "site", "secret", &lo)
	if err := v.Verify(context.Background(), "", ""); err != ErrTokenMissing {
		t.Fatalf("expected ErrTokenMissing, got %v", err)
	}
}

func TestVerifierVerifyFailure(t *testing.T) {
	lo := logf.New(logf.Opts{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success":     false,
			"error-codes": []string{"invalid-input-response"},
		})
	}))
	defer srv.Close()

	v := New(true, "site", "secret", &lo, WithVerifyURL(srv.URL))
	v.client = srv.Client()

	err := v.Verify(context.Background(), "token", "")
	if err == nil {
		t.Fatal("expected verification error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid-input-response") {
		t.Fatalf("expected error code in message, got %v", err)
	}
}

func TestVerifierVerifyActionMismatch(t *testing.T) {
	lo := logf.New(logf.Opts{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"action":  "other_action",
		})
	}))
	defer srv.Close()

	v := New(true, "site", "secret", &lo, WithVerifyURL(srv.URL))
	v.client = srv.Client()

	err := v.Verify(context.Background(), "token", "", WithExpectedAction("customer_register"))
	if err == nil {
		t.Fatal("expected action mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid-action") {
		t.Fatalf("expected invalid-action in message, got %v", err)
	}
}

func TestVerifierVerifyTimeout(t *testing.T) {
	lo := logf.New(logf.Opts{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
	}))
	defer srv.Close()

	v := New(true, "site", "secret", &lo, WithVerifyURL(srv.URL))
	v.client = srv.Client()
	v.client.Timeout = 10 * time.Millisecond

	if err := v.Verify(context.Background(), "token", ""); err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}
