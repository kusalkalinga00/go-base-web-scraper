package metadata

import (
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

type PageMetadata struct {
	Title         *string  `json:"title"`
	Description   *string  `json:"description"`
	Language      *string  `json:"language"`
	CanonicalURL  *string  `json:"canonical_url"`
	OGImage       *string  `json:"og_image"`
	Favicon       *string  `json:"favicon"`
	WordCount     int      `json:"word_count"`
	LinksInternal int      `json:"links_internal"`
	LinksExternal int      `json:"links_external"`
	ImagesCount   int      `json:"images_count"`
	Headings      []string `json:"headings"`
}

func Extract(html, pageURL string) PageMetadata {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return PageMetadata{Headings: []string{}}
	}

	var baseHost string
	if parsed, err := url.Parse(pageURL); err == nil {
		baseHost = parsed.Hostname()
	}

	title := firstNonEmpty(
		textOf(doc, "title"),
		attrOf(doc, `meta[property="og:title"]`, "content"),
	)
	description := firstNonEmpty(
		attrOf(doc, `meta[name="description"]`, "content"),
		attrOf(doc, `meta[property="og:description"]`, "content"),
	)
	language := attrOf(doc, "html", "lang")
	canonical := attrOf(doc, `link[rel="canonical"]`, "href")
	ogImage := attrOf(doc, `meta[property="og:image"]`, "content")
	favicon := firstNonEmpty(
		attrOf(doc, `link[rel="icon"]`, "href"),
		attrOf(doc, `link[rel="shortcut icon"]`, "href"),
	)

	bodyText := strings.TrimSpace(doc.Find("body").Text())
	wordCount := 0
	if bodyText != "" {
		wordCount = len(strings.Fields(bodyText))
	}

	var internal, external int
	doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
		href, ok := s.Attr("href")
		if !ok {
			return
		}
		abs, err := resolveURL(pageURL, href)
		if err != nil {
			return
		}
		if abs.Hostname() == baseHost {
			internal++
		} else {
			external++
		}
	})

	images := doc.Find("img").Length()

	var headings []string
	for _, tag := range []string{"h1", "h2", "h3"} {
		doc.Find(tag).Each(func(_ int, s *goquery.Selection) {
			text := strings.TrimSpace(s.Text())
			if text != "" {
				headings = append(headings, text)
			}
		})
	}
	if headings == nil {
		headings = []string{}
	}

	return PageMetadata{
		Title:         title,
		Description:   description,
		Language:      language,
		CanonicalURL:  canonical,
		OGImage:       ogImage,
		Favicon:       favicon,
		WordCount:     wordCount,
		LinksInternal: internal,
		LinksExternal: external,
		ImagesCount:   images,
		Headings:      headings,
	}
}

func textOf(doc *goquery.Document, selector string) *string {
	text := strings.TrimSpace(doc.Find(selector).First().Text())
	if text == "" {
		return nil
	}
	return &text
}

func attrOf(doc *goquery.Document, selector, attr string) *string {
	val, ok := doc.Find(selector).First().Attr(attr)
	val = strings.TrimSpace(val)
	if !ok || val == "" {
		return nil
	}
	return &val
}

func firstNonEmpty(values ...*string) *string {
	for _, v := range values {
		if v != nil && *v != "" {
			return v
		}
	}
	return nil
}

func resolveURL(base, href string) (*url.URL, error) {
	parsedBase, err := url.Parse(base)
	if err != nil {
		return url.Parse(href)
	}
	parsedHref, err := url.Parse(href)
	if err != nil {
		return nil, err
	}
	return parsedBase.ResolveReference(parsedHref), nil
}
