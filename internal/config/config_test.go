package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestTokenIsPrivateAndRotatesOnlyOnRequest(t *testing.T) {
	home := t.TempDir()
	first, err := EnsureToken(home, false)
	if err != nil {
		t.Fatal(err)
	}
	again, err := EnsureToken(home, false)
	if err != nil {
		t.Fatal(err)
	}
	if first != again {
		t.Fatal("token changed without an explicit rotation")
	}
	rotated, err := EnsureToken(home, true)
	if err != nil {
		t.Fatal(err)
	}
	if rotated == first {
		t.Fatal("token did not rotate")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(home, TokenFileName))
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("token mode = %o, want 600", got)
		}
	}
}

func TestConfigRejectsPublicListenAddress(t *testing.T) {
	cfg := Default()
	cfg.Listen = "0.0.0.0:8765"
	cfg.KataGoPath = "/tmp/katago"
	cfg.ModelPath = "/tmp/model"
	cfg.AnalysisConfig = "/tmp/config"
	cfg.CloudflaredPath = "/tmp/cloudflared"
	if err := cfg.Validate(); err == nil {
		t.Fatal("public listen address was accepted")
	}
}
