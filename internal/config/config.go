package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type ProxyMode string

const (
	ProxyAlways   ProxyMode = "always"
	ProxyFallback ProxyMode = "fallback"
	ProxyNever    ProxyMode = "never"
)

func (m ProxyMode) String() string {
	return string(m)
}

type RetryConfig struct {
	DirectRetries  uint
	ProxyRetries   uint
	RetryDelaySecs uint64
}

type Config struct {
	Port         uint16
	RedisURL     string
	DatabaseURL  string
	ProxyURL     *string
	ProxyMode    ProxyMode
	MaxWorkers   int
	Retry        RetryConfig
	PDFImagesDir string
	ChromePath   string
}

func FromEnv() Config {
	proxyURL := buildProxyURL()

	return Config{
		Port:         envUint16("PORT", 9000),
		RedisURL:     envString("REDIS_URL", "redis://127.0.0.1:6379"),
		DatabaseURL:  envString("DATABASE_URL", "postgres://scraper:scraper@127.0.0.1:5432/scraper?sslmode=disable"),
		ProxyURL:     proxyURL,
		ProxyMode:    proxyModeFromEnv(),
		MaxWorkers:   envInt("MAX_WORKERS", 3),
		Retry: RetryConfig{
			DirectRetries:  envUint("DIRECT_RETRIES", 1),
			ProxyRetries:   envUint("PROXY_RETRIES", 1),
			RetryDelaySecs: envUint64("RETRY_DELAY_SECS", 20),
		},
		PDFImagesDir: envString("PDF_IMAGES_DIR", "data/pdf_images"),
		ChromePath:   os.Getenv("CHROME_PATH"),
	}
}

func proxyModeFromEnv() ProxyMode {
	switch strings.ToLower(os.Getenv("PROXY_MODE")) {
	case "always":
		return ProxyAlways
	case "never":
		return ProxyNever
	default:
		return ProxyFallback
	}
}

func buildProxyURL() *string {
	if full := strings.TrimSpace(os.Getenv("PROXY_URL")); full != "" {
		return &full
	}

	host := strings.TrimSpace(os.Getenv("PROXY_HOST"))
	port := strings.TrimSpace(os.Getenv("PROXY_PORT"))
	if host == "" || port == "" {
		return nil
	}

	protocol := strings.TrimSpace(os.Getenv("PROXY_PROTOCOL"))
	if protocol == "" {
		protocol = "http"
	}

	username := strings.TrimSpace(os.Getenv("PROXY_USERNAME"))
	password := strings.TrimSpace(os.Getenv("PROXY_PASSWORD"))

	var built string
	if username != "" && password != "" {
		built = fmt.Sprintf("%s://%s:%s@%s:%s", protocol, username, password, host, port)
	} else {
		built = fmt.Sprintf("%s://%s:%s", protocol, host, port)
	}
	return &built
}

func (c Config) IsProxyConfigured() bool {
	return c.ProxyURL != nil
}

func (c Config) SafeDatabaseURL() string {
	return redactURL(c.DatabaseURL)
}

func (c Config) SafeRedisURL() string {
	return redactURL(c.RedisURL)
}

func redactURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if parsed.User != nil {
		parsed.User = url.UserPassword("***", "***")
	}
	return parsed.String()
}

func envString(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func envUint16(key string, fallback uint16) uint16 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseUint(v, 10, 16); err == nil {
			return uint16(n)
		}
	}
	return fallback
}

func envUint(key string, fallback uint) uint {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseUint(v, 10, 32); err == nil {
			return uint(n)
		}
	}
	return fallback
}

func envUint64(key string, fallback uint64) uint64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseUint(v, 10, 64); err == nil {
			return n
		}
	}
	return fallback
}
