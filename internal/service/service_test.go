package service

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildConnectionURL(t *testing.T) {
	got, err := buildConnectionURL("https://quiet-sky.trycloudflare.com", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if got != "wss://quiet-sky.trycloudflare.com/katago/secret" {
		t.Fatalf("URL = %q", got)
	}
}

func TestPrintAndWriteQR(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "state"), 0o700); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := PrintQR(&output, "wss://example.test/katago/secret"); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(output.String()) == "" {
		t.Fatal("terminal QR code is empty")
	}
	path, err := WriteQR(home, "wss://example.test/katago/secret")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Fatal("PNG QR code is empty")
	}
}
