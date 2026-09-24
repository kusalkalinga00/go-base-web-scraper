package crawler

import (
	"fmt"
	"log/slog"
	"math/rand"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"go-base-web-scraper/internal/config"
	"go-base-web-scraper/internal/metadata"
	"go-base-web-scraper/internal/proxy"
	"go-base-web-scraper/internal/stealth"
)

type Asset struct {
	Filename    string `json:"filename"`
	URL         string `json:"url"`
	ContentType string `json:"content_type"`
}

type CrawlResult struct {
	URL            string
	HTML           string
	Metadata       metadata.PageMetadata
	ResponseTimeMS int
	UsedProxy      bool
	Assets         []Asset
}

func ScrapeSingle(rawURL string, waitSecs uint64, proxyURL *string, proxyMode config.ProxyMode, retry config.RetryConfig, mainContent bool, chromePath string) ([]CrawlResult, error) {
	start := time.Now()
	html, usedProxy, err := fetchHTMLWithRetry(rawURL, waitSecs, proxyURL, proxyMode, retry, chromePath)
	if err != nil {
		return nil, err
	}

	return []CrawlResult{buildResult(rawURL, html, mainContent, int(time.Since(start).Milliseconds()), usedProxy)}, nil
}

func CrawlBrowser(startURL string, limit uint, waitSecs uint64, proxyURL *string, proxyMode config.ProxyMode, retry config.RetryConfig, mainContent bool, chromePath string) ([]CrawlResult, error) {
	baseHost := hostOf(startURL)
	useProxyDirectly := proxyMode == config.ProxyAlways && proxyURL != nil

	cfg := stealth.NewConfig()
	cfg.ChromePath = chromePath
	if useProxyDirectly {
		cfg = cfg.WithProxy(proxyURL)
	}

	session, err := stealth.Launch(cfg)
	if err != nil {
		return nil, err
	}
	defer session.Close()

	page, err := stealth.NewPage(session, cfg)
	if err != nil {
		return nil, err
	}

	visited := map[string]struct{}{}
	queue := []string{startURL}
	var results []CrawlResult

	for len(queue) > 0 && len(results) < int(limit) {
		raw := queue[0]
		queue = queue[1:]
		normalized := normalizeURL(raw)
		if _, ok := visited[normalized]; ok {
			continue
		}
		visited[normalized] = struct{}{}

		slog.Info("Fetching", "url", raw)
		start := time.Now()
		html, err := stealth.Navigate(page, raw, waitSecs, cfg)
		if err != nil {
			slog.Warn("Skip page", "url", raw, "err", err)
			continue
		}

		if blocked, reason := proxy.BlockReason(html, nil); blocked && !useProxyDirectly {
			slog.Warn("Blocked", "url", raw, "html_bytes", len(html), "reason", reason)
			if proxyMode == config.ProxyFallback && proxyURL != nil {
				if proxyHTML, ferr := fetchViaProxy(raw, waitSecs, proxyURL, retry, chromePath); ferr == nil {
					results = append(results, buildResult(raw, proxyHTML, mainContent, int(time.Since(start).Milliseconds()), true))
					queue = append(queue, extractSameHostLinks(proxyHTML, raw, baseHost, visited)...)
				} else {
					slog.Warn("Proxy fallback also failed", "url", raw, "err", ferr)
				}
			}
			continue
		}

		results = append(results, buildResult(raw, html, mainContent, int(time.Since(start).Milliseconds()), useProxyDirectly))
		queue = append(queue, extractSameHostLinks(html, raw, baseHost, visited)...)

		if len(results) >= int(limit) {
			break
		}

		delay := time.Duration(1000+rand.Intn(2000)) * time.Millisecond
		time.Sleep(delay)
	}

	if results == nil {
		results = []CrawlResult{}
	}
	return results, nil
}

func fetchHTMLWithRetry(rawURL string, waitSecs uint64, proxyURL *string, proxyMode config.ProxyMode, retry config.RetryConfig, chromePath string) (string, bool, error) {
	delay := time.Duration(retry.RetryDelaySecs) * time.Second

	if proxyMode == config.ProxyAlways && proxyURL != nil {
		html, err := retryNavigate(rawURL, waitSecs, proxyURL, retry.ProxyRetries, delay, chromePath, true)
		return html, true, err
	}

	html, err := retryNavigate(rawURL, waitSecs, nil, retry.DirectRetries, delay, chromePath, false)
	if err == nil {
		return html, false, nil
	}

	if proxyMode == config.ProxyFallback {
		if proxyURL == nil {
			return "", false, fmt.Errorf("blocked and no proxy configured")
		}
		slog.Warn("Direct attempts failed, falling back to proxy", "url", rawURL, "err", err)
		html, perr := retryNavigate(rawURL, waitSecs, proxyURL, retry.ProxyRetries, delay, chromePath, true)
		return html, true, perr
	}

	return "", false, fmt.Errorf("blocked on %s after %d attempts (proxy mode: never): %w", rawURL, retry.DirectRetries, err)
}

func retryNavigate(rawURL string, waitSecs uint64, proxyURL *string, attempts uint, delay time.Duration, chromePath string, viaProxy bool) (string, error) {
	if attempts == 0 {
		attempts = 1
	}
	var lastErr error
	for attempt := uint(1); attempt <= attempts; attempt++ {
		html, err := navigateOnce(rawURL, waitSecs, proxyURL, chromePath)
		if err == nil {
			if blocked, reason := proxy.BlockReason(html, nil); blocked {
				kind := "direct"
				if viaProxy {
					kind = "proxy"
				}
				slog.Warn("Page looks blocked",
					"url", rawURL,
					"kind", kind,
					"attempt", attempt,
					"html_bytes", len(html),
					"reason", reason,
				)
				lastErr = fmt.Errorf("blocked on attempt %d (%s, %d bytes)", attempt, reason, len(html))
			} else {
				return html, nil
			}
		} else {
			lastErr = fmt.Errorf("attempt %d failed: %w", attempt, err)
		}
		if attempt < attempts {
			kind := "direct"
			if viaProxy {
				kind = "proxy"
			}
			slog.Info("Retrying", "kind", kind, "attempt", attempt, "total", attempts, "url", rawURL)
			time.Sleep(delay)
		}
	}
	return "", lastErr
}

func navigateOnce(rawURL string, waitSecs uint64, proxyURL *string, chromePath string) (string, error) {
	cfg := stealth.NewConfig()
	cfg.ChromePath = chromePath
	if proxyURL != nil {
		cfg = cfg.WithProxy(proxyURL)
	}
	session, err := stealth.Launch(cfg)
	if err != nil {
		return "", err
	}
	defer session.Close()

	page, err := stealth.NewPage(session, cfg)
	if err != nil {
		return "", err
	}
	return stealth.Navigate(page, rawURL, waitSecs, cfg)
}

func fetchViaProxy(rawURL string, waitSecs uint64, proxyURL *string, retry config.RetryConfig, chromePath string) (string, error) {
	delay := time.Duration(retry.RetryDelaySecs) * time.Second
	return retryNavigate(rawURL, waitSecs, proxyURL, retry.ProxyRetries, delay, chromePath, true)
}

func buildResult(rawURL, html string, mainContent bool, elapsed int, usedProxy bool) CrawlResult {
	meta := metadata.Extract(html, rawURL)
	if mainContent {
		html = MainContentHTML(html)
	}
	return CrawlResult{
		URL:            rawURL,
		HTML:           html,
		Metadata:       meta,
		ResponseTimeMS: elapsed,
		UsedProxy:      usedProxy,
		Assets:         []Asset{},
	}
}

func extractSameHostLinks(html, pageURL, baseHost string, visited map[string]struct{}) []string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}
	base, err := url.Parse(pageURL)
	if err != nil {
		return nil
	}

	var links []string
	doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
		href, ok := s.Attr("href")
		if !ok {
			return
		}
		abs, err := base.Parse(href)
		if err != nil || abs.Host == "" {
			return
		}
		if abs.Scheme != "http" && abs.Scheme != "https" {
			return
		}
		if abs.Hostname() != baseHost {
			return
		}
		abs.Fragment = ""
		normalized := normalizeURL(abs.String())
		if _, ok := visited[normalized]; ok {
			return
		}
		links = append(links, abs.String())
	})
	return links
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func normalizeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.Fragment = ""
	return u.String()
}
