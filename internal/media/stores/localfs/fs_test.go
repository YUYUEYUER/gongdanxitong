package fs

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

type failingReader struct {
	wrote bool
}

func (r *failingReader) Read(p []byte) (int, error) {
	if !r.wrote {
		r.wrote = true
		copy(p, "partial")
		return len("partial"), nil
	}
	return 0, io.ErrUnexpectedEOF
}

func TestPutAtomicRemovesPartialFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	client := &Client{opts: Opts{UploadPath: dir}}
	_, err := client.putAtomic("final-object", &failingReader{})
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("expected write failure, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "final-object")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial final object must not exist: %v", err)
	}
	temporary, err := filepath.Glob(filepath.Join(dir, ".libredesk-upload-*"))
	if err != nil || len(temporary) != 0 {
		t.Fatalf("temporary uploads must be removed, files=%v err=%v", temporary, err)
	}
}
