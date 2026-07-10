package media

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/abhinavxd/libredesk/internal/attachment"
)

type mediaURLTestStore struct{}

func (mediaURLTestStore) Put(name, _ string, _ io.ReadSeeker) (string, error) { return name, nil }
func (mediaURLTestStore) Delete(string) error                                 { return nil }
func (mediaURLTestStore) GetURL(name, disposition, fileName string) string {
	return name + "|" + disposition + "|" + fileName
}
func (mediaURLTestStore) GetBlob(string) ([]byte, error)                       { return nil, nil }
func (mediaURLTestStore) Name() string                                         { return "test" }
func (mediaURLTestStore) SignedURLValidator() func(string, string, int64) bool { return nil }

func TestSniffContentTypeIgnoresSpoofedActiveContentType(t *testing.T) {
	content := bytes.NewReader([]byte("<!doctype html><html><body>active</body></html>"))
	got, err := sniffContentType(content)
	if err != nil {
		t.Fatal(err)
	}
	if got != "text/html; charset=utf-8" && got != "text/html" {
		t.Fatalf("expected HTML content type, got %q", got)
	}
	if pos, _ := content.Seek(0, 1); pos != 0 {
		t.Fatalf("expected reader to be rewound, position=%d", pos)
	}
}

func TestThumbnailAccountingMetadataIsExplicit(t *testing.T) {
	meta := withThumbnailSizeMeta([]byte(`{"width":100}`), 0)
	var values map[string]any
	if err := json.Unmarshal(meta, &values); err != nil {
		t.Fatal(err)
	}
	if values["thumbnail_size"] != float64(0) || values["width"] != float64(100) {
		t.Fatalf("unexpected metadata: %s", meta)
	}
}

func TestSafeInlineMediaContentTypeRejectsUnboundedBrowserCodecs(t *testing.T) {
	for _, contentType := range []string{"image/png", "image/jpeg", "image/gif", "video/mp4", "application/pdf"} {
		if !safeInlineMediaContentType(contentType) {
			t.Fatalf("expected safe inline type: %s", contentType)
		}
	}
	for _, contentType := range []string{"image/webp", "image/avif", "image/svg+xml", "text/html"} {
		if safeInlineMediaContentType(contentType) {
			t.Fatalf("expected attachment-only type: %s", contentType)
		}
	}
}

func TestDecorateAttachmentIssuesDistinctObjectAndDispositionURLs(t *testing.T) {
	manager := &Manager{store: mediaURLTestStore{}}
	item := attachment.Attachment{
		UUID:               "object-uuid",
		Name:               "disguised.pdf",
		ContentType:        "image/png",
		ThumbnailAvailable: true,
	}
	manager.DecorateAttachment(&item)

	if item.URL != "object-uuid|inline|disguised.pdf" {
		t.Fatalf("unexpected preview URL: %s", item.URL)
	}
	if item.DownloadURL != "object-uuid|attachment|disguised.pdf" {
		t.Fatalf("unexpected download URL: %s", item.DownloadURL)
	}
	if item.ThumbnailURL != "thumb_object-uuid|inline|thumbnail-disguised.pdf" {
		t.Fatalf("unexpected thumbnail URL: %s", item.ThumbnailURL)
	}
}

func TestOwnedMediaQuotaIncludesLinkedUsageAndPreventsOverflow(t *testing.T) {
	if !withinOwnedMediaQuota(10, 100, 20) {
		t.Fatal("normal persistent usage should be accepted")
	}
	if withinOwnedMediaQuota(MaxOwnedMediaFiles, 0, 1) {
		t.Fatal("file-count limit must be enforced")
	}
	if withinOwnedMediaQuota(0, MaxOwnedMediaBytes, 1) {
		t.Fatal("byte limit must be enforced")
	}
	if withinOwnedMediaQuota(0, MaxOwnedMediaBytes-1, 2) {
		t.Fatal("next upload must not overflow the remaining byte budget")
	}

	queries, err := efs.ReadFile("queries.sql")
	if err != nil {
		t.Fatal(err)
	}
	queryText := string(queries)
	ownedStart := strings.Index(queryText, "-- name: get-owned-media-usage")
	lockStart := strings.Index(queryText, "-- name: lock-media-owner")
	if ownedStart < 0 || lockStart <= ownedStart {
		t.Fatal("persistent owner usage query is missing")
	}
	ownedQuery := queryText[ownedStart:lockStart]
	if !strings.Contains(ownedQuery, "WHERE owner_user_id = $1;") || strings.Contains(ownedQuery, "model_id") {
		t.Fatal("persistent owner usage must count linked and unlinked media")
	}
	if !strings.Contains(queryText, "FOR UPDATE;") {
		t.Fatal("owner quota inserts must serialize on the owner row")
	}
}

func TestInstanceMediaQuotaCountsObjectsAndBytesAtBoundary(t *testing.T) {
	if !withinMediaQuota(8, 900, 2, 100, 10, 1000) {
		t.Fatal("remaining two objects and bytes should fit exactly")
	}
	if withinMediaQuota(9, 900, 2, 100, 10, 1000) {
		t.Fatal("thumbnail object must count against the global object limit")
	}
	if withinMediaQuota(8, 901, 2, 100, 10, 1000) {
		t.Fatal("thumbnail bytes must count against the global byte limit")
	}

	queries, err := efs.ReadFile("queries.sql")
	if err != nil {
		t.Fatal(err)
	}
	queryText := string(queries)
	if !strings.Contains(queryText, "pg_advisory_xact_lock") ||
		!strings.Contains(queryText, "-- name: get-global-media-usage") ||
		!strings.Contains(queryText, "-- name: get-unlinked-media-usage") ||
		!strings.Contains(queryText, "thumbnail_size") {
		t.Fatal("global media quota must serialize inserts and account for thumbnails")
	}
}
