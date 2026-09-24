package crawler

import (
	"strings"
	"testing"
)

func TestMainContentHTML(t *testing.T) {
	html := `<html><body><nav>skip</nav><h1>Title</h1>` +
		`<script type="application/ld+json">{"name":"Hotel"}</script></body></html>`

	out := MainContentHTML(html)
	if strings.Contains(out, "skip") {
		t.Fatalf("nav should be stripped: %s", out)
	}
	if !strings.Contains(out, "<h1>Title</h1>") {
		t.Fatalf("main markup should survive: %s", out)
	}
	if !strings.Contains(out, `"name":"Hotel"`) {
		t.Fatalf("json-ld should survive: %s", out)
	}
}
