package pdf

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"go-base-web-scraper/internal/crawler"
	"go-base-web-scraper/internal/db"
	"go-base-web-scraper/internal/metadata"
)

const DefaultImagesDir = "data/pdf_images"

func ImagesDir(override string) string {
	if strings.TrimSpace(override) != "" {
		return override
	}
	return DefaultImagesDir
}

func IsPDFURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return strings.HasSuffix(strings.ToLower(u.Path), ".pdf")
}

func IsPDFContentType(ctx context.Context, raw string) bool {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, raw, nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	return strings.Contains(ct, "application/pdf")
}

func ScrapePDF(ctx context.Context, rawURL, taskID, imagesRoot string) ([]crawler.CrawlResult, error) {
	start := time.Now()
	body, err := downloadPDF(ctx, rawURL)
	if err != nil {
		return nil, err
	}

	imageDir := filepath.Join(ImagesDir(imagesRoot), taskID)
	apiPrefix := "/api/pdf-images/" + taskID
	parsed, err := parsePDFToMarkdown(body, imageDir, apiPrefix)
	if err != nil {
		return nil, err
	}

	wordCount := len(strings.Fields(parsed.Markdown))
	var title *string
	for _, line := range strings.Split(parsed.Markdown, "\n") {
		if strings.HasPrefix(line, "# ") {
			t := strings.TrimPrefix(line, "# ")
			title = &t
			break
		}
	}

	return []crawler.CrawlResult{{
		URL:  rawURL,
		HTML: parsed.Markdown,
		Metadata: metadata.PageMetadata{
			Title:       title,
			WordCount:   wordCount,
			ImagesCount: parsed.ImagesCount,
			Headings:    []string{},
		},
		ResponseTimeMS: int(time.Since(start).Milliseconds()),
		UsedProxy:      false,
		Assets:         parsed.Assets,
	}}, nil
}

func CleanupImages(taskID, imagesRoot string) {
	dir := filepath.Join(ImagesDir(imagesRoot), taskID)
	_ = os.RemoveAll(dir)
}

type CleanupResult struct {
	RemovedCount uint32 `json:"removed_count"`
	FreedBytes   uint64 `json:"freed_bytes"`
}

func CleanupOrphanedImages(ctx context.Context, pool *pgxpool.Pool, imagesRoot string) (CleanupResult, error) {
	base := ImagesDir(imagesRoot)
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return CleanupResult{}, nil
		}
		return CleanupResult{}, fmt.Errorf("read dir: %w", err)
	}

	var result CleanupResult
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		taskID := e.Name()
		exists, err := db.JobExists(ctx, pool, taskID)
		if err != nil {
			return result, fmt.Errorf("db query: %w", err)
		}
		if exists {
			continue
		}
		path := filepath.Join(base, taskID)
		size := dirSize(path)
		if err := os.RemoveAll(path); err == nil {
			result.RemovedCount++
			result.FreedBytes += size
		}
	}
	return result, nil
}

func CleanupAllImages(imagesRoot string) (CleanupResult, error) {
	base := ImagesDir(imagesRoot)
	if _, err := os.Stat(base); os.IsNotExist(err) {
		return CleanupResult{}, nil
	}

	entries, err := os.ReadDir(base)
	if err != nil {
		return CleanupResult{}, err
	}
	total := dirSize(base)
	count := len(entries)
	if err := os.RemoveAll(base); err != nil {
		return CleanupResult{}, fmt.Errorf("remove dir: %w", err)
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		return CleanupResult{}, fmt.Errorf("create dir: %w", err)
	}
	return CleanupResult{RemovedCount: uint32(count), FreedBytes: total}, nil
}

func downloadPDF(ctx context.Context, rawURL string) ([]byte, error) {
	client := &http.Client{Timeout: 120 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("HTTP client error: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("PDF download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("PDF download returned HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

type parseResult struct {
	Markdown    string
	Assets      []crawler.Asset
	ImagesCount int
}

func parsePDFToMarkdown(pdfBytes []byte, imageDir, urlPrefix string) (parseResult, error) {
	tmp, err := os.CreateTemp("", "scrape-*.pdf")
	if err != nil {
		return parseResult{}, fmt.Errorf("temp file error: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(pdfBytes); err != nil {
		tmp.Close()
		return parseResult{}, fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return parseResult{}, fmt.Errorf("flush temp: %w", err)
	}

	cmd := exec.Command("pdftotext", "-layout", tmpPath, "-")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return parseResult{}, fmt.Errorf("PDF parse error: %s", strings.TrimSpace(stderr.String()))
	}
	markdown := strings.TrimSpace(stdout.String())
	if markdown == "" {
		return parseResult{}, fmt.Errorf("no extractable text in PDF")
	}

	if err := os.MkdirAll(imageDir, 0o755); err != nil {
		return parseResult{}, fmt.Errorf("create image dir: %w", err)
	}

	prefix := filepath.Join(imageDir, "page")
	imgCmd := exec.Command("pdftoppm", "-png", tmpPath, prefix)
	if err := imgCmd.Run(); err != nil {
		// Image extraction is best-effort; keep the text.
	}

	entries, _ := os.ReadDir(imageDir)
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(e.Name()), "."))
		if ext == "png" || ext == "jpg" || ext == "jpeg" || ext == "gif" || ext == "webp" {
			names = append(names, e.Name())
		}
	}

	assets := make([]crawler.Asset, 0, len(names))
	var refs []string
	for _, name := range names {
		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
		assets = append(assets, crawler.Asset{
			Filename:    name,
			URL:         urlPrefix + "/" + name,
			ContentType: contentTypeFor(ext),
		})
		refs = append(refs, fmt.Sprintf("<image: %s>", name))
	}

	if len(refs) > 0 {
		markdown = markdown + "\n\n" + strings.Join(refs, "\n")
	}

	if len(assets) == 0 {
		_ = os.RemoveAll(imageDir)
	}

	return parseResult{
		Markdown:    markdown,
		Assets:      assets,
		ImagesCount: len(assets),
	}, nil
}

func contentTypeFor(ext string) string {
	switch ext {
	case "png":
		return "image/png"
	case "jpg", "jpeg":
		return "image/jpeg"
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}

func dirSize(path string) uint64 {
	var total uint64
	_ = filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		total += uint64(info.Size())
		return nil
	})
	return total
}
