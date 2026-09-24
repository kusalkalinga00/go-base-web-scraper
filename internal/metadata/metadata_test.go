package metadata

import "testing"

func TestExtract(t *testing.T) {
	html := `<!doctype html>
<html lang="en">
<head>
  <title>Hello</title>
  <meta name="description" content="A page">
  <link rel="canonical" href="https://example.com/hello">
</head>
<body>
  <h1>Hello</h1>
  <p>One two three</p>
  <a href="/internal">in</a>
  <a href="https://other.test/x">out</a>
  <img src="/a.png">
</body>
</html>`

	meta := Extract(html, "https://example.com/hello")
	if meta.Title == nil || *meta.Title != "Hello" {
		t.Fatalf("title = %v", meta.Title)
	}
	if meta.Language == nil || *meta.Language != "en" {
		t.Fatalf("language = %v", meta.Language)
	}
	if meta.LinksInternal != 1 || meta.LinksExternal != 1 {
		t.Fatalf("links internal=%d external=%d", meta.LinksInternal, meta.LinksExternal)
	}
	if meta.ImagesCount != 1 {
		t.Fatalf("images = %d", meta.ImagesCount)
	}
	if len(meta.Headings) != 1 || meta.Headings[0] != "Hello" {
		t.Fatalf("headings = %v", meta.Headings)
	}
	if meta.WordCount == 0 {
		t.Fatal("expected word count")
	}
}
