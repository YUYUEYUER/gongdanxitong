package main

import (
	"bytes"
	"errors"
	"fmt"
	stdimage "image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"net/textproto"
	"testing"

	imageutil "github.com/abhinavxd/libredesk/internal/image"
)

func encodeAvatarTestPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := png.Encode(&out, stdimage.NewRGBA(stdimage.Rect(0, 0, width, height))); err != nil {
		t.Fatalf("encode PNG: %v", err)
	}
	return out.Bytes()
}

func avatarFileHeader(t *testing.T, filename, contentType string, content []byte) *multipart.FileHeader {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="files"; filename="%s"`, filename))
	header.Set("Content-Type", contentType)
	part, err := writer.CreatePart(header)
	if err != nil {
		t.Fatalf("create multipart part: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write multipart part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest("POST", "/api/v1/agents/me", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if err := req.ParseMultipartForm(maxAvatarSizeBytes + (1 << 20)); err != nil {
		t.Fatalf("parse multipart form: %v", err)
	}
	t.Cleanup(func() { _ = req.MultipartForm.RemoveAll() })
	files := req.MultipartForm.File["files"]
	if len(files) != 1 {
		t.Fatalf("multipart file count = %d, want 1", len(files))
	}
	return files[0]
}

func TestPrepareUserAvatarUsesMagicAndCanonicalizesOutput(t *testing.T) {
	marker := []byte("untrusted-original-trailer")
	input := append(encodeAvatarTestPNG(t, 800, 400), marker...)
	// The client-controlled filename and MIME type are deliberately false.
	header := avatarFileHeader(t, "avatar.svg", "text/html", input)

	avatar, err := prepareUserAvatar(header)
	if err != nil {
		t.Fatalf("prepare avatar: %v", err)
	}
	output, err := io.ReadAll(avatar)
	if err != nil {
		t.Fatalf("read prepared avatar: %v", err)
	}
	if bytes.Contains(output, marker) {
		t.Fatal("prepared avatar retained bytes from the original upload")
	}
	cfg, format, err := stdimage.DecodeConfig(bytes.NewReader(output))
	if err != nil {
		t.Fatalf("decode prepared avatar config: %v", err)
	}
	if format != "png" || cfg.Width != imageutil.MaxAvatarDimension || cfg.Height != imageutil.MaxAvatarDimension/2 {
		t.Fatalf("prepared avatar = %s %dx%d, want png 512x256", format, cfg.Width, cfg.Height)
	}
	if avatarFilename != "avatar.png" || avatarContentType != "image/png" {
		t.Fatalf("unexpected canonical metadata: %q %q", avatarFilename, avatarContentType)
	}
}

func TestPrepareUserAvatarRejectsSpoofedContent(t *testing.T) {
	header := avatarFileHeader(t, "avatar.png", "image/png", []byte("<html>not an image</html>"))
	if _, err := prepareUserAvatar(header); !errors.Is(err, errAvatarInvalid) {
		t.Fatalf("expected invalid avatar error, got %v", err)
	}
}

func TestPrepareUserAvatarRejectsOversizedUploadBeforeDecode(t *testing.T) {
	header := avatarFileHeader(t, "avatar.png", "image/png", bytes.Repeat([]byte{'x'}, int(maxAvatarSizeBytes)+1))
	if _, err := prepareUserAvatar(header); !errors.Is(err, errAvatarTooLarge) {
		t.Fatalf("expected avatar-too-large error, got %v", err)
	}
}
