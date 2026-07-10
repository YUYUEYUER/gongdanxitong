package s3

import "testing"

func TestSafeStoredContentType(t *testing.T) {
	tests := map[string]string{
		"text/html; charset=utf-8": "application/octet-stream",
		"image/svg+xml":            "application/octet-stream",
		"application/javascript":   "application/octet-stream",
		"image/png":                "image/png",
		"application/pdf":          "application/pdf",
	}
	for input, want := range tests {
		if got := safeStoredContentType(input); got != want {
			t.Fatalf("safeStoredContentType(%q) = %q, want %q", input, got, want)
		}
	}
}
