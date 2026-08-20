package media

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestSaveAcceptsAndReencodesPNG(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// A PNG with an appended "polyglot" payload.
	raw := append(pngBytes(t, 10, 10), []byte("<script>alert(1)</script>")...)
	s, err := store.Save(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("save png: %v", err)
	}
	if s.ContentType != "image/png" {
		t.Errorf("content type = %s, want image/png", s.ContentType)
	}
	// Stored bytes are re-encoded → the appended payload is gone.
	rc, _ := store.Open(s.Name)
	defer rc.Close()
	stored := new(bytes.Buffer)
	stored.ReadFrom(rc)
	if bytes.Contains(stored.Bytes(), []byte("<script>")) {
		t.Error("re-encode did not strip the appended payload")
	}
}

func TestSaveAcceptsPDFByMagic(t *testing.T) {
	store, _ := NewStore(t.TempDir())
	pdf := append([]byte("%PDF-1.7\n"), []byte("...body...")...)
	s, err := store.Save(bytes.NewReader(pdf))
	if err != nil || s.ContentType != "application/pdf" {
		t.Fatalf("pdf save: ct=%q err=%v", s.ContentType, err)
	}
}

func TestSaveRejectsDisguisedAndSVG(t *testing.T) {
	store, _ := NewStore(t.TempDir())
	// An executable/script renamed as an image — bytes don't match any whitelist.
	if _, err := store.Save(strings.NewReader("#!/bin/sh\necho hi")); err != ErrUnsupported {
		t.Errorf("script err = %v, want ErrUnsupported", err)
	}
	// SVG is XML (can carry scripts) and must be rejected.
	if _, err := store.Save(strings.NewReader(`<svg onload="alert(1)"></svg>`)); err != ErrUnsupported {
		t.Errorf("svg err = %v, want ErrUnsupported", err)
	}
}

func TestOpenRejectsTraversal(t *testing.T) {
	store, _ := NewStore(t.TempDir())
	for _, bad := range []string{"../secret", "a/b", "..\\x", "..%2fetc"} {
		if _, err := store.Open(bad); err == nil {
			t.Errorf("Open(%q) should have been rejected", bad)
		}
	}
}
