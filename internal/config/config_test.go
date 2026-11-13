package config

import "testing"

func TestTimeoutsAndAuth(t *testing.T) {
	t.Setenv("JOB_TIMEOUT_SECS", "90")
	t.Setenv("SHUTDOWN_GRACE_SECS", "5")
	t.Setenv("API_KEY", "secret")
	t.Setenv("RATE_LIMIT_PER_MINUTE", "42")

	cfg := FromEnv()
	if cfg.JobTimeout.Seconds() != 90 {
		t.Errorf("job timeout = %s, want 90s", cfg.JobTimeout)
	}
	if cfg.ShutdownGrace.Seconds() != 5 {
		t.Errorf("shutdown grace = %s, want 5s", cfg.ShutdownGrace)
	}
	if cfg.APIKey != "secret" {
		t.Errorf("api key = %q", cfg.APIKey)
	}
	if cfg.RateLimitPerMinute != 42 {
		t.Errorf("rate limit = %d, want 42", cfg.RateLimitPerMinute)
	}
}

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
