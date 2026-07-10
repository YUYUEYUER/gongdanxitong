package image

import (
	"bytes"
	"errors"
	stdimage "image"
	"image/color"
	"image/png"
	"io"
	"strings"
	"testing"

	"github.com/disintegration/imaging"
)

func encodePNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := stdimage.NewRGBA(stdimage.Rect(0, 0, width, height))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatalf("encode PNG: %v", err)
	}
	return out.Bytes()
}

func TestCreateThumbWithDimensionsBoundsBothAxes(t *testing.T) {
	input := encodePNG(t, 100, 400)
	thumb, width, height, err := CreateThumbWithDimensions(150, bytes.NewReader(input))
	if err != nil {
		t.Fatalf("create thumbnail: %v", err)
	}
	if width != 100 || height != 400 {
		t.Fatalf("unexpected original dimensions: %dx%d", width, height)
	}
	decoded, err := imaging.Decode(thumb)
	if err != nil {
		t.Fatalf("decode thumbnail: %v", err)
	}
	bounds := decoded.Bounds()
	if bounds.Dx() > 150 || bounds.Dy() > 150 {
		t.Fatalf("thumbnail exceeds bounds: %dx%d", bounds.Dx(), bounds.Dy())
	}
}

func TestValidateDimensionsRejectsResourceBombShapes(t *testing.T) {
	tests := []struct {
		name          string
		width, height int
	}{
		{name: "zero", width: 0, height: 10},
		{name: "dimension", width: MaxImageDimension + 1, height: 1},
		{name: "pixels", width: 6_000, height: 6_000},
		{name: "aspect ratio", width: 101, height: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateDimensions(tt.width, tt.height); err == nil {
				t.Fatalf("expected %dx%d to be rejected", tt.width, tt.height)
			}
		})
	}
}

func TestGetDimensionsRejectsExtremeAspectRatioBeforeDecode(t *testing.T) {
	input := encodePNG(t, 1, 101)
	if _, _, err := GetDimensions(bytes.NewReader(input)); err == nil {
		t.Fatal("expected extreme aspect ratio to be rejected")
	}
}

func TestCreateThumbSkipsDecodeWhenCapacityIsFull(t *testing.T) {
	for range MaxConcurrentThumbnailDecodes {
		thumbnailDecodeSlots <- struct{}{}
	}
	defer func() {
		for range MaxConcurrentThumbnailDecodes {
			<-thumbnailDecodeSlots
		}
	}()

	input := encodePNG(t, 20, 10)
	thumb, width, height, err := CreateThumbWithDimensions(10, bytes.NewReader(input))
	if !errors.Is(err, ErrThumbnailBusy) {
		t.Fatalf("expected decoder capacity error, got %v", err)
	}
	if thumb != nil || width != 20 || height != 10 {
		t.Fatalf("busy result must preserve dimensions without a thumbnail: %v %dx%d", thumb, width, height)
	}
}

func TestCreateAvatarReencodesAndBoundsRaster(t *testing.T) {
	input := append(encodePNG(t, 800, 400), []byte("untrusted-trailing-metadata")...)
	avatar, err := CreateAvatar(bytes.NewReader(input))
	if err != nil {
		t.Fatalf("create avatar: %v", err)
	}
	output, err := io.ReadAll(avatar)
	if err != nil {
		t.Fatalf("read sanitized avatar: %v", err)
	}
	if bytes.Contains(output, []byte("untrusted-trailing-metadata")) {
		t.Fatal("sanitized avatar retained bytes from the original upload")
	}

	cfg, format, err := stdimage.DecodeConfig(bytes.NewReader(output))
	if err != nil {
		t.Fatalf("decode sanitized avatar config: %v", err)
	}
	if format != "png" {
		t.Fatalf("sanitized avatar format = %q, want png", format)
	}
	if cfg.Width > MaxAvatarDimension || cfg.Height > MaxAvatarDimension {
		t.Fatalf("sanitized avatar exceeds bounds: %dx%d", cfg.Width, cfg.Height)
	}
	if cfg.Width != 512 || cfg.Height != 256 {
		t.Fatalf("unexpected sanitized dimensions: %dx%d", cfg.Width, cfg.Height)
	}
}

func TestCreateAvatarRejectsSpoofedAndUnsafeImages(t *testing.T) {
	t.Run("non-image magic", func(t *testing.T) {
		_, err := CreateAvatar(strings.NewReader("<svg><script>alert(1)</script></svg>"))
		if !errors.Is(err, ErrUnsupportedFormat) {
			t.Fatalf("expected unsupported format error, got %v", err)
		}
	})

	t.Run("unsafe dimensions", func(t *testing.T) {
		_, err := CreateAvatar(bytes.NewReader(encodePNG(t, 1, MaxImageAspectRatio+1)))
		if err == nil {
			t.Fatal("expected extreme aspect ratio to be rejected")
		}
	})
}
