package crawler

import (
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gocolly/colly/v2"

	"go-base-web-scraper/internal/metadata"
)

const requestStartKey = "request_start"

func CrawlHTTP(startURL string, limit uint, mainContent bool) []CrawlResult {
	if limit == 0 {
		limit = 10
	}

	parsed, err := url.Parse(startURL)
	if err != nil {
		return []CrawlResult{}
	}

	c := colly.NewCollector(
		colly.AllowedDomains(parsed.Host, "www."+parsed.Hostname(), parsed.Hostname()),
		colly.MaxDepth(int(limit)+2),
		colly.Async(false),
	)
	c.IgnoreRobotsTxt = false
	_ = c.Limit(&colly.LimitRule{
		DomainGlob:  "*",
		Parallelism: 1,
		Delay:       0,
	})

	var (
		mu      sync.Mutex
		results []CrawlResult
		count   int32
	)

	c.OnRequest(func(r *colly.Request) {
		r.Ctx.Put(requestStartKey, time.Now())
	})

	c.OnResponse(func(r *colly.Response) {
		if atomic.LoadInt32(&count) >= int32(limit) {
			return
		}
		ct := strings.ToLower(r.Headers.Get("Content-Type"))
		if ct != "" && !strings.Contains(ct, "html") && !strings.Contains(ct, "text/plain") && !strings.Contains(ct, "xml") {
			return
		}
		html := string(r.Body)
		n := atomic.AddInt32(&count, 1)
		if n > int32(limit) {
			return
		}

		elapsed := 0
		if start, ok := r.Ctx.GetAny(requestStartKey).(time.Time); ok {
			elapsed = int(time.Since(start).Milliseconds())
		}

		meta := metadata.Extract(html, r.Request.URL.String())
		if mainContent {
			html = MainContentHTML(html)
		}

		mu.Lock()
		results = append(results, CrawlResult{
			URL:            r.Request.URL.String(),
			HTML:           html,
			Metadata:       meta,
			ResponseTimeMS: elapsed,
			UsedProxy:      false,
			Assets:         []Asset{},
		})
		mu.Unlock()
	})

	c.OnHTML("a[href]", func(e *colly.HTMLElement) {
		if atomic.LoadInt32(&count) >= int32(limit) {
			return
		}
		href := strings.TrimSpace(e.Attr("href"))
		if href == "" || strings.HasPrefix(href, "mailto:") || strings.HasPrefix(href, "javascript:") {
			return
		}
		link := e.Request.AbsoluteURL(href)
		if link == "" {
			return
		}
		abs, err := url.Parse(link)
		if err != nil {
			return
		}
		if abs.Hostname() != parsed.Hostname() {
			return
		}
		if isSkippablePath(abs.Path) {
			return
		}
		_ = e.Request.Visit(link)
	})

	_ = c.Visit(startURL)
	c.Wait()

	if results == nil {
		return []CrawlResult{}
	}
	if len(results) > int(limit) {
		results = results[:limit]
	}
	return results
}

func isSkippablePath(path string) bool {
	lower := strings.ToLower(path)
	skip := []string{".jpg", ".jpeg", ".png", ".gif", ".webp", ".svg", ".css", ".js", ".zip", ".mp4", ".mp3", ".woff", ".woff2"}
	for _, ext := range skip {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}
