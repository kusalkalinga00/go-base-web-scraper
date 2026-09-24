package proxy

import (
	"strings"
	"testing"
)

func TestIsBlocked(t *testing.T) {
	if IsBlocked("<html>"+string(make([]byte, 600))+"</html>", nil) {
		t.Fatal("normal html should not be blocked")
	}
	short := "<html>hi</html>"
	if !IsBlocked(short, nil) {
		t.Fatal("short html should be treated as blocked")
	}
	code := 403
	if !IsBlocked("ok content that is long enough to pass the length check.....", &code) {
		t.Fatal("403 should be blocked")
	}
	if !IsBlocked("Please wait, checking your browser before continuing", nil) {
		t.Fatal("cloudflare challenge should be blocked")
	}
}

func TestBlockReasonIgnoresGenericPhrasesOnLargePages(t *testing.T) {
	html := `<html><body>` + strings.Repeat("hotel content ", 2000) +
		`<noscript>To use our website, you need to enable JavaScript and cookies in your web browser.</noscript>` +
		`<p>Just a moment...</p></body></html>`
	if blocked, reason := BlockReason(html, nil); blocked {
		t.Fatalf("large page with generic challenge phrases should not be blocked, got %q", reason)
	}
}

func TestBlockReasonWeakPatternOnSmallPages(t *testing.T) {
	html := `<html><body>` + strings.Repeat("x", 600) +
		`Please enable JavaScript and cookies to continue</body></html>`
	blocked, reason := BlockReason(html, nil)
	if !blocked {
		t.Fatal("small page with enable-js noscript should be blocked")
	}
	if reason != "enable javascript and cookies" {
		t.Fatalf("reason = %q", reason)
	}

	cf := `<html><body>` + strings.Repeat("x", 600) + `Just a moment...</body></html>`
	blocked, reason = BlockReason(cf, nil)
	if !blocked {
		t.Fatal("small cloudflare interstitial should be blocked")
	}
	if reason != "just a moment..." {
		t.Fatalf("reason = %q", reason)
	}
}

func TestBlockReasonStrongPatternOnLargePages(t *testing.T) {
	html := strings.Repeat("content ", 5000) + " verify you are a human "
	blocked, reason := BlockReason(html, nil)
	if !blocked {
		t.Fatal("captcha text should be blocked even on large html")
	}
	if reason != "verify you are a human" {
		t.Fatalf("reason = %q", reason)
	}
}
