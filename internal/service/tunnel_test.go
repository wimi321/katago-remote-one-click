package service

import "testing"

func TestQuickTunnelURLPattern(t *testing.T) {
	line := "Visit https://quiet-sky-123.trycloudflare.com when ready"
	if got := quickTunnelURL.FindString(line); got != "https://quiet-sky-123.trycloudflare.com" {
		t.Fatalf("URL = %q", got)
	}
	for _, invalid := range []string{
		"http://quiet-sky.trycloudflare.com",
		"https://trycloudflare.com",
		"https://quiet_sky.trycloudflare.com",
		"https://quiet-sky.example.com",
	} {
		if got := quickTunnelURL.FindString(invalid); got != "" {
			t.Fatalf("matched invalid URL %q as %q", invalid, got)
		}
	}
}
