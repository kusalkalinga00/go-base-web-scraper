# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2026-11-13

First public release: an API-only scraping service with browser, HTTP, and PDF
fetch paths, a durable queue, and a security-first request pipeline.

### Added

- Scrape, crawl, and HTTP fetch modes, plus PDF text/image extraction.
- Stealth Chromium pipeline with randomized user agent/viewport and blocked
  images/fonts/media.
- Proxy support with `always`, `fallback`, and `never` modes, retries, and
  per-page fallback during crawls.
- Metadata extraction: title, description, language, canonical URL, OG image,
  favicon, word count, headings, and link/image counts.
- Optional `main_content` stripping of layout chrome.
- Redis Streams queue with a consumer group, acknowledgements, and idle-message
  reclaim.
- Startup recovery for pending stream messages and interrupted jobs.
- Per-job timeouts and graceful worker drain on shutdown.
- SSRF guard that rejects non-public addresses before fetching.
- Optional API-key authentication and per-IP rate limiting.
- Raw HTML endpoint and `include_html` opt-in for job responses.
- Expired-job cleanup for retention.
- Embedded database migrations.
- Docker image running as an unprivileged user with a healthcheck.
- CI running gofmt, vet, race tests, and build.
- README with architecture, lifecycle, queue, and browser-pipeline diagrams.

### Changed

- Job responses omit HTML by default to keep pollers cheap.
- Browser retries reuse one Chromium session.
- Metadata and main-content extraction parse each page once.
- HTTP crawl domain matching handles `www` and ports correctly.
- Short pages are no longer automatically treated as blocked.

### Fixed

- Path traversal in PDF image serving.
- Internal database/filesystem errors leaking to API clients.
- Jobs stuck in `queued` when enqueueing failed.
- Lost jobs when a worker died between dequeue and completion.
- Jobs stuck in `running` after a restart.
- False-positive block detection on legitimate short pages.
- Job list pagination and scrape limits accepting unbounded values.

### Security

- Removed real proxy credentials from `.env.example`.
- Added a sandboxed CSP and attachment disposition to scraped HTML responses.
- Added request body limits, strict JSON decoding, and server timeouts.
- Health checks return `503` when Redis or PostgreSQL is unavailable.

[Unreleased]: https://github.com/kusalkalinga00/go-base-web-scraper/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/kusalkalinga00/go-base-web-scraper/releases/tag/v0.1.0
