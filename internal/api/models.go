package api

import (
	"encoding/json"
	"time"

	"go-base-web-scraper/internal/crawler"
	"go-base-web-scraper/internal/db"
)

type ScrapeRequest struct {
	URL         string `json:"url"`
	Mode        string `json:"mode"`
	Limit       *int   `json:"limit"`
	WaitSeconds *int   `json:"wait_seconds"`
	MainContent bool   `json:"main_content"`
}

func (r *ScrapeRequest) defaults() {
	if r.Mode == "" {
		r.Mode = "scrape"
	}
	if r.Limit == nil {
		v := 10
		r.Limit = &v
	}
	if r.WaitSeconds == nil {
		v := 3
		r.WaitSeconds = &v
	}
}

func (r ScrapeRequest) pageLimit() int {
	if r.Limit == nil {
		return 10
	}
	return *r.Limit
}

func (r ScrapeRequest) waitSeconds() int {
	if r.WaitSeconds == nil {
		return 3
	}
	return *r.WaitSeconds
}

type ScrapeResponse struct {
	TaskID    string `json:"task_id"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

type ResultMetadata struct {
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
	UsedProxy     bool     `json:"used_proxy"`
	CrawledAt     string   `json:"crawled_at"`
}

type ResultItem struct {
	URL            string          `json:"url"`
	HTML           string          `json:"html"`
	HTMLBytes      int             `json:"html_bytes"`
	ResponseTimeMS int             `json:"response_time_ms"`
	Metadata       ResultMetadata  `json:"metadata"`
	Assets         []crawler.Asset `json:"assets"`
}

type JobDetailResponse struct {
	TaskID       string       `json:"task_id"`
	Status       string       `json:"status"`
	URL          string       `json:"url"`
	Mode         string       `json:"mode"`
	PagesCrawled int          `json:"pages_crawled"`
	CreatedAt    string       `json:"created_at"`
	StartedAt    *string      `json:"started_at"`
	CompletedAt  *string      `json:"completed_at"`
	DurationMS   *int         `json:"duration_ms"`
	Error        *string      `json:"error"`
	Results      []ResultItem `json:"results"`
}

type JobListResponse struct {
	Jobs   []db.Job `json:"jobs"`
	Total  int64    `json:"total"`
	Limit  int      `json:"limit"`
	Offset int      `json:"offset"`
}

type HealthResponse struct {
	Status   string `json:"status"`
	Redis    string `json:"redis"`
	Postgres string `json:"postgres"`
}

type StatsResponse struct {
	Jobs       db.JobStats `json:"jobs"`
	QueueDepth int64       `json:"queue_depth"`
	MaxWorkers int         `json:"max_workers"`
}

type QueueStatusResponse struct {
	QueueDepth     int64               `json:"queue_depth"`
	MaxWorkers     int                 `json:"max_workers"`
	RunningJobs    int64               `json:"running_jobs"`
	QueuedJobs     int64               `json:"queued_jobs"`
	RecentActivity []db.RecentActivity `json:"recent_activity"`
}

type SystemConfig struct {
	Port            uint16 `json:"port"`
	MaxWorkers      int    `json:"max_workers"`
	ProxyConfigured bool   `json:"proxy_configured"`
	ProxyMode       string `json:"proxy_mode"`
	DatabaseURL     string `json:"database_url"`
	RedisURL        string `json:"redis_url"`
}

type SystemInfoResponse struct {
	Health       HealthResponse `json:"health"`
	Config       SystemConfig   `json:"config"`
	RecentErrors []db.Job       `json:"recent_errors"`
}

type CleanupResponse struct {
	RemovedCount uint32 `json:"removed_count"`
	FreedBytes   uint64 `json:"freed_bytes"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func jobDetailFrom(job db.Job, results []db.JobResult) JobDetailResponse {
	items := make([]ResultItem, 0, len(results))
	for _, r := range results {
		assets := []crawler.Asset{}
		if len(r.AssetsJSON) > 0 {
			_ = json.Unmarshal(r.AssetsJSON, &assets)
		}
		if assets == nil {
			assets = []crawler.Asset{}
		}
		headings := r.Headings
		if headings == nil {
			headings = []string{}
		}
		items = append(items, ResultItem{
			URL:            r.URL,
			HTML:           r.HTML,
			HTMLBytes:      len(r.HTML),
			ResponseTimeMS: r.ResponseTimeMS,
			Metadata: ResultMetadata{
				Title:         r.Title,
				Description:   r.Description,
				Language:      r.Language,
				CanonicalURL:  r.CanonicalURL,
				OGImage:       r.OGImage,
				Favicon:       r.Favicon,
				WordCount:     r.WordCount,
				LinksInternal: r.LinksInternal,
				LinksExternal: r.LinksExternal,
				ImagesCount:   r.ImagesCount,
				Headings:      headings,
				UsedProxy:     r.UsedProxy,
				CrawledAt:     r.CrawledAt.UTC().Format(time.RFC3339),
			},
			Assets: assets,
		})
	}

	return JobDetailResponse{
		TaskID:       job.ID,
		Status:       job.Status,
		URL:          job.URL,
		Mode:         job.Mode,
		PagesCrawled: job.PagesCrawled,
		CreatedAt:    job.CreatedAt.UTC().Format(time.RFC3339),
		StartedAt:    formatPtr(job.StartedAt),
		CompletedAt:  formatPtr(job.CompletedAt),
		DurationMS:   durationMS(job.StartedAt, job.CompletedAt),
		Error:        job.Error,
		Results:      items,
	}
}

func durationMS(started, completed *time.Time) *int {
	if started == nil || completed == nil {
		return nil
	}
	ms := int(completed.Sub(*started).Milliseconds())
	return &ms
}

func formatPtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}
