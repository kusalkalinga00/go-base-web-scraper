# Contributing

Thanks for taking the time to contribute. This project is a small, opinionated scraping service; the goal is to keep it boring, observable, and safe to operate.

By participating you agree to the [Code of Conduct](CODE_OF_CONDUCT.md). Please report security issues through the process in [SECURITY.md](SECURITY.md), not in a public issue.

## Ways to contribute

- Report a bug or a documentation gap with the issue templates.
- Propose a feature with the rationale and the operational cost in mind.
- Improve tests, error messages, or the README.
- Pick up an issue labelled `good first issue`.

For anything larger than a small fix, open an issue first so we can agree on the shape before you spend time on it.

## Development setup

Requirements: Go 1.25+, Redis, PostgreSQL, Chromium (for `scrape`/`crawl`), and poppler-utils (for PDF).

```bash
git clone https://github.com/kusalkalinga00/go-base-web-scraper.git
cd go-base-web-scraper
cp .env.example .env
docker compose up -d postgres redis
set -a && source .env && set +a
go run ./cmd/api
```

The API listens on `:9000`. The app reads environment variables directly; it does not load `.env` itself. Docker Compose does.

To run the whole stack in containers:

```bash
docker compose up --build
```

## Before you open a pull request

```bash
make check
```

`make check` runs `gofmt -l`, `go vet ./...`, `go test -race ./...`, and `go build ./...`. CI runs the same checks and they are required to merge.

Guidelines:

- Keep PRs focused. One concern per PR is easier to review and revert.
- Add or update tests for behaviour changes. The project uses table-driven tests and `httptest`; no live database is required.
- Use `slog` for logging, wrap errors with `%w`, and pass `context.Context` as the first argument.
- Do not add a dependency for something the standard library or an existing dependency already does.
- Update the README or [CHANGELOG.md](CHANGELOG.md) when behaviour, configuration, or API shape changes.
- Never commit credentials, proxy URLs with passwords, `.env` files, or generated PDFs.

## Commit style

Small commits with a prefix and an imperative subject:

```text
feat(api): add a raw HTML endpoint
fix(queue): retry dequeue once instead of recursing
perf(browser): block images, fonts and media
docs: document the security model
test: cover PDF path traversal
chore: add the MIT license
```

Explain the *why* in the body when the change is not obvious. Reference issues with `Fixes #123` or `Refs #123`.

## Pull request process

1. Fork the repo and create a branch from `main` (`feat/...`, `fix/...`, `docs/...`).
2. Make your changes and run `make check`.
3. Open a PR using the template. Describe the problem, the change, and how you verified it.
4. CI must pass. A maintainer will review; expect questions about edge cases and operational impact.
5. PRs are squash-merged, and the branch is deleted after merge.

## Licensing

The project is MIT licensed. By submitting a pull request you agree that your contribution is licensed under the same terms. There is no CLA and no copyright assignment.

## Questions

Open a discussion in an issue labelled `question` if the README and code do not answer it.
