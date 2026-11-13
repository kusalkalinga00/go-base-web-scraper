package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAPIKeyAuth(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	t.Run("disabled", func(t *testing.T) {
		rec := httptest.NewRecorder()
		APIKeyAuth("")(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/stats", nil))
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
		}
	})

	t.Run("health bypass", func(t *testing.T) {
		rec := httptest.NewRecorder()
		APIKeyAuth("secret")(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
		}
	})

	t.Run("missing key", func(t *testing.T) {
		rec := httptest.NewRecorder()
		APIKeyAuth("secret")(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/stats", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
	})

	t.Run("header key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
		req.Header.Set("X-API-Key", "secret")
		rec := httptest.NewRecorder()
		APIKeyAuth("secret")(next).ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
		}
	})

	t.Run("bearer key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
		req.Header.Set("Authorization", "Bearer secret")
		rec := httptest.NewRecorder()
		APIKeyAuth("secret")(next).ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
		}
	})
}

func TestRateLimit(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})
	limited := RateLimit(2)(next)

	call := func() int {
		req := httptest.NewRequest(http.MethodPost, "/api/scrape", nil)
		req.RemoteAddr = "203.0.113.9:1234"
		rec := httptest.NewRecorder()
		limited.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := call(); code != http.StatusAccepted {
		t.Fatalf("first request = %d, want %d", code, http.StatusAccepted)
	}
	if code := call(); code != http.StatusAccepted {
		t.Fatalf("second request = %d, want %d", code, http.StatusAccepted)
	}
	if code := call(); code != http.StatusTooManyRequests {
		t.Fatalf("third request = %d, want %d", code, http.StatusTooManyRequests)
	}

	// Read-only requests are not limited.
	req := httptest.NewRequest(http.MethodGet, "/api/scrape", nil)
	req.RemoteAddr = "203.0.113.9:1234"
	rec := httptest.NewRecorder()
	limited.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("GET = %d, want %d", rec.Code, http.StatusAccepted)
	}
}
