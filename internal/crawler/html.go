package crawler

import (
	"strings"

	"github.com/PuerkitoBio/goquery"

	"go-base-web-scraper/internal/metadata"
)

// MainContentHTML removes layout chrome but leaves the rest of the document
// markup intact, including inline scripts such as JSON-LD blocks.
func MainContentHTML(html string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return html
	}
	return MainContentHTMLDoc(doc, html)
}

// MainContentHTMLDoc strips layout chrome from an already parsed document.
func MainContentHTMLDoc(doc *goquery.Document, fallback string) string {
	doc.Find("nav, footer, aside, header, form, iframe, [role='navigation'], .sidebar, .footer, #footer, #nav, #header").Remove()

	out, err := doc.Html()
	if err != nil {
		return fallback
	}
	return out
}

// extractHTML parses the page once and returns its metadata plus the HTML to
// store (optionally with layout chrome removed).
func extractHTML(html, pageURL string, mainContent bool) (metadata.PageMetadata, string) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return metadata.PageMetadata{Headings: []string{}}, html
	}

	meta := metadata.ExtractDoc(doc, pageURL)
	if mainContent {
		html = MainContentHTMLDoc(doc, html)
	}
	return meta, html
}
