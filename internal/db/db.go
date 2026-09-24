package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"go-base-web-scraper/internal/metadata"
	"go-base-web-scraper/migrations"
)

func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.MaxConns = 10

	var pool *pgxpool.Pool
	var lastErr error
	for attempt := 1; attempt <= 20; attempt++ {
		pool, lastErr = pgxpool.NewWithConfig(ctx, cfg)
		if lastErr == nil {
			if pingErr := pool.Ping(ctx); pingErr == nil {
				break
			} else {
				lastErr = pingErr
				pool.Close()
				pool = nil
			}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	if pool == nil {
		return nil, fmt.Errorf("connect to postgres: %w", lastErr)
	}

	if err := runMigrations(databaseURL); err != nil {
		pool.Close()
		return nil, err
	}

	return pool, nil
}

func runMigrations(databaseURL string) error {
	source, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("migration source: %w", err)
	}

	migrateURL := databaseURL
	switch {
	case strings.HasPrefix(migrateURL, "postgres://"):
		migrateURL = "pgx5://" + strings.TrimPrefix(migrateURL, "postgres://")
	case strings.HasPrefix(migrateURL, "postgresql://"):
		migrateURL = "pgx5://" + strings.TrimPrefix(migrateURL, "postgresql://")
	}

	m, err := migrate.NewWithSourceInstance("iofs", source, migrateURL)
	if err != nil {
		return fmt.Errorf("migrate init: %w", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}

type Job struct {
	ID           string
	URL          string
	Mode         string
	PageLimit    int
	WaitSeconds  int
	Status       string
	Error        *string
	PagesCrawled int
	CreatedAt    time.Time
	StartedAt    *time.Time
	CompletedAt  *time.Time
	MainContent  bool
}

func (j Job) MarshalJSON() ([]byte, error) {
	type jobJSON struct {
		ID           string  `json:"id"`
		URL          string  `json:"url"`
		Mode         string  `json:"mode"`
		PageLimit    int     `json:"page_limit"`
		WaitSeconds  int     `json:"wait_seconds"`
		Status       string  `json:"status"`
		Error        *string `json:"error"`
		PagesCrawled int     `json:"pages_crawled"`
		CreatedAt    string  `json:"created_at"`
		StartedAt    *string `json:"started_at"`
		CompletedAt  *string `json:"completed_at"`
		MainContent  bool    `json:"main_content"`
	}
	out := jobJSON{
		ID:           j.ID,
		URL:          j.URL,
		Mode:         j.Mode,
		PageLimit:    j.PageLimit,
		WaitSeconds:  j.WaitSeconds,
		Status:       j.Status,
		Error:        j.Error,
		PagesCrawled: j.PagesCrawled,
		CreatedAt:    formatTime(j.CreatedAt),
		MainContent:  j.MainContent,
	}
	out.StartedAt = formatTimePtr(j.StartedAt)
	out.CompletedAt = formatTimePtr(j.CompletedAt)
	return json.Marshal(out)
}

type JobResult struct {
	ID             int64
	JobID          string
	URL            string
	HTML           string
	Title          *string
	Description    *string
	Language       *string
	CanonicalURL   *string
	OGImage        *string
	Favicon        *string
	WordCount      int
	LinksInternal  int
	LinksExternal  int
	ImagesCount    int
	Headings       []string
	ResponseTimeMS int
	UsedProxy      bool
	CrawledAt      time.Time
	AssetsJSON     []byte
}

type JobStats struct {
	Total              int64   `json:"total"`
	Queued             int64   `json:"queued"`
	Running            int64   `json:"running"`
	Completed          int64   `json:"completed"`
	Failed             int64   `json:"failed"`
	TotalPagesCrawled  int64   `json:"total_pages_crawled"`
	AvgResponseTimeMS  float64 `json:"avg_response_time_ms"`
	TotalResults       int64   `json:"total_results"`
}

type RecentActivity struct {
	ID           string  `json:"id"`
	URL          string  `json:"url"`
	Status       string  `json:"status"`
	Mode         string  `json:"mode"`
	PagesCrawled int     `json:"pages_crawled"`
	Error        *string `json:"error"`
	CreatedAt    string  `json:"created_at"`
	StartedAt    *string `json:"started_at"`
	CompletedAt  *string `json:"completed_at"`
}

func CreateJob(ctx context.Context, pool *pgxpool.Pool, id, rawURL, mode string, pageLimit, waitSeconds int, mainContent bool) (Job, error) {
	_, err := pool.Exec(ctx, `
		INSERT INTO jobs (id, url, mode, page_limit, wait_seconds, main_content, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, 'queued', NOW())
	`, id, rawURL, mode, pageLimit, waitSeconds, mainContent)
	if err != nil {
		return Job{}, err
	}
	return GetJob(ctx, pool, id)
}

func GetJob(ctx context.Context, pool *pgxpool.Pool, id string) (Job, error) {
	row := pool.QueryRow(ctx, `SELECT id, url, mode, page_limit, wait_seconds, status, error, pages_crawled, created_at, started_at, completed_at, main_content FROM jobs WHERE id = $1`, id)
	job, err := scanJob(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, fmt.Errorf("job %s not found", id)
	}
	return job, err
}

func ListJobs(ctx context.Context, pool *pgxpool.Pool, limit, offset int) ([]Job, error) {
	rows, err := pool.Query(ctx, `SELECT id, url, mode, page_limit, wait_seconds, status, error, pages_crawled, created_at, started_at, completed_at, main_content FROM jobs ORDER BY created_at DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []Job
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	if jobs == nil {
		jobs = []Job{}
	}
	return jobs, rows.Err()
}

func CountJobs(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	var total int64
	err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM jobs`).Scan(&total)
	return total, err
}

func UpdateJobStatus(ctx context.Context, pool *pgxpool.Pool, id, status string, jobErr *string) error {
	switch status {
	case "running":
		_, err := pool.Exec(ctx, `UPDATE jobs SET status = 'running', started_at = NOW() WHERE id = $1`, id)
		return err
	case "completed":
		_, err := pool.Exec(ctx, `UPDATE jobs SET status = 'completed', completed_at = NOW() WHERE id = $1`, id)
		return err
	case "failed":
		_, err := pool.Exec(ctx, `UPDATE jobs SET status = 'failed', error = $1, completed_at = NOW() WHERE id = $2`, jobErr, id)
		return err
	default:
		return nil
	}
}

func UpdatePagesCrawled(ctx context.Context, pool *pgxpool.Pool, id string, count int) error {
	_, err := pool.Exec(ctx, `UPDATE jobs SET pages_crawled = $1 WHERE id = $2`, count, id)
	return err
}

func InsertResult(ctx context.Context, pool *pgxpool.Pool, jobID, rawURL, html string, meta metadata.PageMetadata, responseTimeMS int, usedProxy bool, assetsJSON []byte) error {
	html = strings.ToValidUTF8(html, "")
	if assetsJSON == nil {
		assetsJSON = []byte("[]")
	}
	headings, err := json.Marshal(meta.Headings)
	if err != nil {
		headings = []byte("[]")
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO results (
			job_id, url, html, title, description, language, canonical_url, og_image, favicon,
			word_count, links_internal, links_external, images_count, headings, response_time_ms,
			used_proxy, crawled_at, assets
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,NOW(),$17)
	`, jobID, rawURL, html, meta.Title, meta.Description, meta.Language, meta.CanonicalURL, meta.OGImage, meta.Favicon,
		meta.WordCount, meta.LinksInternal, meta.LinksExternal, meta.ImagesCount, headings, responseTimeMS, usedProxy, assetsJSON)
	return err
}

func GetResults(ctx context.Context, pool *pgxpool.Pool, jobID string) ([]JobResult, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, job_id, url, html, title, description, language, canonical_url, og_image, favicon,
			word_count, links_internal, links_external, images_count, headings, response_time_ms,
			used_proxy, crawled_at, assets
		FROM results WHERE job_id = $1 ORDER BY id
	`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []JobResult
	for rows.Next() {
		var r JobResult
		var headings []byte
		if err := rows.Scan(
			&r.ID, &r.JobID, &r.URL, &r.HTML, &r.Title, &r.Description, &r.Language, &r.CanonicalURL, &r.OGImage, &r.Favicon,
			&r.WordCount, &r.LinksInternal, &r.LinksExternal, &r.ImagesCount, &headings, &r.ResponseTimeMS,
			&r.UsedProxy, &r.CrawledAt, &r.AssetsJSON,
		); err != nil {
			return nil, err
		}
		if len(headings) > 0 {
			_ = json.Unmarshal(headings, &r.Headings)
		}
		if r.Headings == nil {
			r.Headings = []string{}
		}
		results = append(results, r)
	}
	if results == nil {
		results = []JobResult{}
	}
	return results, rows.Err()
}

func GetStats(ctx context.Context, pool *pgxpool.Pool) (JobStats, error) {
	var stats JobStats
	err := pool.QueryRow(ctx, `
		SELECT
			COUNT(*),
			COALESCE(SUM(CASE WHEN status = 'queued' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'running' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(pages_crawled), 0)
		FROM jobs
	`).Scan(&stats.Total, &stats.Queued, &stats.Running, &stats.Completed, &stats.Failed, &stats.TotalPagesCrawled)
	if err != nil {
		return JobStats{}, err
	}

	err = pool.QueryRow(ctx, `SELECT COUNT(*), COALESCE(AVG(response_time_ms), 0) FROM results`).Scan(&stats.TotalResults, &stats.AvgResponseTimeMS)
	if err != nil {
		return JobStats{}, err
	}
	return stats, nil
}

func GetRecentActivity(ctx context.Context, pool *pgxpool.Pool, limit int) ([]RecentActivity, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, url, status, mode, pages_crawled, error, created_at, started_at, completed_at
		FROM jobs ORDER BY created_at DESC LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []RecentActivity
	for rows.Next() {
		var (
			id, rawURL, status, mode string
			pages                    int
			jobErr                   *string
			created                  time.Time
			started, completed       *time.Time
		)
		if err := rows.Scan(&id, &rawURL, &status, &mode, &pages, &jobErr, &created, &started, &completed); err != nil {
			return nil, err
		}
		items = append(items, RecentActivity{
			ID:           id,
			URL:          rawURL,
			Status:       status,
			Mode:         mode,
			PagesCrawled: pages,
			Error:        jobErr,
			CreatedAt:    formatTime(created),
			StartedAt:    formatTimePtr(started),
			CompletedAt:  formatTimePtr(completed),
		})
	}
	if items == nil {
		items = []RecentActivity{}
	}
	return items, rows.Err()
}

func GetFailedJobs(ctx context.Context, pool *pgxpool.Pool, limit int) ([]Job, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, url, mode, page_limit, wait_seconds, status, error, pages_crawled, created_at, started_at, completed_at, main_content
		FROM jobs WHERE status = 'failed' ORDER BY completed_at DESC NULLS LAST LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []Job
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	if jobs == nil {
		jobs = []Job{}
	}
	return jobs, rows.Err()
}

func DeleteJob(ctx context.Context, pool *pgxpool.Pool, id string) error {
	tag, err := pool.Exec(ctx, `DELETE FROM jobs WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("job %s not found", id)
	}
	return nil
}

func JobExists(ctx context.Context, pool *pgxpool.Pool, id string) (bool, error) {
	var exists bool
	err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM jobs WHERE id = $1)`, id).Scan(&exists)
	return exists, err
}

func Ping(ctx context.Context, pool *pgxpool.Pool) bool {
	return pool.Ping(ctx) == nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanJob(row rowScanner) (Job, error) {
	var j Job
	err := row.Scan(
		&j.ID, &j.URL, &j.Mode, &j.PageLimit, &j.WaitSeconds, &j.Status, &j.Error,
		&j.PagesCrawled, &j.CreatedAt, &j.StartedAt, &j.CompletedAt, &j.MainContent,
	)
	return j, err
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func formatTimePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}
