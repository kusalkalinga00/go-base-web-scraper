package config

import "testing"

func TestProxyModeAndURL(t *testing.T) {
	t.Setenv("PROXY_MODE", "always")
	t.Setenv("PROXY_HOST", "proxy.example.com")
	t.Setenv("PROXY_PORT", "8080")
	t.Setenv("PROXY_USERNAME", "user")
	t.Setenv("PROXY_PASSWORD", "pass")
	t.Setenv("DATABASE_URL", "postgres://u:p@localhost:5432/db")

	cfg := FromEnv()
	if cfg.ProxyMode != ProxyAlways {
		t.Fatalf("mode = %s", cfg.ProxyMode)
	}
	if cfg.ProxyURL == nil || *cfg.ProxyURL != "http://user:pass@proxy.example.com:8080" {
		t.Fatalf("proxy url = %v", cfg.ProxyURL)
	}
	if got := cfg.SafeDatabaseURL(); got == "postgres://u:p@localhost:5432/db" {
		t.Fatalf("expected redacted database url, got %s", got)
	}
}
