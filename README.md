# Go Web Scraper

**A personal project, built to make one boring job reliable:** give it a URL, fetch the page the way a browser would, and hand back the markup plus structured metadata — without losing the job when a worker dies.

[![CI](https://github.com/kusalkalinga00/go-base-web-scraper/actions/workflows/ci.yml/badge.svg)](https://github.com/kusalkalinga00/go-base-web-scraper/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go&logoColor=white)](go.mod)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-16-4169E1?logo=postgresql&logoColor=white)](docker-compose.yml)
[![Redis](https://img.shields.io/badge/Redis-7-DC382D?logo=redis&logoColor=white)](docker-compose.yml)

It is API-only on purpose: there is no dashboard, no queue UI, and no browser extension. A job goes in over HTTP, a worker picks it up from Redis, Chromium (or Colly, or `pdftotext`) does the fetching, and PostgreSQL holds the result. Three fetch paths share one job model, one queue, and one set of metadata fields.

The interesting part is not the scraping. It is everything around it: a stream that survives a dying worker, an SSRF guard that refuses to scrape your own metadata service, a browser pipeline that does not launch Chromium once per retry, and an API that will not accidentally send you a 10 MB JSON blob unless you ask for it.

---

## Contents

1. [What it does](#what-it-does)
2. [Architecture](#architecture)
3. [Request lifecycle](#request-lifecycle)
4. [The job queue](#the-job-queue)
5. [Fetch strategies](#fetch-strategies)
6. [Security model](#security-model)
7. [API reference](#api-reference)
8. [Configuration reference](#configuration-reference)
9. [Data model](#data-model)
10. [Repository layout](#repository-layout)
11. [Running it](#running-it)
12. [Verifying a deployment](#verifying-a-deployment)
13. [Troubleshooting](#troubleshooting)
14. [Design decisions that carry weight](#design-decisions-that-carry-weight)
15. [Known limits](#known-limits)
16. [Built with](#built-with)

---

## What it does

Four modes, one request shape:

| Mode | Engine | Good for |
| --- | --- | --- |
| `scrape` | Headless Chromium (Rod + stealth) | One JS-heavy page, rendered and waited for |
| `crawl` | Headless Chromium | Following same-site links up to `limit`, still rendered |
| `http` | Colly | Fast, JS-free crawls; respects `robots.txt` |
| PDF | `pdftotext` / `pdftoppm` | A URL that returns `application/pdf`, or ends in `.pdf` |

Every result stores:

- the raw HTML (or extracted text for PDFs);
- title, description, language, canonical URL, OG image, favicon;
- word count and `h1`–`h3` headings;
- internal/external link counts and image count;
- response time and whether the proxy was used;
- a list of extracted PDF page images, when applicable.

Jobs move through `queued → running → completed | failed` and are polled over HTTP. Deleting a job removes its results and PDF images. Expired jobs can be swept by age.

---

## Architecture

```mermaid
flowchart LR
  subgraph clients["Clients"]
    C["HTTP client<br/>curl · app · SDK"]
  end

  subgraph service["scraper service · cmd/api"]
    direction TB
    API["Chi API<br/>auth · rate limit · gzip"]
    W["Worker pool<br/>MAX_WORKERS goroutines"]
  end

  PG[("PostgreSQL<br/>jobs · results")]
  RS[("Redis Stream<br/>scrape:stream")]
  NET["Target sites"]
  PROXY["Optional proxy<br/>HTTP / SOCKS5"]
  FS[("PDF image volume")]

  C -->|REST| API
  API -->|INSERT · SELECT| PG
  API -->|XADD task id| RS
  API -->|serve images| FS
  W -->|XREADGROUP · XACK| RS
  W -->|status · results| PG
  W -->|Chromium · Colly| NET
  W -.->|blocked pages| PROXY
  PROXY --> NET
  W -->|pdftoppm| FS
```

Two processes in one binary, one stateful boundary:

**The API** is a Chi router with a small middleware stack: request IDs, real-IP, panic recovery, logging, gzip, optional API-key auth, and a per-IP rate limit on expensive writes. It validates the URL, inserts the job, adds a task ID to the stream, and returns `202`. It also serves status, results, raw HTML, stats, PDF images, and cleanup.

**The workers** are `MAX_WORKERS` goroutines in the same process. Each one has a Redis consumer name (`worker-0`, `worker-1`, …), claims tasks from the stream, fetches them, writes results, and acknowledges. A task stays pending until the ack, which is what makes a crash recoverable.

**Redis** is a stream, not a list. The consumer group is `scrape-workers`; messages are only removed after `XACK` + `XDEL`, and messages idle for 10 minutes are reclaimed by another worker.

**PostgreSQL** is the source of truth for job state and page results. Migrations are embedded in the binary and run at startup.

**Chromium** is launched by Rod with a stealth page, a randomized user agent and viewport, and a small set of `navigator` patches. It is a scraper, not a browser farm: each navigation gets one session, and retries reuse it.

---

## Request lifecycle

```mermaid
sequenceDiagram
  autonumber
  participant C as Client
  participant A as API
  participant DB as PostgreSQL
  participant R as Redis Stream
  participant W as Worker
  participant T as Target

  C->>A: POST /api/scrape {url, mode, limit}
  A->>A: validate JSON, limits, SSRF guard
  A->>DB: INSERT jobs (queued)
  A->>R: XADD scrape:stream {task_id}
  A-->>C: 202 {task_id, status: queued}

  W->>R: XREADGROUP group scrape-workers worker-N
  R-->>W: task_id (pending, not removed)
  W->>DB: UPDATE jobs SET status = running
  W->>T: navigate / crawl / download
  T-->>W: HTML · text · images
  W->>DB: INSERT results
  W->>DB: UPDATE jobs SET status = completed
  W->>R: XACK + XDEL

  C->>A: GET /api/scrape/{task_id}
  A->>DB: SELECT job + results
  A-->>C: JSON (HTML only with include_html=true)

  C->>A: GET .../results/{result_id}/html
  A-->>C: raw HTML as a sandboxed attachment
```

### The flow, in words

1. **Submit.** `POST /api/scrape` decodes a strict JSON body capped at 1 MiB. `mode`, `limit`, and `wait_seconds` are validated (defaults: `scrape`, 10, 3). The URL must be absolute `http(s)` and resolve only to public addresses.
2. **Persist first.** The job row is inserted as `queued` before it is queued. If Redis rejects the `XADD`, the row is marked `failed` instead of sitting in `queued` forever.
3. **Queue.** A task ID is appended to `scrape:stream`. Redis returns immediately; the API returns `202 Accepted` with the task ID.
4. **Claim.** A worker blocks on `XREADGROUP` for up to 5 seconds. The message becomes pending for that consumer — it is still in the stream.
5. **Run.** The worker loads the job, re-validates the URL, sets it to `running`, and executes with a per-job deadline (`JOB_TIMEOUT_SECS`, default 300 s). A PDF is detected by path suffix or a `HEAD` content-type probe; otherwise the requested mode runs.
6. **Store.** Each page is inserted into `results`. `pages_crawled`, `completed_at`, and the final status are written. The most common failure is a fetch error, timeout, or rejected URL; the error string is stored on the job.
7. **Acknowledge.** `XACK` marks the message handled and `XDEL` removes it. Failed jobs are acknowledged too — retrying them automatically would loop forever.
8. **Read.** `GET /api/scrape/{task_id}` returns the job and its results. HTML is omitted by default because pages are routinely megabytes; use `?include_html=true` or the raw HTML endpoint.

On shutdown the HTTP server stops first, then workers stop dequeuing and are given `SHUTDOWN_GRACE_SECS` (default 20 s) to finish. In-flight jobs use a detached context, so a canceled signal does not abort them mid-write.

---

## The job queue

```mermaid
stateDiagram-v2
  [*] --> queued: POST /api/scrape
  queued --> running: worker claims task
  queued --> failed: XADD failed
  running --> completed: results stored
  running --> failed: fetch error / timeout / unsafe URL
  running --> queued: process restart recovery
  completed --> [*]: DELETE or retention cleanup
  failed --> [*]: DELETE or retention cleanup
```

The queue is a Redis Stream with one consumer group:

| Piece | Value |
| --- | --- |
| Stream | `scrape:stream` |
| Consumer group | `scrape-workers` |
| Consumers | `worker-0` … `worker-N` |
| Reclaim threshold | 10 minutes idle |
| Ack | `XACK` + `XDEL` |
| Startup recovery | claim all pending, requeue, reset `running` jobs |

Why a stream instead of `LPUSH`/`BRPOP`:

- **Nothing is lost at dequeue.** `BRPOP` deletes the task the moment a worker sees it. A crash then leaves a job `queued` in Postgres with no message anywhere. A stream keeps the message pending until the work is acknowledged.
- **A dead consumer is recoverable.** Every loop iteration calls `XAUTOCLAIM` for messages idle longer than 10 minutes and processes them. The job timeout (5 minutes by default) is deliberately shorter, so an in-flight job is never stolen.
- **Restart is explicit.** On boot, pending messages are moved back to the stream and jobs stuck in `running` are reset to `queued` before any worker starts.

The cost is at-least-once delivery: a crash between writing results and acknowledging can cause the job to run again. Results have no `(job_id, url)` uniqueness constraint, so duplicates are possible. That is a deliberate trade for not losing work; see [known limits](#known-limits).

---

## Fetch strategies

### Browser (`scrape`, `crawl`)

```mermaid
flowchart TD
  S["Job: scrape or crawl"] --> M{"PROXY_MODE"}
  M -->|always| PX["Launch Chromium via proxy"]
  M -->|fallback| DR["Launch Chromium direct"]
  M -->|never| DR
  DR --> NAV["Navigate · WaitLoad · wait_seconds"]
  PX --> NAV
  NAV --> B{"Blocked?<br/>challenge patterns or empty document"}
  B -->|no| OK["Return HTML"]
  B -->|retry left| NAV
  B -->|fallback + direct| PF["Relaunch via proxy"]
  PF --> NAV
  B -->|no retries left| FAIL["Job failed"]
```

- One Chromium session and one page per navigation, reused across retry attempts.
- Images, fonts, video, audio, and icons are blocked via CDP; they are not part of the extracted HTML and often dominate page weight.
- `scrape` fetches exactly one page. `crawl` does a breadth-first walk of same-host links, normalizes fragments, skips already-visited URLs, and sleeps 1–3 s between pages.
- `BLOCKED` detection looks for Cloudflare/DataDome/CAPTCHA markers, generic challenge phrases on small responses, and short responses with no visible text. A legitimate short page is not treated as blocked.
- Retries are policy-driven: `DIRECT_RETRIES` direct attempts, `PROXY_RETRIES` proxy attempts, `RETRY_DELAY_SECS` between them.

### HTTP (`http`)

Colly, no JavaScript:

- respects `robots.txt`;
- filters non-HTML responses;
- 60-second request timeout;
- one request at a time, capped by `limit`;
- same host matching treats `www.example.com` and `example.com` as one site.

Use it for static sites, sitemaps, and bulk text collection. Use `scrape`/`crawl` when the page needs a browser.

### PDF

A job is treated as a PDF when the path ends in `.pdf` or a `HEAD` request reports `application/pdf`.

- Download is capped at 64 MiB.
- Text is extracted with `pdftotext -layout` and stored in the `html` column (there is no source markup for a PDF).
- Page images are rendered with `pdftoppm` at 100 DPI, up to 100 pages, and stored under `PDF_IMAGES_DIR/{task_id}`.
- Images are listed in `assets` and served from `GET /api/pdf-images/{task_id}/{filename}`.
- A scanned PDF with no text layer fails with `no extractable text`; OCR is not included.

### Proxy modes

| Mode | Behaviour |
| --- | --- |
| `always` | Every browser request goes through the proxy. If no proxy is configured, requests go direct. |
| `fallback` | Try direct first; switch to the proxy when the page looks blocked. In `crawl`, the fallback is per page. |
| `never` | Never use a proxy, even if one is configured. |

Proxy credentials can be a single `PROXY_URL` or separate `PROXY_HOST`, `PROXY_PORT`, `PROXY_USERNAME`, `PROXY_PASSWORD`, and `PROXY_PROTOCOL`. The browser passes credentials to Chromium separately from the proxy URL; they never appear in the rendered page or logs.

---

## Security model

Scraping arbitrary URLs from your own server is an SSRF engine unless you stop it. This project treats that as a first-class concern.

| Control | What it does |
| --- | --- |
| SSRF guard | Allows only `http`/`https`; resolves the host and rejects loopback, RFC1918, CGNAT, link-local, multicast, unspecified, `localhost`, and `.local`. Cloud metadata endpoints are blocked. Re-validated in the worker. |
| Path validation | PDF image routes require a UUID task ID and a single-segment filename; files are served with `http.ServeContent`. |
| Optional auth | With `API_KEY` set, every endpoint except `GET /api/health` requires `X-API-Key: <key>` or `Authorization: Bearer <key>`. Constant-time comparison. |
| Rate limiting | Fixed one-minute window per client IP for `POST /api/scrape` and `POST /api/cleanup` (`RATE_LIMIT_PER_MINUTE`, 0 disables). |
| Request limits | 1 MiB JSON body, unknown fields rejected, 30 s read / 2 min write / 60 s idle server timeouts. |
| Error hygiene | Database and filesystem errors are logged, never returned; clients get `internal server error`. |
| HTML serving | Raw scraped markup is sent with `Content-Disposition: attachment`, `X-Content-Type-Options: nosniff`, and a sandboxed CSP so it cannot run on the API origin. |
| Container | Runs as an unprivileged `scraper` user; Chromium runs with `--no-sandbox`, which is required in this container setup. |
| Credentials | No proxy secrets belong in the repo. `.env` is ignored; `.env.example` contains placeholders only. |

> The SSRF guard validates the submitted URL. Redirects are followed by Chromium/Colly rather than re-validated by the guard, so network-level egress rules are still the right defence in a hostile environment.

---

## API reference

Base URL: `http://localhost:9000`

All responses are JSON unless noted. Errors look like:

```json
{ "error": "limit must be between 1 and 1000" }
```

| Method | Path | Purpose |
| --- | --- | --- |
| `POST` | `/api/scrape` | Submit a job |
| `GET` | `/api/scrape?limit=50&offset=0` | List jobs (`limit` ≤ 200) |
| `GET` | `/api/scrape/{task_id}` | Job status, metadata, results |
| `GET` | `/api/scrape/{task_id}/results/{result_id}/html` | One result's raw HTML |
| `DELETE` | `/api/scrape/{task_id}` | Delete job, results, and PDF images |
| `GET` | `/api/pdf-images/{task_id}/{filename}` | Extracted PDF page image |
| `GET` | `/api/stats` | Job counts, queue depth, worker count |
| `GET` | `/api/queue/status` | Queue depth and recent activity |
| `GET` | `/api/system` | Health, redacted config, recent failures |
| `POST` | `/api/cleanup?mode=orphaned\|all\|expired` | Storage cleanup |
| `GET` | `/api/health` | Liveness/readiness (`200` or `503`) |

### Submit a job

```bash
curl -X POST http://localhost:9000/api/scrape \
  -H 'Content-Type: application/json' \
  -d '{
    "url": "https://example.com/article",
    "mode": "crawl",
    "limit": 10,
    "wait_seconds": 3,
    "main_content": false
  }'
```

| Field | Type | Default | Notes |
| --- | --- | --- | --- |
| `url` | string | required | Absolute `http(s)` URL |
| `mode` | string | `scrape` | `scrape`, `crawl`, or `http` |
| `limit` | number | `10` | Max pages, 1–1000 (ignored by `scrape`) |
| `wait_seconds` | number | `3` | Extra JS wait per navigation, 0–60 |
| `main_content` | boolean | `false` | Strip nav/header/footer/aside/form/iframe before storing |

`202 Accepted`:

```json
{
  "task_id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "queued",
  "created_at": "2026-11-13T06:01:06Z"
}
```

### Get a job

`GET /api/scrape/{task_id}`

HTML is omitted by default. Pass `?include_html=true` to inline it.

```json
{
  "task_id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "completed",
  "url": "https://example.com/article",
  "mode": "crawl",
  "pages_crawled": 1,
  "duration_ms": 1184,
  "results": [
    {
      "id": 28,
      "url": "https://example.com/",
      "html_bytes": 577,
      "response_time_ms": 16,
      "metadata": {
        "title": "Example Domain",
        "description": null,
        "language": "en",
        "canonical_url": null,
        "og_image": null,
        "favicon": "data:,",
        "word_count": 25,
        "links_internal": 0,
        "links_external": 0,
        "images_count": 0,
        "headings": [],
        "used_proxy": false,
        "crawled_at": "2026-11-13T06:01:06Z"
      },
      "assets": []
    }
  ]
}
```

| Field | Meaning |
| --- | --- |
| `duration_ms` | Whole job, `started_at` → `completed_at` |
| `response_time_ms` | Fetch time for one page (navigation + JS wait) |
| `html_bytes` | Stored HTML size, even when `html` is omitted |
| `id` | Result ID used by the raw HTML endpoint |
| `assets` | PDF page images (`filename`, `url`, `content_type`) |

PDF jobs put extracted text in `html`; there is no source markup.

### Raw HTML

```bash
curl -OJ http://localhost:9000/api/scrape/{task_id}/results/28/html
```

Returns `text/html` as an attachment with a sandboxed CSP. The result `id` comes from the job response.

### Cleanup

| Mode | Effect |
| --- | --- |
| `orphaned` (default) | Delete PDF image directories with no matching job |
| `all` | Delete every PDF image directory |
| `expired` | Delete jobs older than `days` (default 30, max 3650), plus results and images |

```bash
curl -X POST 'http://localhost:9000/api/cleanup?mode=expired&days=30'
```

Running jobs are never deleted by the expired sweep.

### Health

```json
{
  "status": "ok",
  "redis": "connected",
  "postgres": "connected"
}
```

Returns `503` with `"status": "degraded"` if either dependency is down. With `API_KEY` set, health remains unauthenticated for probes.

---

## Configuration reference

The API reads environment variables directly; it does not load `.env` itself. Docker Compose does. For local runs, export the file first (`set -a && source .env && set +a`).

### Core

| Variable | Default | Notes |
| --- | --- | --- |
| `PORT` | `9000` | API listen port |
| `REDIS_URL` | `redis://127.0.0.1:6379` | Redis connection URL |
| `DATABASE_URL` | `postgres://scraper:scraper@127.0.0.1:5432/scraper?sslmode=disable` | PostgreSQL |
| `PDF_IMAGES_DIR` | `data/pdf_images` | Extracted PDF page images |
| `CHROME_PATH` | auto-detect | Chromium binary (`/usr/bin/chromium` in Docker) |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

### Workers and queue

| Variable | Default | Notes |
| --- | --- | --- |
| `MAX_WORKERS` | `3` | Worker goroutines and concurrent Chromium instances |
| `JOB_TIMEOUT_SECS` | `300` | Whole-job deadline |
| `SHUTDOWN_GRACE_SECS` | `20` | Time in-flight jobs get to finish on SIGTERM |

### Fetching

| Variable | Default | Notes |
| --- | --- | --- |
| `DIRECT_RETRIES` | `1` | Direct browser attempts |
| `PROXY_RETRIES` | `1` | Proxy browser attempts |
| `RETRY_DELAY_SECS` | `20` | Delay between attempts |

### Proxy

| Variable | Default | Notes |
| --- | --- | --- |
| `PROXY_URL` | — | Full proxy URL; wins over the fields below |
| `PROXY_HOST` / `PROXY_PORT` | — | Built into a URL when both are set |
| `PROXY_USERNAME` / `PROXY_PASSWORD` | — | Optional basic auth |
| `PROXY_PROTOCOL` | `http` | `http` or `socks5` |
| `PROXY_MODE` | `fallback` | `always`, `fallback`, `never` |

### Security

| Variable | Default | Notes |
| --- | --- | --- |
| `API_KEY` | — | Require `X-API-Key` or bearer token when set |
| `RATE_LIMIT_PER_MINUTE` | `120` | Submissions/cleanups per IP; `0` disables |

---

## Data model

Migrations live in [`migrations/`](migrations) and are embedded into the binary. They run automatically on startup; there is no separate migrate command.

**`jobs`**

| Column | Type | Notes |
| --- | --- | --- |
| `id` | UUID | Primary key |
| `url` | TEXT | Original URL |
| `mode` | TEXT | `scrape`, `crawl`, or `http` |
| `page_limit` / `wait_seconds` | INTEGER | Request settings |
| `main_content` | BOOLEAN | Strip layout chrome |
| `status` | TEXT | `queued`, `running`, `completed`, `failed` |
| `error` | TEXT | Failure message |
| `pages_crawled` | INTEGER | Written on completion |
| `created_at` / `started_at` / `completed_at` | TIMESTAMPTZ | Timing |

Indexed on `status` and `created_at DESC`.

**`results`**

| Column | Type | Notes |
| --- | --- | --- |
| `id` | BIGSERIAL | Primary key, used by the raw HTML endpoint |
| `job_id` | UUID | `REFERENCES jobs(id) ON DELETE CASCADE` |
| `url` | TEXT | Final page URL |
| `html` | TEXT | Markup, or extracted PDF text |
| `title` … `favicon` | TEXT | Extracted metadata |
| `word_count`, `links_internal`, `links_external`, `images_count` | INTEGER | Counts |
| `headings` | JSONB | `h1`–`h3` text |
| `response_time_ms` | INTEGER | Fetch duration |
| `used_proxy` | BOOLEAN | Whether the proxy was used |
| `crawled_at` | TIMESTAMPTZ | Insert time |
| `assets` | JSONB | PDF images |

Indexed on `job_id`.

---

## Repository layout

```text
go-base-web-scraper/
├── cmd/api/main.go                  config, DB/Redis connect, workers, HTTP server, shutdown
├── internal/
│   ├── api/
│   │   ├── router.go                routes and middleware stack
│   │   ├── handlers.go              submit, poll, list, raw HTML, images, stats, cleanup
│   │   ├── models.go                request/response types and result projection
│   │   ├── middleware.go            optional API key + per-IP rate limit
│   │   └── handlers_test.go         PDF path traversal and image serving
│   ├── config/config.go             environment parsing and URL redaction
│   ├── crawler/
│   │   ├── browser.go               Rod navigation, BFS crawl, retries, proxy fallback
│   │   ├── http.go                  Colly crawler
│   │   ├── html.go                  one-parse metadata + main-content extraction
│   │   └── host.go                  bare/www host matching
│   ├── db/db.go                     pgx pool, migrations, all SQL
│   ├── metadata/metadata.go         title, description, links, headings, counts
│   ├── pdf/pdf.go                   download, pdftotext, pdftoppm, image cleanup
│   ├── proxy/proxy.go               blocked/challenge detection
│   ├── queue/queue.go               stream enqueue, dequeue, ack, reclaim, recovery
│   ├── stealth/stealth.go           Chromium launch, fingerprint patches, navigation
│   ├── urlguard/urlguard.go         SSRF validation
│   └── worker/worker.go             worker pool, job execution, drain, recovery
├── migrations/                      embedded SQL migrations
├── .github/workflows/ci.yml         gofmt · vet · tests -race · build
├── Dockerfile                       multi-stage build, non-root runtime
└── docker-compose.yml               api + redis + postgres
```

---

## Running it

### Everything in Docker

```bash
git clone https://github.com/kusalkalinga00/go-base-web-scraper.git
cd go-base-web-scraper
cp .env.example .env
docker compose up --build
```

The API is on [http://localhost:9000](http://localhost:9000). The image installs Chromium and poppler-utils, runs as a non-root user, stores PDF images in a named volume, and has a container healthcheck.

```bash
curl http://localhost:9000/api/health
```

### Local development

Requires Go 1.25+, Redis, PostgreSQL, Chromium (for `scrape`/`crawl`), and poppler-utils (for PDF).

```bash
cp .env.example .env
docker compose up -d postgres redis
set -a && source .env && set +a

# macOS: point Rod at an installed Chrome if it cannot find one
# export CHROME_PATH="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"

go run ./cmd/api
```

Submit an HTTP-mode job — no browser required:

```bash
curl -X POST http://localhost:9000/api/scrape \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://example.com","mode":"http","limit":2}'
```

### Tests and checks

```bash
gofmt -l .
go vet ./...
go test -race ./...
go build ./...
```

CI runs exactly those steps on `main` and on every pull request. There are unit tests for URL/SSRF validation, config parsing, metadata extraction, main-content stripping, block detection, rate limiting/auth, and PDF image serving. There is no database or Redis integration test in CI.

---

## Verifying a deployment

Three rungs, cheapest first.

**1. Health and config**

```bash
curl -s http://localhost:9000/api/health
curl -s http://localhost:9000/api/system
```

`/api/system` returns redacted database and Redis URLs, whether a proxy is configured, and recent failed jobs — useful for confirming the container got the environment you think it did.

**2. A real job**

```bash
TASK=$(curl -s -X POST http://localhost:9000/api/scrape \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://example.com","mode":"http","limit":1}' \
  | sed -E 's/.*"task_id":"([^"]+)".*/\1/')

curl -s "http://localhost:9000/api/scrape/$TASK" | head -c 800
curl -s "http://localhost:9000/api/scrape/$TASK?include_html=true" | head -c 800
```

Then confirm the guard and auth behave as configured:

```bash
# SSRF rejection: expect 400
curl -s -X POST http://localhost:9000/api/scrape \
  -H 'Content-Type: application/json' \
  -d '{"url":"http://169.254.169.254/latest/meta-data/"}'

# With API_KEY set: expect 401 without the header
curl -s -o /dev/null -w '%{http_code}\n' http://localhost:9000/api/stats
```

**3. A browser job and a restart**

Submit a `scrape` job, confirm `used_proxy` and `response_time_ms` look sane, then stop the process with `SIGTERM` while a job is running. On restart the job should be requeued (`Req...`/`Reset interrupted jobs` in the log) and complete without manual intervention.

---

## Troubleshooting

| Symptom | Cause | Fix |
| --- | --- | --- |
| `401 unauthorized` | `API_KEY` is set | Send `X-API-Key` or `Authorization: Bearer` |
| `429 rate limit exceeded` | Too many submissions from one IP | Wait for `Retry-After`, or raise `RATE_LIMIT_PER_MINUTE` |
| `400 url resolves to a non-public address` | SSRF guard blocked an internal host | Intentional; use a public URL or an explicit egress proxy |
| Job stays `queued` | No worker reachable, Redis down, or wrong stream/group | Check `/api/health`, `/api/queue/status`, and worker logs |
| Job fails with `blocked … no proxy configured` | Target challenged the direct request and `PROXY_MODE=fallback` has no proxy | Configure a proxy, or use `never` if direct is acceptable |
| Browser job fails in Docker | `CHROME_PATH` wrong or Chromium missing | Image sets `/usr/bin/chromium`; for custom images install Chromium |
| Chromium crashes under load | Too many browsers for the memory available | Lower `MAX_WORKERS` |
| PDF fails with `no extractable text` | Scanned PDF with no text layer | OCR is not included; handle it separately |
| PDF image returns `404` | Job deleted, volume missing, or the file was never rendered | Check the `assets` array and `PDF_IMAGES_DIR` |
| Health returns `503` | Redis or PostgreSQL is down | Fix the dependency; the body says which one |
| `GET` response is enormous | HTML was requested | Use the default response or the raw HTML endpoint |
| Duplicate results after a crash | At-least-once queue delivery | Expected; add a uniqueness constraint if you need exactly-once |
| Rate limit behaves oddly behind a proxy | Limiter keys on the client IP from `X-Forwarded-For` | Ensure your reverse proxy overwrites that header |
| Startup logs `Reset interrupted jobs` | Jobs were running when the process stopped | Normal recovery; they are requeued |
| `migrate` permissions error | DB user cannot create tables | Grant DDL rights or run migrations with an owner role |

---

## Design decisions that carry weight

**The queue is at-least-once, not best-effort.** A `BRPOP` queue loses a task when a worker dies between dequeue and completion. A consumer group keeps the task pending until the work is acknowledged, and `XAUTOCLAIM` gives it to someone else after 10 minutes. The price is possible duplicate work; the alternative is silent loss.

**Jobs are detached from shutdown, but bounded.** A canceled signal must not abort a job halfway through writing results. `context.WithoutCancel` lets the current job finish, while `JOB_TIMEOUT_SECS` keeps a hung page from holding the process open forever. Workers stop dequeuing first, then drain.

**SSRF is a validator, not a proxy.** The guard resolves the host and rejects private ranges before Chromium ever starts, and the worker re-checks because jobs can be injected into Redis by older code. Redirects are the known gap; egress filtering closes it properly.

**HTML is opt-in.** A crawl of ten pages can easily be 20 MB of JSON. Returning metadata by default, plus a separate streamed HTML endpoint, keeps pollers cheap without hiding the data.

**One parse per page.** Metadata extraction and `main_content` stripping both need a parsed DOM. Doing them separately meant parsing every page twice; `extractHTML` parses once and derives both.

**Retries reuse the browser.** Launching Chromium costs seconds and hundreds of megabytes. The retry loop launches one session and page, then repeats only the navigation. The crawl mode reuses one session for the whole walk.

**Assets are blocked in the browser.** Images, fonts, and media are not needed for text extraction and often account for most of a page's transfer. Blocking them makes navigation faster and proxy traffic cheaper.

**A short page is not automatically a block.** The first version treated every response under 500 bytes as a challenge, which broke legitimate small pages and burned retries. Now short pages are blocked only when they have no visible text.

**Proxy fallback is per page in crawl mode.** A crawl can hit a mix of open and protected pages. Falling back for the whole job would route everything through the proxy after the first challenge; per-page fallback keeps the cheap path cheap.

**PDFs are capped twice.** Download size and rendered page count are limited independently. A 2 GB PDF and a 5,000-page PDF are different failures, and both need a ceiling.

**Stealth is best-effort.** Random user agents and viewports, `navigator` patches, and disabled automation flags raise the cost of casual blocking. They do not defeat a serious anti-bot vendor, and the code does not pretend otherwise.

---

## Known limits

- **Startup recovery assumes one API/worker instance per database.** `ResetRunningJobs` requeues every `running` job, and `RecoverPending` claims all pending stream messages. Running multiple replicas against the same database will interfere with in-flight jobs. Horizontal scaling needs per-instance recovery and stream ownership.
- **Delivery is at-least-once.** A crash after inserting results but before `XACK` can produce duplicate result rows. There is no unique `(job_id, url)` constraint.
- **Redirects are not re-validated by the SSRF guard.** The submitted URL is checked; a public URL that redirects to an internal address is followed. Use network egress restrictions for hard isolation.
- **The rate limiter is in-memory and per process.** Multiple replicas each have their own window, and `X-Forwarded-For` must be sanitised by a trusted proxy.
- **Authentication is optional.** Without `API_KEY`, anyone who can reach the port can submit jobs, read results, and run cleanup. CORS allows all origins; put the API behind a gateway if it is public.
- **Browser mode ignores `robots.txt`.** Only `http` mode honours it. Scrape responsibly and within the target site's terms.
- **`main_content` is a heuristic.** It removes `nav`, `header`, `footer`, `aside`, `form`, `iframe`, and common sidebar/footer selectors. Some sites keep real content in those elements, and some layouts defeat the selector list.
- **HTML is stored as text.** Results keep markup in PostgreSQL rather than object storage. Large crawls grow the database quickly; there is no automatic retention job, so call the `expired` cleanup mode on a schedule.
- **No metrics or tracing.** Logs are structured `slog` output, and `/api/stats` computes counts with full-table queries. Prometheus and query caching are follow-ups.
- **The browser has no OCR and no PDF viewer pipeline.** Scanned PDFs and image-only pages produce no text.
- **No dashboard.** This is an API by design; results are JSON and raw HTML.

---

## Built with

| Piece | Used for |
| --- | --- |
| [Go 1.25](https://go.dev) | Service, workers, tests |
| [Chi](https://github.com/go-chi/chi) | HTTP routing and middleware |
| [pgx](https://github.com/jackc/pgx) | PostgreSQL pool and queries |
| [go-redis](https://github.com/redis/go-redis) | Streams, consumer groups, health |
| [Rod](https://github.com/go-rod/rod) + [stealth](https://github.com/go-rod/stealth) | Headless Chromium |
| [Colly](https://github.com/gocolly/colly) | JS-free HTTP crawling |
| [goquery](https://github.com/PuerkitoBio/goquery) | Metadata and main-content extraction |
| [golang-migrate](https://github.com/golang-migrate/migrate) | Embedded migrations |
| Chromium | Rendered pages |
| poppler-utils | `pdftotext`, `pdftoppm` |
| PostgreSQL 16 / Redis 7 | State and queue |
| Docker Compose | Local and container deployment |

No license file has been added to this repository yet.
