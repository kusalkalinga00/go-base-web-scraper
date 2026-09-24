package crawler

import (
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// MainContentHTML removes layout chrome but leaves the rest of the document
// markup intact, including inline scripts such as JSON-LD blocks.
func MainContentHTML(html string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return html
	}

	doc.Find("nav, footer, aside, header, form, iframe, [role='navigation'], .sidebar, .footer, #footer, #nav, #header").Remove()

	out, err := doc.Html()
	if err != nil {
		return html
	}
	return out
}
