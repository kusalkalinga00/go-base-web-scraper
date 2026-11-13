package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"go-base-web-scraper/internal/config"
	"go-base-web-scraper/internal/crawler"
	"go-base-web-scraper/internal/db"
	"go-base-web-scraper/internal/pdf"
	"go-base-web-scraper/internal/queue"
	"go-base-web-scraper/internal/urlguard"
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
	consumer := fmt.Sprintf("worker-%d", workerID)
	for {
		select {
		case <-ctx.Done():
			slog.Info("Worker stopping", "worker", workerID)
			return
		default:
		}

		// Take back jobs whose workers died before acknowledging them.
		claimed, err := queue.Reclaim(ctx, rdb, consumer, 1)
		if err != nil {
			slog.Warn("Reclaim error", "worker", workerID, "err", err)
		}
		for _, msg := range claimed {
			slog.Info("Reclaimed job", "worker", workerID, "task_id", msg.TaskID)
			runJob(ctx, pool, cfg, workerID, rdb, msg)
		}

		msg, err := queue.Dequeue(ctx, rdb, consumer, 5*time.Second)
		if err != nil {
			if errors.Is(err, redis.Nil) || errors.Is(err, context.Canceled) {
				continue
			}
			slog.Error("Dequeue error", "worker", workerID, "err", err)
			time.Sleep(5 * time.Second)
			continue
		}

		slog.Info("Picked up job", "worker", workerID, "task_id", msg.TaskID)
		runJob(ctx, pool, cfg, workerID, rdb, msg)
	}
}

func runJob(ctx context.Context, pool *pgxpool.Pool, cfg config.Config, workerID int, rdb *redis.Client, msg queue.Message) {
	process(ctx, pool, cfg, workerID, msg.TaskID)
	// Acknowledge even after a failed job: retrying it immediately would loop
	// forever. Failed jobs stay visible in the database.
	if err := queue.Ack(ctx, rdb, msg.ID); err != nil {
		slog.Error("Ack error", "worker", workerID, "task_id", msg.TaskID, "err", err)
	}
}

func process(ctx context.Context, pool *pgxpool.Pool, cfg config.Config, workerID int, taskID string) {
	job, err := db.GetJob(ctx, pool, taskID)
	if err != nil {
		slog.Error("Job fetch error", "worker", workerID, "err", err)
		return
	}

	jobCtx, cancel := context.WithTimeout(ctx, cfg.JobTimeout)
	defer cancel()

	if _, err := urlguard.Validate(jobCtx, job.URL); err != nil {
		msg := err.Error()
		slog.Error("Rejected unsafe job", "task_id", taskID, "err", err)
		_ = db.UpdateJobStatus(ctx, pool, taskID, "failed", &msg)
		return
	}

	if err := db.UpdateJobStatus(ctx, pool, taskID, "running", nil); err != nil {
		slog.Error("Failed to update job status", "err", err)
	}

	results, jobErr := execute(jobCtx, cfg, job)
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
		return crawler.ScrapeSingle(ctx, job.URL, uint64(job.WaitSeconds), cfg.ProxyURL, cfg.ProxyMode, cfg.Retry, job.MainContent, cfg.ChromePath)
	case "crawl":
		return crawler.CrawlBrowser(ctx, job.URL, uint(job.PageLimit), uint64(job.WaitSeconds), cfg.ProxyURL, cfg.ProxyMode, cfg.Retry, job.MainContent, cfg.ChromePath)
	case "http":
		return crawler.CrawlHTTP(ctx, job.URL, uint(job.PageLimit), job.MainContent), nil
	default:
		return nil, errors.New("unknown mode")
	}
}
