package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"go-base-web-scraper/internal/config"
)

func testHandler(t *testing.T) (Handler, string) {
	t.Helper()
	root := t.TempDir()
	imagesDir := filepath.Join(root, "images")
	if err := os.MkdirAll(imagesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return Handler{Config: config.Config{PDFImagesDir: imagesDir}}, imagesDir
}

func TestServePDFImageRejectsPathTraversal(t *testing.T) {
	h, imagesDir := testHandler(t)

	// A file outside the images directory that the old implementation could
	// reach with a ".." task ID.
	secret := filepath.Join(filepath.Dir(imagesDir), "secret.png")
	if err := os.WriteFile(secret, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/pdf-images/../secret.png", nil)
	rec := httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body %q)", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if rec.Body.String() == "secret" {
		t.Fatal("traversal returned the secret file")
	}
}

func TestServePDFImageRejectsBadIDs(t *testing.T) {
	h, _ := testHandler(t)
	router := NewRouter(h)

	cases := []struct {
		path string
		want int
	}{
		{"/api/pdf-images/not-a-uuid/page.png", http.StatusBadRequest},
		{"/api/pdf-images/" + uuid.NewString() + "/page.png", http.StatusNotFound},
		{"/api/pdf-images/" + uuid.NewString() + "/..", http.StatusBadRequest},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if rec.Code != tc.want {
			t.Errorf("%s: status = %d, want %d", tc.path, rec.Code, tc.want)
		}
	}
}

func TestServePDFImageServesFile(t *testing.T) {
	h, imagesDir := testHandler(t)
	taskID := uuid.NewString()
	if err := os.MkdirAll(filepath.Join(imagesDir, taskID), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(imagesDir, taskID, "page-1.png"), []byte("png-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/pdf-images/"+taskID+"/page-1.png", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %q)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if rec.Body.String() != "png-bytes" {
		t.Fatalf("body = %q", rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("content type = %q, want image/png", got)
	}
}
