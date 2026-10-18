package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func rangeFixture(t *testing.T) (string, []byte) {
	t.Helper()
	dir := t.TempDir()
	data := make([]byte, 1<<20)
	for i := range data {
		data[i] = byte(i % 251)
	}
	p := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p, data
}

func get(t *testing.T, h http.HandlerFunc, path, rangeHeader string) (*http.Response, []byte) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/x?"+url.Values{"path": {path}}.Encode(), nil)
	if rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}
	rec := httptest.NewRecorder()
	h(rec, req)
	resp := rec.Result()
	body, _ := io.ReadAll(resp.Body)
	return resp, body
}

func TestDownloadWithoutRangeSendsTheWholeFileAndAdvertisesRanges(t *testing.T) {
	p, data := rangeFixture(t)
	resp, body := get(t, handleDownload(&Config{}), p, "")
	if resp.StatusCode != 200 || len(body) != len(data) {
		t.Fatalf("got %d, %d bytes", resp.StatusCode, len(body))
	}
	if resp.Header.Get("Accept-Ranges") != "bytes" || resp.Header.Get("Content-Length") != "1048576" {
		t.Fatalf("headers: %v", resp.Header)
	}
	if resp.Header.Get("Content-Type") != "application/octet-stream" || resp.Header.Get("Content-Disposition") == "" {
		t.Fatalf("download headers changed: %v", resp.Header)
	}
}

func TestDownloadAnswersARangeWithPartialContent(t *testing.T) {
	p, data := rangeFixture(t)
	resp, body := get(t, handleDownload(&Config{}), p, "bytes=1000-1999")
	if resp.StatusCode != 206 || string(body) != string(data[1000:2000]) {
		t.Fatalf("got %d, %d bytes", resp.StatusCode, len(body))
	}
	if resp.Header.Get("Content-Range") != "bytes 1000-1999/1048576" {
		t.Fatalf("Content-Range: %q", resp.Header.Get("Content-Range"))
	}
	resp, body = get(t, handleDownload(&Config{}), p, "bytes=-10") // last 10 bytes
	if resp.StatusCode != 206 || string(body) != string(data[len(data)-10:]) {
		t.Fatalf("suffix range: %d, %d bytes", resp.StatusCode, len(body))
	}
	if resp, _ := get(t, handleDownload(&Config{}), p, "bytes=5000000-"); resp.StatusCode != 416 {
		t.Fatalf("range past the end: %d", resp.StatusCode)
	}
}

func TestRangeStillWorksWhenThrottled(t *testing.T) {
	p, data := rangeFixture(t)
	resp, body := get(t, handleDownload(&Config{ThrottleKbps: 100000}), p, "bytes=10-19")
	if resp.StatusCode != 206 || string(body) != string(data[10:20]) {
		t.Fatalf("got %d %v", resp.StatusCode, body)
	}
}

func TestPreviewOfAnImageSupportsRange(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.png")
	os.WriteFile(p, []byte("\x89PNG fake image bytes"), 0o644)
	resp, body := get(t, handlePreview(&Config{}), p, "bytes=0-3")
	if resp.StatusCode != 206 || string(body) != "\x89PNG" || resp.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("got %d %q %s", resp.StatusCode, body, resp.Header.Get("Content-Type"))
	}
}

func TestHealthAdvertisesRangeSupport(t *testing.T) {
	rec := httptest.NewRecorder()
	handleHealth(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if body := rec.Body.String(); !contains(body, `"features":["range"]`) || !contains(body, `"version":"1.2.0"`) {
		t.Fatalf("health: %s", body)
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }

