package turnstile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/zerodha/logf"
)

const verifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"
const defaultVerifyTimeout = 5 * time.Second

var (
	ErrTokenMissing       = errors.New("turnstile: token missing")
	ErrVerificationFailed = errors.New("turnstile: verification failed")
)

type Verifier struct {
	client         *http.Client
	enabled        bool
	siteKey        string
	secretKey      string
	verifyURL      string
	expectedAction string
	expectedCData  string
	expectedHost   string
	allowedHosts   map[string]struct{}
	lo             *logf.Logger
}

type verifyResponse struct {
	Success    bool     `json:"success"`
	ErrorCodes []string `json:"error-codes"`
	Hostname   string   `json:"hostname"`
	Action     string   `json:"action"`
	CData      string   `json:"cdata"`
}

type VerificationError struct {
	Codes []string
}

func (e *VerificationError) Error() string {
	if len(e.Codes) == 0 {
		return ErrVerificationFailed.Error()
	}
	return fmt.Sprintf("%s: %s", ErrVerificationFailed.Error(), strings.Join(e.Codes, ","))
}

func (e *VerificationError) Unwrap() error {
	return ErrVerificationFailed
}

type Option func(*Verifier)

func WithVerifyURL(rawURL string) Option {
	return func(v *Verifier) {
		rawURL = strings.TrimSpace(rawURL)
		if rawURL != "" {
			v.verifyURL = rawURL
		}
	}
}

func WithTimeout(timeout time.Duration) Option {
	return func(v *Verifier) {
		if timeout > 0 {
			v.client.Timeout = timeout
		}
	}
}

func WithExpectedAction(action string) Option {
	return func(v *Verifier) {
		v.expectedAction = strings.TrimSpace(action)
	}
}

func WithExpectedCData(cdata string) Option {
	return func(v *Verifier) {
		v.expectedCData = strings.TrimSpace(cdata)
	}
}

func WithExpectedHostname(hostname string) Option {
	return func(v *Verifier) {
		v.expectedHost = strings.TrimSpace(strings.ToLower(hostname))
	}
}

func WithAllowedHostnames(hostnames []string) Option {
	return func(v *Verifier) {
		v.allowedHosts = make(map[string]struct{}, len(hostnames))
		for _, hostname := range hostnames {
			hostname = strings.TrimSpace(strings.ToLower(hostname))
			if hostname != "" {
				v.allowedHosts[hostname] = struct{}{}
			}
		}
	}
}

func New(enabled bool, siteKey, secretKey string, lo *logf.Logger, opts ...Option) *Verifier {
	enabled = enabled && strings.TrimSpace(siteKey) != "" && strings.TrimSpace(secretKey) != ""

	v := &Verifier{
		client: &http.Client{
			Timeout: defaultVerifyTimeout,
		},
		enabled:   enabled,
		siteKey:   strings.TrimSpace(siteKey),
		secretKey: strings.TrimSpace(secretKey),
		verifyURL: verifyURL,
		lo:        lo,
	}

	for _, opt := range opts {
		opt(v)
	}

	return v
}

func (v *Verifier) Enabled() bool {
	return v != nil && v.enabled
}

func (v *Verifier) SiteKey() string {
	if !v.Enabled() {
		return ""
	}
	return v.siteKey
}

func (v *Verifier) Verify(ctx context.Context, token, remoteIP string, opts ...Option) error {
	if !v.Enabled() {
		return nil
	}

	verifier := *v
	client := *v.client
	verifier.client = &client
	for _, opt := range opts {
		opt(&verifier)
	}

	token = strings.TrimSpace(token)
	if token == "" {
		return ErrTokenMissing
	}

	form := url.Values{}
	form.Set("secret", v.secretKey)
	form.Set("response", token)
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, verifier.verifyURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("creating turnstile verification request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := verifier.client.Do(req)
	if err != nil {
		return fmt.Errorf("making turnstile verification request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		if v.lo != nil {
			v.lo.Error("turnstile verification returned non-200", "status_code", resp.StatusCode, "body", string(body))
		}
		return fmt.Errorf("turnstile verification returned status %d", resp.StatusCode)
	}

	var out verifyResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return fmt.Errorf("decoding turnstile verification response: %w", err)
	}

	if !out.Success {
		if verifier.lo != nil {
			verifier.lo.Warn("turnstile verification failed", "error_codes", out.ErrorCodes)
		}
		return &VerificationError{Codes: out.ErrorCodes}
	}

	if err := verifier.validateResponse(out); err != nil {
		return err
	}

	return nil
}

func (v *Verifier) validateResponse(out verifyResponse) error {
	if v.expectedAction != "" && out.Action != v.expectedAction {
		return &VerificationError{Codes: []string{"invalid-action"}}
	}

	if v.expectedCData != "" && out.CData != v.expectedCData {
		return &VerificationError{Codes: []string{"invalid-cdata"}}
	}

	hostname := strings.TrimSpace(strings.ToLower(out.Hostname))
	if v.expectedHost != "" && hostname != v.expectedHost {
		return &VerificationError{Codes: []string{"invalid-hostname"}}
	}

	if len(v.allowedHosts) > 0 {
		if _, ok := v.allowedHosts[hostname]; !ok {
			return &VerificationError{Codes: []string{"invalid-hostname"}}
		}
	}

	return nil
}
