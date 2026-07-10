package main

import (
	"bytes"
	"testing"
	"time"

	"github.com/valyala/fasthttp"
)

func TestRequestBodyLimitBeforeRead(t *testing.T) {
	const configuredUploadLimit = 24 << 20
	tests := []struct {
		name        string
		method      string
		uri         string
		contentType string
		encoding    string
		contentLen  int
		want        int
	}{
		{name: "public JSON", method: fasthttp.MethodPost, uri: "/api/v1/auth/login", contentType: "application/json", want: maxPublicJSONBodyBytes},
		{name: "media multipart", method: fasthttp.MethodPost, uri: "/api/v1/media?inline=true", contentType: "multipart/form-data; boundary=test", contentLen: 1024, want: configuredUploadLimit},
		{name: "customer media multipart", method: fasthttp.MethodPost, uri: "/api/v1/customer/media", contentType: "multipart/form-data; boundary=test", contentLen: 1024, want: configuredUploadLimit},
		{name: "widget media multipart", method: fasthttp.MethodPost, uri: "/api/v1/widget/media/upload", contentType: "multipart/form-data; boundary=test", contentLen: 1024, want: configuredUploadLimit},
		{name: "multipart parser case mismatch", method: fasthttp.MethodPost, uri: "/api/v1/media", contentType: "Multipart/Form-Data; boundary=test", contentLen: 1024, want: maxPublicJSONBodyBytes},
		{name: "boundary parameter case mismatch", method: fasthttp.MethodPost, uri: "/api/v1/media", contentType: "multipart/form-data; Boundary=test", contentLen: 1024, want: maxPublicJSONBodyBytes},
		{name: "multipart missing boundary", method: fasthttp.MethodPost, uri: "/api/v1/media", contentType: "multipart/form-data", contentLen: 1024, want: maxPublicJSONBodyBytes},
		{name: "multipart empty boundary", method: fasthttp.MethodPost, uri: "/api/v1/media", contentType: "multipart/form-data; boundary=", contentLen: 1024, want: maxPublicJSONBodyBytes},
		{name: "multipart oversized boundary", method: fasthttp.MethodPost, uri: "/api/v1/media", contentType: "multipart/form-data; boundary=abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrs", contentLen: 1024, want: maxPublicJSONBodyBytes},
		{name: "multipart missing length", method: fasthttp.MethodPost, uri: "/api/v1/media", contentType: "multipart/form-data; boundary=test", want: maxPublicJSONBodyBytes},
		{name: "multipart chunked", method: fasthttp.MethodPost, uri: "/api/v1/media", contentType: "multipart/form-data; boundary=test", contentLen: -1, want: maxPublicJSONBodyBytes},
		{name: "media wrong method", method: fasthttp.MethodPut, uri: "/api/v1/media", contentType: "multipart/form-data; boundary=test", contentLen: 1024, want: maxPublicJSONBodyBytes},
		{name: "media lookalike", method: fasthttp.MethodPost, uri: "/api/v1/media/extra", contentType: "multipart/form-data; boundary=test", contentLen: 1024, want: maxPublicJSONBodyBytes},
		{name: "media wrong content type", method: fasthttp.MethodPost, uri: "/api/v1/media", contentType: "application/json", want: maxPublicJSONBodyBytes},
		{name: "agent avatar", method: fasthttp.MethodPut, uri: "/api/v1/agents/me", contentType: "multipart/form-data; boundary=test", contentLen: 1024, want: maxAvatarMultipartBodyBytes},
		{name: "contact avatar", method: fasthttp.MethodPut, uri: "/api/v1/contacts/42", contentType: "multipart/form-data; boundary=test", contentLen: 1024, want: maxAvatarMultipartBodyBytes},
		{name: "contact subroute", method: fasthttp.MethodPut, uri: "/api/v1/contacts/42/block", contentType: "multipart/form-data; boundary=test", contentLen: 1024, want: maxPublicJSONBodyBytes},
		{name: "agent import", method: fasthttp.MethodPost, uri: "/api/v1/agents/import", contentType: "multipart/form-data; boundary=test", contentLen: 1024, want: maxImportMultipartBodyBytes},
		{name: "compressed multipart fails closed", method: fasthttp.MethodPost, uri: "/api/v1/media", contentType: "multipart/form-data; boundary=test", encoding: "gzip", contentLen: 1024, want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			header := &fasthttp.RequestHeader{}
			header.SetMethod(tt.method)
			header.SetRequestURI(tt.uri)
			if tt.contentType != "" {
				header.SetContentType(tt.contentType)
			}
			if tt.encoding != "" {
				header.Set("Content-Encoding", tt.encoding)
			}
			if tt.contentLen != 0 {
				header.SetContentLength(tt.contentLen)
			}
			if got := requestBodyLimitBeforeRead(header, configuredUploadLimit); got != tt.want {
				t.Fatalf("requestBodyLimitBeforeRead() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestRequestHeaderConfigClampsUploadAndSetsReadDeadline(t *testing.T) {
	config := requestHeaderConfig(128<<20, 3*time.Second)
	header := &fasthttp.RequestHeader{}
	header.SetMethod(fasthttp.MethodPost)
	header.SetRequestURI("/api/v1/media")
	header.SetContentType("multipart/form-data; boundary=test")
	header.SetContentLength(1024)

	got := config(header)
	if got.MaxRequestBodySize != maxBufferedRequestBodyBytes {
		t.Fatalf("buffer limit = %d, want %d", got.MaxRequestBodySize, maxBufferedRequestBodyBytes)
	}
	if got.ReadTimeout != 3*time.Second {
		t.Fatalf("read timeout = %s, want 3s", got.ReadTimeout)
	}
	if normalizeHTTPConcurrency(0) != defaultHTTPConcurrency || normalizeHTTPConcurrency(maxHTTPConcurrency+1) != maxHTTPConcurrency {
		t.Fatal("HTTP concurrency must have secure defaults and an upper bound")
	}
}

func TestEnforceRequestBodyPolicy(t *testing.T) {
	tests := []struct {
		name          string
		contentLength int
		encoding      string
		wantStatus    int
		wantCalled    bool
	}{
		{name: "allowed upload", contentLength: 1 << 20, wantStatus: fasthttp.StatusNoContent, wantCalled: true},
		{name: "oversized upload", contentLength: maxMediaMultipartBodyBytes + 1, wantStatus: fasthttp.StatusRequestEntityTooLarge},
		{name: "chunked upload", contentLength: -1, wantStatus: fasthttp.StatusLengthRequired},
		{name: "compressed upload", contentLength: 1024, encoding: "gzip", wantStatus: fasthttp.StatusUnsupportedMediaType},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &fasthttp.RequestCtx{}
			ctx.Request.Header.SetMethod(fasthttp.MethodPost)
			ctx.Request.Header.SetRequestURI("/api/v1/media")
			ctx.Request.Header.SetContentType("multipart/form-data; boundary=test")
			ctx.Request.Header.SetContentLength(tt.contentLength)
			if tt.encoding != "" {
				ctx.Request.Header.Set("Content-Encoding", tt.encoding)
			}

			called := false
			handler := enforceRequestBodyPolicy(func(ctx *fasthttp.RequestCtx) {
				called = true
				ctx.SetStatusCode(fasthttp.StatusNoContent)
			}, maxMediaMultipartBodyBytes)
			handler(ctx)

			if called != tt.wantCalled {
				t.Fatalf("handler called = %v, want %v", called, tt.wantCalled)
			}
			if ctx.Response.StatusCode() != tt.wantStatus {
				t.Fatalf("status = %d, want %d", ctx.Response.StatusCode(), tt.wantStatus)
			}
			if !tt.wantCalled && !ctx.Response.Header.ConnectionClose() {
				t.Fatal("rejected request must close the connection")
			}
		})
	}
}

func TestEnforceRequestBodyPolicyClosesUnreadStream(t *testing.T) {
	ctx := &fasthttp.RequestCtx{}
	ctx.Request.Header.SetMethod(fasthttp.MethodPost)
	ctx.Request.Header.SetRequestURI("/api/v1/media")
	ctx.Request.Header.SetContentType("multipart/form-data; boundary=test")
	ctx.Request.SetBodyStream(bytes.NewReader([]byte("body")), 4)

	enforceRequestBodyPolicy(func(ctx *fasthttp.RequestCtx) {
		ctx.SetStatusCode(fasthttp.StatusUnauthorized)
	}, maxMediaMultipartBodyBytes)(ctx)

	if ctx.Request.IsBodyStream() {
		t.Fatal("unread request stream must be closed")
	}
	if !ctx.Response.Header.ConnectionClose() {
		t.Fatal("connection with an unread request stream must not be reused")
	}
}
