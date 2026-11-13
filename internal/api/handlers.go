package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"go-base-web-scraper/internal/config"
	"go-base-web-scraper/internal/db"
	"go-base-web-scraper/internal/pdf"
	"go-base-web-scraper/internal/queue"
	"go-base-web-scraper/internal/urlguard"
)

type Handler struct {
	DB     *pgxpool.Pool
	Redis  *redis.Client
	Config config.Config
}

const maxSubmitBodyBytes = 1 << 20 // 1 MiB is plenty for a URL and a few options.

func (h Handler) SubmitScrape(w http.ResponseWriter, r *http.Request) {
	var req ScrapeRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxSubmitBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON: "+err.Error())
		return
	}
	req.defaults()
	if err := req.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.Mode != "scrape" && req.Mode != "crawl" && req.Mode != "http" {
		writeError(w, http.StatusBadRequest, "mode must be 'scrape', 'crawl', or 'http'")
		return
	}
	if _, err := urlguard.Validate(r.Context(), req.URL); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	taskID := uuid.NewString()
	job, err := db.CreateJob(r.Context(), h.DB, taskID, req.URL, req.Mode, req.pageLimit(), req.waitSeconds(), req.MainContent)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	if err := queue.Enqueue(r.Context(), h.Redis, taskID); err != nil {
		// The job row exists but will never be picked up; mark it failed so it
		// does not linger as "queued" forever.
		msg := "failed to enqueue job: " + err.Error()
		_ = db.UpdateJobStatus(r.Context(), h.DB, taskID, "failed", &msg)
		writeInternalError(w, r, err)
		return
	}

	writeJSON(w, http.StatusAccepted, ScrapeResponse{
		TaskID:    job.ID,
		Status:    job.Status,
		CreatedAt: job.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	})
}

func (h Handler) GetScrape(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	job, err := db.GetJob(r.Context(), h.DB, taskID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "job not found")
			return
		}
		writeInternalError(w, r, err)
		return
	}
	results, err := db.GetResults(r.Context(), h.DB, taskID)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, jobDetailFrom(job, results, queryBool(r, "include_html", false)))
}

// GetResultHTML streams the raw HTML for a single result. It is separate from
// the job endpoint so callers can fetch metadata without downloading
// megabytes of markup.
func (h Handler) GetResultHTML(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	if err := uuid.Validate(taskID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid task id")
		return
	}
	resultID, err := strconv.ParseInt(chi.URLParam(r, "resultID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid result id")
		return
	}

	html, err := db.GetResultHTML(r.Context(), h.DB, taskID, resultID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "result not found")
			return
		}
		writeInternalError(w, r, err)
		return
	}

	// Scraped markup is untrusted. Send it as an attachment with a sandboxed
	// CSP so it cannot run scripts on the API origin.
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="result-%d.html"`, resultID))
	_, _ = io.WriteString(w, html)
}

func (h Handler) ListScrapes(w http.ResponseWriter, r *http.Request) {
	limit := min(max(queryInt(r, "limit", 50), 1), MaxListLimit)
	offset := max(queryInt(r, "offset", 0), 0)
	jobs, err := db.ListJobs(r.Context(), h.DB, limit, offset)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	total, err := db.CountJobs(r.Context(), h.DB)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, JobListResponse{
		Jobs:   jobs,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	})
}

func (h Handler) DeleteScrape(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	if err := db.DeleteJob(r.Context(), h.DB, taskID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "job not found")
			return
		}
		writeInternalError(w, r, err)
		return
	}
	pdf.CleanupImages(taskID, h.Config.PDFImagesDir)
	w.WriteHeader(http.StatusNoContent)
}

func (h Handler) GetStats(w http.ResponseWriter, r *http.Request) {
	stats, err := db.GetStats(r.Context(), h.DB)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	depth, _ := queue.Length(r.Context(), h.Redis)
	writeJSON(w, http.StatusOK, StatsResponse{
		Jobs:       stats,
		QueueDepth: depth,
		MaxWorkers: h.Config.MaxWorkers,
	})
}

func (h Handler) GetQueueStatus(w http.ResponseWriter, r *http.Request) {
	stats, err := db.GetStats(r.Context(), h.DB)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	depth, _ := queue.Length(r.Context(), h.Redis)
	recent, err := db.GetRecentActivity(r.Context(), h.DB, 30)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, QueueStatusResponse{
		QueueDepth:     depth,
		MaxWorkers:     h.Config.MaxWorkers,
		RunningJobs:    stats.Running,
		QueuedJobs:     stats.Queued,
		RecentActivity: recent,
	})
}

func (h Handler) GetSystemInfo(w http.ResponseWriter, r *http.Request) {
	health := h.health(r)
	failed, err := db.GetFailedJobs(r.Context(), h.DB, 20)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, SystemInfoResponse{
		Health: health,
		Config: SystemConfig{
			Port:            h.Config.Port,
			MaxWorkers:      h.Config.MaxWorkers,
			ProxyConfigured: h.Config.IsProxyConfigured(),
			ProxyMode:       h.Config.ProxyMode.String(),
			DatabaseURL:     h.Config.SafeDatabaseURL(),
			RedisURL:        h.Config.SafeRedisURL(),
		},
		RecentErrors: failed,
	})
}

func (h Handler) CleanupStorage(w http.ResponseWriter, r *http.Request) {
	mode := strings.ToLower(r.URL.Query().Get("mode"))
	if mode == "" {
		mode = "orphaned"
	}
	switch mode {
	case "all", "orphaned":
		var (
			result pdf.CleanupResult
			err    error
		)
		if mode == "all" {
			result, err = pdf.CleanupAllImages(h.Config.PDFImagesDir)
		} else {
			result, err = pdf.CleanupOrphanedImages(r.Context(), h.DB, h.Config.PDFImagesDir)
		}
		if err != nil {
			writeInternalError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, CleanupResponse{
			RemovedCount: result.RemovedCount,
			FreedBytes:   result.FreedBytes,
		})
	case "expired":
		days := min(max(queryInt(r, "days", 30), 1), 3650)
		ids, err := db.DeleteJobsBefore(r.Context(), h.DB, time.Now().AddDate(0, 0, -days))
		if err != nil {
			writeInternalError(w, r, err)
			return
		}
		for _, id := range ids {
			pdf.CleanupImages(id, h.Config.PDFImagesDir)
		}
		slog.Info("Deleted expired jobs", "count", len(ids), "days", days)
		writeJSON(w, http.StatusOK, CleanupResponse{JobsRemoved: len(ids)})
	default:
		writeError(w, http.StatusBadRequest, "mode must be 'orphaned', 'all', or 'expired'")
	}
}

func (h Handler) HealthCheck(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.health(r))
}

func (h Handler) ServePDFImage(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	// Validate the task ID before it is used as a path segment. Chi does not
	// clean URL paths, so an unvalidated ".." here would escape the images
	// directory.
	if err := uuid.Validate(taskID); err != nil {
		http.Error(w, "bad task id", http.StatusBadRequest)
		return
	}

	filename := filepath.Base(chi.URLParam(r, "filename"))
	if filename == "" || filename == "." || filename == ".." || strings.ContainsAny(filename, `/\`) {
		http.Error(w, "bad filename", http.StatusBadRequest)
		return
	}

	path := filepath.Join(pdf.ImagesDir(h.Config.PDFImagesDir), taskID, filename)
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "read error", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, filename, info.ModTime(), file)
}

func (h Handler) health(r *http.Request) HealthResponse {
	pgOK := db.Ping(r.Context(), h.DB)
	redisOK := queue.Ping(r.Context(), h.Redis)
	status := "ok"
	if !pgOK || !redisOK {
		status = "degraded"
	}
	pg := "disconnected"
	if pgOK {
		pg = "connected"
	}
	rd := "disconnected"
	if redisOK {
		rd = "connected"
	}
	return HealthResponse{Status: status, Redis: rd, Postgres: pg}
}

func queryInt(r *http.Request, key string, fallback int) int {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return n
}

func queryBool(r *http.Request, key string, fallback bool) bool {
	switch strings.ToLower(r.URL.Query().Get(key)) {
	case "1", "true", "yes":
		return true
	case "0", "false", "no":
		return false
	default:
		return fallback
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}

// writeInternalError logs the underlying error and returns a generic message
// so database and filesystem details are not exposed to clients.
func writeInternalError(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	writeError(w, http.StatusInternalServerError, "internal server error")
}
