// Package image provides utilities for processing image files, including
// retrieving image dimensions and creating thumbnails.
package image

import (
	"bytes"
	"errors"
	"fmt"
	stdimage "image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"

	"github.com/disintegration/imaging"
	"github.com/gabriel-vasile/mimetype"
)

var (
	Exts         = []string{"gif", "png", "jpg", "jpeg"}
	DefThumbSize = 150
	ThumbPrefix  = "thumb_"

	ErrImageDecoderBusy  = errors.New("image decoder is busy")
	ErrThumbnailBusy     = ErrImageDecoderBusy
	ErrUnsupportedFormat = errors.New("unsupported image format")
	thumbnailDecodeSlots = make(chan struct{}, MaxConcurrentThumbnailDecodes)
)

const (
	MaxImagePixels                = 12_000_000
	MaxImageDimension             = 12_000
	MaxImageAspectRatio           = 100
	MaxAvatarDimension            = 512
	MaxConcurrentThumbnailDecodes = 2
	ThumbnailStorageReserveBytes  = int64(128 << 10)
)

// IsImageByContent returns true when the file's magic bytes identify it as one
// of the raster formats this package can decode. Used as a fallback when the
// filename has no extension or an unreliable one (e.g. attachments arriving
// through email without proper file extensions).
func IsImageByContent(r io.ReadSeeker) bool {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return false
	}
	defer r.Seek(0, io.SeekStart)
	mtype, err := mimetype.DetectReader(r)
	if err != nil {
		return false
	}
	switch mtype.String() {
	case "image/png", "image/jpeg", "image/gif":
		return true
	}
	return false
}

// GetDimensions returns the width and height of the image in the provided file.
// It returns an error if the image cannot be decoded.
func GetDimensions(r io.Reader) (int, int, error) {
	cfg, _, err := stdimage.DecodeConfig(r)
	if err != nil {
		return 0, 0, err
	}
	if err := validateDimensions(cfg.Width, cfg.Height); err != nil {
		return 0, 0, err
	}
	return cfg.Width, cfg.Height, nil
}

// CreateThumb generates a thumbnail bounded by thumbPxSize on both axes while
// maintaining the aspect ratio.
func CreateThumb(thumbPxSize int, r io.Reader) (*bytes.Reader, error) {
	thumb, _, _, err := CreateThumbWithDimensions(thumbPxSize, r)
	return thumb, err
}

// CreateThumbWithDimensions validates dimensions before a full decode, then
// returns a thumbnail bounded on both axes and the original dimensions.
func CreateThumbWithDimensions(thumbPxSize int, r io.Reader) (*bytes.Reader, int, int, error) {
	if thumbPxSize <= 0 {
		return nil, 0, 0, fmt.Errorf("thumbnail size must be positive")
	}

	var prefix bytes.Buffer
	cfg, _, err := stdimage.DecodeConfig(io.TeeReader(r, &prefix))
	if err != nil {
		return nil, 0, 0, err
	}
	if err := validateDimensions(cfg.Width, cfg.Height); err != nil {
		return nil, 0, 0, err
	}
	select {
	case thumbnailDecodeSlots <- struct{}{}:
		defer func() { <-thumbnailDecodeSlots }()
	default:
		return nil, cfg.Width, cfg.Height, ErrThumbnailBusy
	}

	img, err := imaging.Decode(io.MultiReader(bytes.NewReader(prefix.Bytes()), r))
	if err != nil {
		return nil, 0, 0, err
	}

	thumb := imaging.Fit(img, thumbPxSize, thumbPxSize, imaging.Lanczos)
	var out bytes.Buffer
	if err := imaging.Encode(&out, thumb, imaging.PNG); err != nil {
		return nil, 0, 0, err
	}

	return bytes.NewReader(out.Bytes()), cfg.Width, cfg.Height, nil
}

// CreateAvatar validates an uploaded raster image using its actual encoded
// format, decodes it within the shared image resource limits, and returns a
// metadata-free PNG bounded by MaxAvatarDimension on both axes. The returned
// bytes are safe to persist instead of the original compressed upload.
func CreateAvatar(r io.Reader) (*bytes.Reader, error) {
	var prefix bytes.Buffer
	cfg, format, err := stdimage.DecodeConfig(io.TeeReader(r, &prefix))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnsupportedFormat, err)
	}
	if !isSupportedRasterFormat(format) {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedFormat, format)
	}
	if err := validateDimensions(cfg.Width, cfg.Height); err != nil {
		return nil, err
	}

	select {
	case thumbnailDecodeSlots <- struct{}{}:
		defer func() { <-thumbnailDecodeSlots }()
	default:
		return nil, ErrImageDecoderBusy
	}

	decoded, decodedFormat, err := stdimage.Decode(io.MultiReader(bytes.NewReader(prefix.Bytes()), r))
	if err != nil {
		return nil, fmt.Errorf("decode avatar: %w", err)
	}
	if decodedFormat != format || !isSupportedRasterFormat(decodedFormat) {
		return nil, fmt.Errorf("%w: inconsistent format", ErrUnsupportedFormat)
	}
	bounds := decoded.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if err := validateDimensions(width, height); err != nil {
		return nil, err
	}
	if width != cfg.Width || height != cfg.Height {
		return nil, fmt.Errorf("image dimensions changed while decoding: %dx%d to %dx%d", cfg.Width, cfg.Height, width, height)
	}

	avatar := imaging.Fit(decoded, MaxAvatarDimension, MaxAvatarDimension, imaging.Lanczos)
	var out bytes.Buffer
	if err := imaging.Encode(&out, avatar, imaging.PNG); err != nil {
		return nil, fmt.Errorf("encode avatar: %w", err)
	}
	return bytes.NewReader(out.Bytes()), nil
}

func isSupportedRasterFormat(format string) bool {
	switch format {
	case "png", "jpeg", "gif":
		return true
	default:
		return false
	}
}

func validateDimensions(width, height int) error {
	if width <= 0 || height <= 0 {
		return fmt.Errorf("invalid image dimensions: %dx%d", width, height)
	}
	if width > MaxImageDimension || height > MaxImageDimension {
		return fmt.Errorf("image dimensions exceed limit: %dx%d", width, height)
	}
	if width > MaxImagePixels/height {
		return fmt.Errorf("image pixel count exceeds limit: %dx%d", width, height)
	}
	longest, shortest := width, height
	if height > width {
		longest, shortest = height, width
	}
	if longest > shortest*MaxImageAspectRatio {
		return fmt.Errorf("image aspect ratio exceeds limit: %dx%d", width, height)
	}
	return nil
}
