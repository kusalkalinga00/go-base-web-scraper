package worker

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"go-base-web-scraper/internal/config"
	"go-base-web-scraper/internal/crawler"
	"go-base-web-scraper/internal/db"
	"go-base-web-scraper/internal/pdf"
	"go-base-web-scraper/internal/queue"
)

func Start(ctx context.Context, pool *pgxpool.Pool, rdb *redis.Client, cfg config.Config) {
	for i := 0; i < cfg.MaxWorkers; i++ {
		id := i
		go func() {
			slog.Info("Worker started", "worker", id)
			loop(ctx, pool, rdb, cfg, id)
		}()
	}
}

func loop(ctx context.Context, pool *pgxpool.Pool, rdb *redis.Client, cfg config.Config, workerID int) {
	for {
		select {
		case <-ctx.Done():
			slog.Info("Worker stopping", "worker", workerID)
			return
		default:
		}

		taskID, err := queue.Dequeue(ctx, rdb, 5*time.Second)
		if err != nil {
			if errors.Is(err, redis.Nil) || errors.Is(err, context.Canceled) {
				continue
			}
			slog.Error("Dequeue error", "worker", workerID, "err", err)
			time.Sleep(5 * time.Second)
			continue
		}

		slog.Info("Picked up job", "worker", workerID, "task_id", taskID)
		process(ctx, pool, cfg, workerID, taskID)
	}
}

func process(ctx context.Context, pool *pgxpool.Pool, cfg config.Config, workerID int, taskID string) {
	job, err := db.GetJob(ctx, pool, taskID)
	if err != nil {
		slog.Error("Job fetch error", "worker", workerID, "err", err)
		return
	}

	if err := db.UpdateJobStatus(ctx, pool, taskID, "running", nil); err != nil {
		slog.Error("Failed to update job status", "err", err)
	}

	results, jobErr := execute(ctx, cfg, job)
	if jobErr != nil {
		msg := jobErr.Error()
		slog.Error("Job failed", "task_id", taskID, "err", jobErr)
		_ = db.UpdateJobStatus(ctx, pool, taskID, "failed", &msg)
		return
	}

	for _, cr := range results {
		var assetsJSON []byte
		if len(cr.Assets) > 0 {
			assetsJSON, _ = json.Marshal(cr.Assets)
		}
		if err := db.InsertResult(ctx, pool, taskID, cr.URL, cr.HTML, cr.Metadata, cr.ResponseTimeMS, cr.UsedProxy, assetsJSON); err != nil {
			slog.Error("Failed to insert result", "err", err)
		}
	}

	_ = db.UpdatePagesCrawled(ctx, pool, taskID, len(results))
	_ = db.UpdateJobStatus(ctx, pool, taskID, "completed", nil)
	slog.Info("Job completed", "task_id", taskID, "pages", len(results))
}

func execute(ctx context.Context, cfg config.Config, job db.Job) ([]crawler.CrawlResult, error) {
	if pdf.IsPDFURL(job.URL) || pdf.IsPDFContentType(ctx, job.URL) {
		slog.Info("Detected PDF URL", "url", job.URL)
		return pdf.ScrapePDF(ctx, job.URL, job.ID, cfg.PDFImagesDir)
	}

	switch job.Mode {
	case "scrape":
		return crawler.ScrapeSingle(job.URL, uint64(job.WaitSeconds), cfg.ProxyURL, cfg.ProxyMode, cfg.Retry, job.MainContent, cfg.ChromePath)
	case "crawl":
		return crawler.CrawlBrowser(job.URL, uint(job.PageLimit), uint64(job.WaitSeconds), cfg.ProxyURL, cfg.ProxyMode, cfg.Retry, job.MainContent, cfg.ChromePath)
	case "http":
		return crawler.CrawlHTTP(job.URL, uint(job.PageLimit), job.MainContent), nil
	default:
		return nil, errors.New("unknown mode")
	}
}
