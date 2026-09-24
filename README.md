# Go Web Scraper

API-only web scraping engine in Go. Submit jobs over HTTP, process them with background workers, and store raw HTML plus metadata in PostgreSQL. There is no dashboard.

## Architecture

```
Client  --POST /api/scrape-->  Chi API  --INSERT job-->  PostgreSQL
                                  |                      ^
                                  | LPUSH task_id        | results + status
                                  v                      |
                                Redis  --BRPOP-->  Workers
                                                     |
                                                     +-- scrape/crawl: Chromium (Rod + stealth)
                                                     +-- http: Colly
                                                     +-- PDF: pdftotext / pdftoppm
```

- **API** (`:9000`) — submit, poll, list, and delete scrape jobs
- **Redis** — job queue (`scrape:queue`, LPUSH/BRPOP)
- **PostgreSQL** — jobs and page results
- **Workers** — goroutines that pick jobs and scrape



## Modes


| Mode     | Behavior                                                            |
| -------- | ------------------------------------------------------------------- |
| `scrape` | Headless Chrome, single page, waits for JS                          |
| `crawl`  | Headless Chrome, follows same-host links up to `limit`              |
| `http`   | Fast HTTP crawl (Colly), respects robots.txt, no JS                 |
| PDF      | If the URL is a PDF (path or `Content-Type`), extract text + images |




## Quick start

```bash
cp .env.example .env
docker compose up --build
```

Health check:

```bash
curl http://localhost:9000/api/health
```

Submit a job:

```bash
curl -X POST http://localhost:9000/api/scrape \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://example.com","mode":"http","limit":5}'
```

Poll results:

```bash
curl http://localhost:9000/api/scrape/{task_id}
```



## Configuration


| Variable                             | Default                                                             | Purpose                                         |
| ------------------------------------ | ------------------------------------------------------------------- | ----------------------------------------------- |
| `PORT`                               | `9000`                                                              | API listen port                                 |
| `REDIS_URL`                          | `redis://127.0.0.1:6379`                                            | Redis connection                                |
| `DATABASE_URL`                       | `postgres://scraper:scraper@127.0.0.1:5432/scraper?sslmode=disable` | PostgreSQL                                      |
| `PDF_IMAGES_DIR`                     | `data/pdf_images`                                                   | Extracted PDF images                            |
| `CHROME_PATH`                        | —                                                                   | Chromium binary (`/usr/bin/chromium` in Docker) |
| `PROXY_URL` or `PROXY_HOST/PORT/...` | —                                                                   | Optional proxy                                  |
| `PROXY_MODE`                         | `fallback`                                                          | `always`, `fallback`, or `never`                |
| `MAX_WORKERS`                        | `3`                                                                 | Concurrent workers                              |
| `DIRECT_RETRIES`                     | `1`                                                                 | Direct browser retries                          |
| `PROXY_RETRIES`                      | `1`                                                                 | Proxy retries                                   |
| `RETRY_DELAY_SECS`                   | `20`                                                                | Delay between retries                           |
| `LOG_LEVEL`                          | `info`                                                              | `debug`, `info`, `warn`, `error`                |




## API

Base URL: `http://localhost:9000`

### Submit job — `POST /api/scrape`

```json
{
  "url": "https://example.com/article",
  "mode": "scrape",
  "limit": 10,
  "wait_seconds": 3,
  "main_content": false
}
```


| Field          | Type    | Default    | Description                                          |
| -------------- | ------- | ---------- | ---------------------------------------------------- |
| `url`          | string  | required   | URL to scrape                                        |
| `mode`         | string  | `"scrape"` | `scrape`, `crawl`, or `http`                         |
| `limit`        | number  | `10`       | Max pages (crawl/http)                               |
| `wait_seconds` | number  | `3`        | JS wait for scrape/crawl                             |
| `main_content` | boolean | `false`    | Strip nav/header/footer/aside from the returned HTML |


Response `202 Accepted`:

```json
{
  "task_id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "queued",
  "created_at": "2026-09-17T19:00:00Z"
}
```



### List jobs — `GET /api/scrape?limit=50&offset=0`



### Get job — `GET /api/scrape/{task_id}`

Returns status, timestamps, raw HTML, metadata, and assets.

```json
{
  "task_id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "completed",
  "duration_ms": 11840,
  "results": [
    {
      "url": "https://example.com/article",
      "html": "<!DOCTYPE html>...",
      "html_bytes": 1328897,
      "response_time_ms": 11623,
      "metadata": { "title": "...", "used_proxy": true }
    }
  ]
}
```


| Field              | Meaning                                         |
| ------------------ | ----------------------------------------------- |
| `duration_ms`      | Whole job, `started_at` → `completed_at`        |
| `response_time_ms` | Fetch time for that page (navigation + JS wait) |
| `html_bytes`       | Size of the stored HTML                         |


PDF jobs store extracted text in `html` (there is no source markup).

Statuses: `queued` → `running` → `completed` | `failed`

### Delete job — `DELETE /api/scrape/{task_id}`

Removes the job, results, and extracted PDF images.

### PDF images — `GET /api/pdf-images/{task_id}/{filename}`



### Stats — `GET /api/stats`

Job counts, average response time, queue depth, worker count.

### Queue — `GET /api/queue/status`

Queue depth, running/queued counts, recent activity.

### System — `GET /api/system`

Health, redacted config, recent failed jobs.

### Cleanup — `POST /api/cleanup?mode=orphaned|all`

Deletes orphaned or all PDF image directories.

### Health — `GET /api/health`

```json
{
  "status": "ok",
  "redis": "connected",
  "postgres": "connected"
}
```



## Local development

Requires Go 1.23+, Redis, PostgreSQL, Chromium (for scrape/crawl), and poppler-utils (for PDF).

```bash
cp .env.example .env
docker compose up -d postgres redis
go run ./cmd/api
```

