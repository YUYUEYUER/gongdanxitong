package main

import (
	"bytes"
	"time"

	"github.com/valyala/fasthttp"
)

const (
	maxAvatarMultipartBodyBytes = 3 << 20
	maxImportMultipartBodyBytes = 2 << 20
	maxMediaMultipartBodyBytes  = 32 << 20
	maxBufferedRequestBodyBytes = maxPublicJSONBodyBytes
	defaultHTTPConcurrency      = 64
	maxHTTPConcurrency          = 1024
)

func requestHeaderConfig(maxConfiguredBodySize int, readTimeout time.Duration) func(*fasthttp.RequestHeader) fasthttp.RequestConfig {
	maxMediaBodySize := configuredMediaBodyLimit(maxConfiguredBodySize)

	return func(header *fasthttp.RequestHeader) fasthttp.RequestConfig {
		// Bodies above this small buffer are streamed to the handler. This lets
		// authentication and rate limiting run before multipart parsing.
		bufferLimit := min(requestBodyLimitBeforeRead(header, maxMediaBodySize), maxBufferedRequestBodyBytes)
		return fasthttp.RequestConfig{
			ReadTimeout:        readTimeout,
			MaxRequestBodySize: bufferLimit,
		}
	}
}

func configuredMediaBodyLimit(maxConfiguredBodySize int) int {
	if maxConfiguredBodySize <= 0 || maxConfiguredBodySize > maxMediaMultipartBodyBytes {
		return maxMediaMultipartBodyBytes
	}
	return maxConfiguredBodySize
}

func requestBodyLimitBeforeRead(header *fasthttp.RequestHeader, maxMediaBodySize int) int {
	if header == nil {
		return maxPublicJSONBodyBytes
	}
	// Compressed request bodies are unsupported. In particular, allowing gzip
	// on multipart endpoints would bypass fasthttp's bounded file spooling.
	if len(bytes.TrimSpace(header.Peek("Content-Encoding"))) > 0 {
		return 1
	}
	// fasthttp can only spool multipart files to disk when it has a valid
	// boundary and a fixed body length. Without both, the body is buffered in
	// memory before authentication and rate limiting run, so keep the small
	// public-request limit for malformed or chunked requests.
	boundary := header.MultipartFormBoundary()
	if header.ContentLength() <= 0 || len(boundary) == 0 || len(boundary) > 70 {
		return maxPublicJSONBodyBytes
	}

	method := header.Method()
	path := header.RequestURI()
	if query := bytes.IndexByte(path, '?'); query >= 0 {
		path = path[:query]
	}

	switch {
	case bytes.Equal(method, []byte(fasthttp.MethodPost)) &&
		(bytes.Equal(path, []byte("/api/v1/media")) ||
			bytes.Equal(path, []byte("/api/v1/customer/media")) ||
			bytes.Equal(path, []byte("/api/v1/widget/media/upload"))):
		return maxMediaBodySize
	case bytes.Equal(method, []byte(fasthttp.MethodPut)) && bytes.Equal(path, []byte("/api/v1/agents/me")):
		return min(maxAvatarMultipartBodyBytes, maxMediaBodySize)
	case bytes.Equal(method, []byte(fasthttp.MethodPut)) && isNumericPath(path, []byte("/api/v1/contacts/")):
		return min(maxAvatarMultipartBodyBytes, maxMediaBodySize)
	case bytes.Equal(method, []byte(fasthttp.MethodPost)) &&
		(bytes.Equal(path, []byte("/api/v1/agents/import")) || bytes.Equal(path, []byte("/api/v1/tags/import"))):
		return min(maxImportMultipartBodyBytes, maxMediaBodySize)
	default:
		return maxPublicJSONBodyBytes
	}
}

func enforceRequestBodyPolicy(next fasthttp.RequestHandler, maxConfiguredBodySize int) fasthttp.RequestHandler {
	maxMediaBodySize := configuredMediaBodyLimit(maxConfiguredBodySize)

	return func(ctx *fasthttp.RequestCtx) {
		fail := func(status int, message string) {
			// Streamed requests can still contain unread bytes. Never reuse the
			// connection and interpret those bytes as another HTTP request.
			ctx.Error(message, status)
			ctx.Response.Header.SetConnectionClose()
			_ = ctx.Request.CloseBodyStream()
		}

		if len(bytes.TrimSpace(ctx.Request.Header.Peek("Content-Encoding"))) > 0 {
			fail(fasthttp.StatusUnsupportedMediaType, "compressed request bodies are not supported")
			return
		}

		contentLength := ctx.Request.Header.ContentLength()
		if contentLength < 0 {
			fail(fasthttp.StatusLengthRequired, "Content-Length is required")
			return
		}
		if contentLength > requestBodyLimitBeforeRead(&ctx.Request.Header, maxMediaBodySize) {
			fail(fasthttp.StatusRequestEntityTooLarge, "request body too large")
			return
		}

		next(ctx)
		if ctx.Request.IsBodyStream() {
			// A failed authentication can intentionally leave the body unread.
			ctx.Response.Header.SetConnectionClose()
			_ = ctx.Request.CloseBodyStream()
		}
	}
}

func isNumericPath(path, prefix []byte) bool {
	if !bytes.HasPrefix(path, prefix) || len(path) == len(prefix) {
		return false
	}
	for _, char := range path[len(prefix):] {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func normalizeHTTPConcurrency(configured int) int {
	if configured <= 0 {
		return defaultHTTPConcurrency
	}
	return min(configured, maxHTTPConcurrency)
}
