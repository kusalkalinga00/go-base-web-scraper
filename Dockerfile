FROM golang:1.25-bookworm AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /scraper-api ./cmd/api

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    chromium \
    fonts-liberation \
    poppler-utils \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=builder /scraper-api /usr/local/bin/scraper-api

ENV CHROME_PATH=/usr/bin/chromium
ENV PDF_IMAGES_DIR=/data/pdf_images
EXPOSE 9000

ENTRYPOINT ["scraper-api"]
