package proxy

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	shortHTMLLimit      = 500
	weakPatternMaxBytes = 20_000
)

var strongPatterns = []string{
	"verify you are a human",
	"cf-browser-verification",
	`<div id="cf-wrapper">`,
	"ray id:",
	"captcha-delivery",
	"datadome",
}

var weakPatterns = []string{
	"enable javascript and cookies",
	"just a moment...",
	"checking your browser",
	"attention required",
	"access denied",
}

func IsBlocked(html string, statusHint *int) bool {
	blocked, _ := BlockReason(html, statusHint)
	return blocked
}

var (
	tagPattern    = regexp.MustCompile(`<[^>]*>`)
	spacePattern  = regexp.MustCompile(`\s+`)
	scriptPattern = regexp.MustCompile(`(?is)<(script|style)[^>]*>.*?</(script|style)>`)
)

func BlockReason(html string, statusHint *int) (bool, string) {
	if statusHint != nil {
		switch *statusHint {
		case 403, 429, 503:
			return true, fmt.Sprintf("http_%d", *statusHint)
		}
	}

	lower := strings.ToLower(html)
	for _, p := range strongPatterns {
		if strings.Contains(lower, p) {
			return true, p
		}
	}

	if len(html) < weakPatternMaxBytes {
		for _, p := range weakPatterns {
			if strings.Contains(lower, p) {
				return true, p
			}
		}
	}

	// A short response is only suspicious when it has no visible text. Many
	// legitimate pages are small; treating every short page as blocked burned
	// retries and produced false failures.
	if len(html) < shortHTMLLimit && !hasVisibleText(html) {
		return true, "empty_html"
	}

	return false, ""
}

func hasVisibleText(html string) bool {
	text := scriptPattern.ReplaceAllString(html, " ")
	text = tagPattern.ReplaceAllString(text, " ")
	text = spacePattern.ReplaceAllString(text, " ")
	return strings.TrimSpace(text) != ""
}
