package fs

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/abhinavxd/libredesk/internal/media"
)

// Opts holds fs options.
type Opts struct {
	UploadPath string
	UploadURI  string
	RootURL    func() string
	SigningKey string        // HMAC signing key for generating signed URLs.
	Expiry     time.Duration // URL expiry duration.
}

// Client implements `media.Store`
type Client struct {
	opts    Opts
	writeMu sync.Mutex
}

// New initialises store for Filesystem provider.
func New(opts Opts) (media.Store, error) {
	return &Client{
		opts: opts,
	}, nil
}

// Put accepts the filename, the content type and file object itself and stores the file in disk.
func (c *Client) Put(filename string, cType string, src io.ReadSeeker) (string, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.putAtomic(filename, src)
}

// PutWithReserve serializes the physical free-space check and write. This is
// paired with LibreDesk's single-replica deployment contract; the volume or S3
// provider should also enforce an independent quota.
func (c *Client) PutWithReserve(filename, _ string, src io.ReadSeeker, minFreeBytes uint64) (string, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	size, err := src.Seek(0, io.SeekEnd)
	if err != nil {
		return "", fmt.Errorf("measuring upload: %w", err)
	}
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("rewinding upload: %w", err)
	}
	available, err := c.AvailableBytes()
	if err != nil {
		return "", fmt.Errorf("checking upload volume capacity: %w", err)
	}
	if available <= minFreeBytes || uint64(size) > available-minFreeBytes {
		return "", fmt.Errorf("upload volume free-space reserve reached")
	}
	return c.putAtomic(filename, src)
}

func (c *Client) putAtomic(filename string, src io.Reader) (string, error) {

	dir := getDir(c.opts.UploadPath)
	out, err := os.CreateTemp(dir, ".libredesk-upload-*")
	if err != nil {
		return "", fmt.Errorf("creating temporary upload in %q: %w", dir, err)
	}
	temporaryName := out.Name()
	committed := false
	defer func() {
		_ = out.Close()
		if !committed {
			_ = os.Remove(temporaryName)
		}
	}()
	if err := out.Chmod(0664); err != nil {
		return "", fmt.Errorf("setting temporary upload permissions: %w", err)
	}
	if n, err := io.Copy(out, src); err != nil {
		return "", fmt.Errorf("writing temporary upload after %d bytes: %w", n, err)
	}
	if err := out.Sync(); err != nil {
		return "", fmt.Errorf("syncing temporary upload: %w", err)
	}
	if err := out.Close(); err != nil {
		return "", fmt.Errorf("closing temporary upload: %w", err)
	}
	if err := os.Rename(temporaryName, filepath.Join(dir, filename)); err != nil {
		return "", fmt.Errorf("committing upload %q: %w", filepath.Join(dir, filename), err)
	}
	committed = true
	return filename, nil
}

// GetURL accepts a filename and retrieves the full URL for file.
// If a signing key is configured, returns a signed URL with expiry.
func (c *Client) GetURL(name, disposition, _ string) string {
	var result string
	// If no signing key configured, return unsigned URL.
	if c.opts.SigningKey == "" {
		result = fmt.Sprintf("%s%s/%s", c.opts.RootURL(), c.opts.UploadURI, name)
	} else {
		result = c.signURL(name)
	}
	if disposition == "attachment" {
		separator := "?"
		if strings.Contains(result, "?") {
			separator = "&"
		}
		result += separator + "download=1"
	}
	return result
}

// signURL generates a signed URL with expiry timestamp.
func (c *Client) signURL(name string) string {
	exp := time.Now().Add(c.opts.Expiry).Unix()
	sig := c.generateSignature(name, exp)
	return fmt.Sprintf("%s%s/%s?sig=%s&exp=%d", c.opts.RootURL(), c.opts.UploadURI, name, sig, exp)
}

// generateSignature creates HMAC-SHA256 signature for the given name and expiry.
func (c *Client) generateSignature(name string, exp int64) string {
	message := fmt.Sprintf("%s:%d", name, exp)
	h := hmac.New(sha256.New, []byte(c.opts.SigningKey))
	h.Write([]byte(message))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

// ValidateSignature verifies the signature and expiry of a signed URL.
// Returns true if the signature is valid and the URL has not expired.
func (c *Client) ValidateSignature(name, sig string, exp int64) bool {
	if time.Now().Unix() > exp {
		return false
	}
	expectedSig := c.generateSignature(name, exp)
	return hmac.Equal([]byte(sig), []byte(expectedSig))
}

// SignedURLValidator returns a validator function if the store supports signed URLs.
// Returns nil if the store doesn't use signed URLs (no signing key configured).
func (c *Client) SignedURLValidator() func(name, sig string, exp int64) bool {
	if c.opts.SigningKey == "" {
		return nil
	}
	return c.ValidateSignature
}

// GetBlob accepts a URL, reads the file, and returns the blob.
func (c *Client) GetBlob(url string) ([]byte, error) {
	b, err := os.ReadFile(filepath.Join(getDir(c.opts.UploadPath), filepath.Base(url)))
	return b, err
}

// Delete accepts a filename and removes it from disk.
func (c *Client) Delete(file string) error {
	dir := getDir(c.opts.UploadPath)
	err := os.Remove(filepath.Join(dir, file))
	if err != nil {
		return err
	}
	return nil
}

// Name returns the name of the store.
func (c *Client) Name() string {
	return "fs"
}

// GetSignedURL generates a signed URL for the file with expiration.
// This implements the SignedURLStore interface for secure public access.
func (c *Client) GetSignedURL(name string) string {
	if c.opts.SigningKey == "" {
		return fmt.Sprintf("%s%s/%s", c.opts.RootURL(), c.opts.UploadURI, name)
	}
	return c.signURL(name)
}

// getDir returns the current working directory path if no directory is specified,
// else returns the directory path specified itself.
func getDir(dir string) string {
	if dir == "" {
		dir, _ = os.Getwd()
	}
	return dir
}
