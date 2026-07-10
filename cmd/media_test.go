package main

import (
	"testing"

	"github.com/abhinavxd/libredesk/internal/attachment"
)

func TestWithinUnlinkedUploadQuota(t *testing.T) {
	tests := []struct {
		name              string
		count, used, next int64
		want              bool
	}{
		{name: "within quota", count: 1, used: 10, next: 20, want: true},
		{name: "count exhausted", count: maxUnlinkedUploadsPerUser, used: 0, next: 1, want: false},
		{name: "bytes exactly fill quota", count: 1, used: maxUnlinkedUploadBytes - 1, next: 1, want: true},
		{name: "bytes exceed quota", count: 1, used: maxUnlinkedUploadBytes, next: 1, want: false},
		{name: "invalid size", count: 0, used: 0, next: 0, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := withinUnlinkedUploadQuota(tt.count, tt.used, tt.next); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSafeInlineMediaType(t *testing.T) {
	for _, contentType := range []string{"image/png", "image/jpeg; charset=binary", "video/mp4"} {
		if !safeInlineMediaType(contentType) {
			t.Fatalf("expected %q to be safe for inline rendering", contentType)
		}
	}
	for _, contentType := range []string{
		"application/pdf",
		"image/svg+xml",
		"image/svg+xml; charset=utf-8",
		"text/html",
		"application/javascript",
		"image/unknown",
		"image/webp",
		"image/avif",
	} {
		if safeInlineMediaType(contentType) {
			t.Fatalf("expected %q to require download", contentType)
		}
	}
}

func TestValidMediaSignatureUsesExactThumbnailNameAndLegacyFallback(t *testing.T) {
	validator := func(name, sig string, exp int64) bool {
		return sig == "signed:"+name && exp == 42
	}
	if !validMediaSignature(validator, "thumb_uuid", "signed:thumb_uuid", 42) {
		t.Fatal("new thumbnail signatures must validate against the exact object name")
	}
	if !validMediaSignature(validator, "thumb_uuid", "signed:uuid", 42) {
		t.Fatal("legacy thumbnail signatures must remain valid during rollout")
	}
	if validMediaSignature(validator, "thumb_uuid", "signed:other", 42) {
		t.Fatal("unrelated signatures must be rejected")
	}
}

func TestSafeAttachmentCIDURLUsesThumbnailOnlyForAutomaticImageLoads(t *testing.T) {
	imageAttachment := attachment.Attachment{
		ContentType:  "image/png",
		URL:          "https://example.test/original",
		ThumbnailURL: "https://example.test/thumbnail",
	}
	if got := safeAttachmentCIDURL(imageAttachment); got != imageAttachment.ThumbnailURL {
		t.Fatalf("got %q, want thumbnail URL", got)
	}
	unsafe := attachment.Attachment{ContentType: "image/webp", URL: "https://example.test/original"}
	if got := safeAttachmentCIDURL(unsafe); got != "" {
		t.Fatalf("unvalidated image codec must not receive an automatic CID URL: %q", got)
	}
}
