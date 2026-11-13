FROM golang:1.25-bookworm AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /scraper-api ./cmd/api

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    chromium \
    curl \
    fonts-liberation \
    poppler-utils \
    && rm -rf /var/lib/apt/lists/*

# Run as an unprivileged user. Rod launches Chromium with NoSandbox, which is
# required inside containers unless a user namespace sandbox is configured.
RUN groupadd --system scraper \
    && useradd --system --gid scraper --create-home --home-dir /home/scraper scraper \
    && mkdir -p /data/pdf_images \
    && chown -R scraper:scraper /data /home/scraper

WORKDIR /app
COPY --from=builder /scraper-api /usr/local/bin/scraper-api

ENV CHROME_PATH=/usr/bin/chromium \
    PDF_IMAGES_DIR=/data/pdf_images \
    HOME=/home/scraper
EXPOSE 9000

USER scraper

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD curl -fsS http://127.0.0.1:9000/api/health || exit 1

ENTRYPOINT ["scraper-api"]
