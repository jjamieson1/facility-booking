// Package media stores uploaded waiver / insurance documents securely, following
// skills/Security.md: the type is decided from the bytes (not the client name or
// header), only an explicit whitelist is accepted, images are re-encoded to strip
// embedded payloads, files get a random name written 0644 into a directory
// OUTSIDE any web root, and are served only by streaming back through the app.
package media

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// MaxUploadBytes caps a single upload (defense-in-depth beyond the HTTP layer).
const MaxUploadBytes = 10 << 20 // 10 MiB

// MaxImagePixels guards against decompression bombs.
const MaxImagePixels = 25_000_000 // 25 MP

var (
	ErrTooLarge     = errors.New("media: file too large")
	ErrUnsupported  = errors.New("media: unsupported file type (allowed: PDF, PNG, JPEG, GIF)")
	ErrImageTooBig  = errors.New("media: image dimensions too large")
	ErrDecodeFailed = errors.New("media: could not decode image")
)

// Stored describes a file written to disk.
type Stored struct {
	Name        string // random on-disk filename (with extension)
	ContentType string // canonical type after validation
	Size        int64
}

// Store is the on-disk media store, rooted at a directory outside the web root.
type Store struct{ dir string }

// NewStore ensures the storage directory exists (0700) and returns the store.
func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("media: create dir: %w", err)
	}
	return &Store{dir: dir}, nil
}

// Save validates the upload by its bytes, re-encodes images to canonical form,
// and writes it under a random name. r is read up to MaxUploadBytes.
func (s *Store) Save(r io.Reader) (*Stored, error) {
	raw, err := io.ReadAll(io.LimitReader(r, MaxUploadBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxUploadBytes {
		return nil, ErrTooLarge
	}

	data, ext, ctype, err := sanitize(raw)
	if err != nil {
		return nil, err
	}

	name, err := randName(ext)
	if err != nil {
		return nil, err
	}
	// 0644: readable by the service user only for streaming; never executable and
	// not under any web-server document root.
	if err := os.WriteFile(filepath.Join(s.dir, name), data, 0o644); err != nil {
		return nil, err
	}
	return &Stored{Name: name, ContentType: ctype, Size: int64(len(data))}, nil
}

// Open returns a reader for a stored file, rejecting any name that isn't a plain
// base name (defense against path traversal on read).
func (s *Store) Open(name string) (io.ReadCloser, error) {
	if name == "" || strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
		return nil, errors.New("media: invalid name")
	}
	full := filepath.Join(s.dir, name)
	// Belt-and-suspenders: the resolved path must stay inside the store dir.
	if rel, err := filepath.Rel(s.dir, full); err != nil || strings.HasPrefix(rel, "..") {
		return nil, errors.New("media: path escapes store")
	}
	return os.Open(full)
}

// sanitize decides the type from the leading bytes, accepts only the whitelist,
// and re-encodes raster images to canonical bytes. PDFs are validated by their
// header and stored as-is. SVG and everything else are rejected.
func sanitize(raw []byte) (data []byte, ext, ctype string, err error) {
	if len(raw) >= 5 && bytes.HasPrefix(raw, []byte("%PDF-")) {
		return raw, ".pdf", "application/pdf", nil
	}
	switch sniff(raw) {
	case "image/png", "image/jpeg", "image/gif":
		return reencodeImage(raw)
	default:
		return nil, "", "", ErrUnsupported
	}
}

// sniff reports the content type from the magic bytes only (never the filename).
func sniff(raw []byte) string {
	if len(raw) >= 8 && bytes.HasPrefix(raw, []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}) {
		return "image/png"
	}
	if len(raw) >= 3 && bytes.HasPrefix(raw, []byte{0xff, 0xd8, 0xff}) {
		return "image/jpeg"
	}
	if bytes.HasPrefix(raw, []byte("GIF87a")) || bytes.HasPrefix(raw, []byte("GIF89a")) {
		return "image/gif"
	}
	return ""
}

// reencodeImage decodes then re-encodes an image so the stored bytes contain only
// data we produced — stripping trailing polyglot payloads, EXIF, and any embedded
// scripts. Dimensions are checked before a full decode to avoid decompression
// bombs.
func reencodeImage(raw []byte) (data []byte, ext, ctype string, err error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, "", "", ErrDecodeFailed
	}
	if cfg.Width*cfg.Height > MaxImagePixels {
		return nil, "", "", ErrImageTooBig
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, "", "", ErrDecodeFailed
	}
	var buf bytes.Buffer
	switch format {
	case "png":
		err, ext, ctype = png.Encode(&buf, img), ".png", "image/png"
	case "jpeg":
		err, ext, ctype = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}), ".jpg", "image/jpeg"
	case "gif":
		err, ext, ctype = gif.Encode(&buf, img, nil), ".gif", "image/gif"
	default:
		return nil, "", "", ErrUnsupported
	}
	if err != nil {
		return nil, "", "", err
	}
	return buf.Bytes(), ext, ctype, nil
}

func randName(ext string) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b) + ext, nil
}
