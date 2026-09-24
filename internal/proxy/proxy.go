package proxy

import (
	"fmt"
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

func BlockReason(html string, statusHint *int) (bool, string) {
	if statusHint != nil {
		switch *statusHint {
		case 403, 429, 503:
			return true, fmt.Sprintf("http_%d", *statusHint)
		}
	}

	if html != "" && len(html) < shortHTMLLimit {
		return true, "short_html"
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

	return false, ""
}
