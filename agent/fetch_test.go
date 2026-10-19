package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const sourceToken = "source-token"

func signPass(token string, p passFields) string {
	payload, _ := json.Marshal(p)
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// sourceAgent is a source agent's handler set behind the real auth
// middleware, so the pass route's token exemption is exercised too.
func sourceAgent(t *testing.T) *httptest.Server {
	t.Helper()
	cfg := &Config{Token: sourceToken}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/pass-download", handlePassDownload(cfg))
	mux.HandleFunc("GET /api/download", handleDownload(cfg))
	srv := httptest.NewServer(authMiddleware(cfg.Token, mux))
	t.Cleanup(srv.Close)
	return srv
}

func sourceFile(t *testing.T, size int) (string, []byte, passFields) {
	t.Helper()
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i * 7 % 253)
	}
	p := filepath.Join(t.TempDir(), "big.bin")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(p)
	return p, data, passFields{
		V: 1, Job: time.Now().UnixNano(), File: 1, Path: p, Size: info.Size(),
		Mtime: info.ModTime().UTC().Format(time.RFC3339), Holders: []string{"192.0.2.1", "127.0.0.1"},
		Expires: time.Now().Add(15 * time.Minute).Unix(), Nonce: randomHex(16),
	}
}

func passGet(t *testing.T, srv *httptest.Server, pass, rng string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/pass-download", nil)
	req.Header.Set("X-LANHub-Pass", pass)
	if rng != "" {
		req.Header.Set("Range", rng)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestPassDownloadServesOnlyTheSignedFile(t *testing.T) {
	srv := sourceAgent(t)
	_, data, p := sourceFile(t, 1<<16)

	resp := passGet(t, srv, signPass(sourceToken, p), "")
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !bytes.Equal(body, data) {
		t.Fatalf("valid pass: %d, %d bytes", resp.StatusCode, len(body))
	}

	resp = passGet(t, srv, signPass(sourceToken, p), "bytes=100-199")
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 206 || !bytes.Equal(body, data[100:200]) {
		t.Fatalf("range: %d", resp.StatusCode)
	}

	// The token-protected routes still need the token.
	if r, _ := http.Get(srv.URL + "/api/download?" + url.Values{"path": {p.Path}}.Encode()); r.StatusCode != 401 {
		t.Fatalf("download without token: %d", r.StatusCode)
	}

	cases := map[string]struct {
		pass string
		want int
	}{
		"wrong key":     {signPass("someone-else", p), 401},
		"no pass":       {"", 401},
		"tampered path": {tamper(signPass(sourceToken, p), `"size"`, `"sizf"`), 401},
		"expired":       {signPass(sourceToken, with(p, func(q *passFields) { q.Expires = time.Now().Add(-time.Second).Unix() })), 401},
		"other holder":  {signPass(sourceToken, with(p, func(q *passFields) { q.Holders = []string{"192.0.2.9"} })), 403},
		"size changed":  {signPass(sourceToken, with(p, func(q *passFields) { q.Size++ })), 409},
		"mtime changed": {signPass(sourceToken, with(p, func(q *passFields) { q.Mtime = "2001-01-01T00:00:00Z" })), 409},
	}
	for name, c := range cases {
		resp := passGet(t, srv, c.pass, "")
		resp.Body.Close()
		if resp.StatusCode != c.want {
			t.Errorf("%s: got %d, want %d", name, resp.StatusCode, c.want)
		}
	}

	passes.retire(p.key())
	resp = passGet(t, srv, signPass(sourceToken, with(p, func(q *passFields) { q.Nonce = randomHex(16) })), "")
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatalf("retired job/file: %d", resp.StatusCode)
	}
}

func with(p passFields, f func(*passFields)) passFields { f(&p); return p }

// tamper edits the signed JSON without re-signing it.
func tamper(pass, old, new string) string {
	body, sig, _ := strings.Cut(pass, ".")
	raw, _ := base64.RawURLEncoding.DecodeString(body)
	raw = []byte(strings.Replace(string(raw), old, new, 1))
	return base64.RawURLEncoding.EncodeToString(raw) + "." + sig
}

func destCall(t *testing.T, h http.HandlerFunc, method, path, id string, body any) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	if id != "" {
		req.SetPathValue("id", id)
	}
	rec := httptest.NewRecorder()
	h(rec, req)
	var out map[string]any
	json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func waitFetch(t *testing.T, id string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		_, st := destCall(t, handleGetFetch, "GET", "/", id, nil)
		if st["status"] != "fetching" {
			return st
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("fetch never finished")
	return nil
}

func TestFetchCommitsOnlyAfterCompleteAndHidesThePartialFile(t *testing.T) {
	srv := sourceAgent(t)
	_, data, p := sourceFile(t, 3<<20)
	dir := t.TempDir()
	dest := filepath.Join(dir, "copy.bin")

	code, st := destCall(t, handleCreateFetch, "POST", "/", "", map[string]any{
		"source_url": srv.URL + "/api/pass-download", "pass": signPass(sourceToken, p),
		"destination": dest, "size": p.Size,
	})
	if code != 200 {
		t.Fatalf("create: %d %v", code, st)
	}
	id := st["id"].(string)
	st = waitFetch(t, id)
	sum := sha256.Sum256(data)
	if st["status"] != "received" || st["sha256"] != hex.EncodeToString(sum[:]) {
		t.Fatalf("after fetch: %v", st)
	}
	if _, err := os.Stat(dest); err == nil {
		t.Fatal("the real name exists before commit")
	}
	// The partial file is on disk but not listed.
	rec := httptest.NewRecorder()
	handleList(rec, httptest.NewRequest("GET", "/?"+url.Values{"path": {dir}}.Encode(), nil))
	if strings.Contains(rec.Body.String(), partialPrefix) {
		t.Fatalf("listing shows the partial file: %s", rec.Body.String())
	}

	if code, _ := destCall(t, handleCommitFetch, "POST", "/", id, nil); code != 200 {
		t.Fatalf("commit: %d", code)
	}
	got, _ := os.ReadFile(dest)
	if !bytes.Equal(got, data) {
		t.Fatal("committed file differs")
	}
	left, _ := filepath.Glob(filepath.Join(dir, partialPrefix+"*"))
	if len(left) != 0 {
		t.Fatalf("partial left behind: %v", left)
	}
}

func TestFetchRefusesToOverwrite(t *testing.T) {
	srv := sourceAgent(t)
	_, _, p := sourceFile(t, 1024)
	dir := t.TempDir()
	dest := filepath.Join(dir, "exists.bin")
	os.WriteFile(dest, []byte("keep me"), 0o644)
	code, _ := destCall(t, handleCreateFetch, "POST", "/", "", map[string]any{
		"source_url": srv.URL + "/api/pass-download", "pass": signPass(sourceToken, p), "destination": dest, "size": p.Size,
	})
	if code != 409 {
		t.Fatalf("create over an existing file: %d", code)
	}

	// The name appearing while the fetch ran is caught at commit.
	dest2 := filepath.Join(dir, "late.bin")
	_, st := destCall(t, handleCreateFetch, "POST", "/", "", map[string]any{
		"source_url": srv.URL + "/api/pass-download", "pass": signPass(sourceToken, p), "destination": dest2, "size": p.Size,
	})
	id := st["id"].(string)
	waitFetch(t, id)
	os.WriteFile(dest2, []byte("arrived first"), 0o644)
	if code, _ := destCall(t, handleCommitFetch, "POST", "/", id, nil); code != 409 {
		t.Fatalf("commit over a new file: %d", code)
	}
	if got, _ := os.ReadFile(dest2); string(got) != "arrived first" {
		t.Fatal("existing file was overwritten")
	}
	destCall(t, handleAbortFetch, "POST", "/", id, nil)
	if left, _ := filepath.Glob(filepath.Join(dir, partialPrefix+"*")); len(left) != 0 {
		t.Fatalf("abort left %v", left)
	}
}

func TestFetchFailsCleanlyOnABadPass(t *testing.T) {
	srv := sourceAgent(t)
	_, _, p := sourceFile(t, 1024)
	dir := t.TempDir()
	_, st := destCall(t, handleCreateFetch, "POST", "/", "", map[string]any{
		"source_url": srv.URL + "/api/pass-download", "pass": signPass("wrong", p),
		"destination": filepath.Join(dir, "x.bin"), "size": p.Size,
	})
	st = waitFetch(t, st["id"].(string))
	if st["status"] != "error" || !strings.Contains(*strPtr(st["error"]), "invalid pass") {
		t.Fatalf("bad pass: %v", st)
	}
	if left, _ := os.ReadDir(dir); len(left) != 0 {
		t.Fatalf("files left after a failed fetch: %d", len(left))
	}
}

func strPtr(v any) *string { s, _ := v.(string); return &s }

// A connection that drops mid-file is resumed with Range, and the hash
// still covers the whole file.
func TestFetchResumesAfterADroppedConnection(t *testing.T) {
	_, data, p := sourceFile(t, 2<<20)
	cfg := &Config{Token: sourceToken}
	real := handlePassDownload(cfg)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			// First attempt: send part of the file, then cut the connection.
			w.Header().Set("Content-Length", "2097152")
			w.WriteHeader(200)
			w.Write(data[:700000])
			w.(http.Flusher).Flush()
			conn, _, _ := w.(http.Hijacker).Hijack()
			conn.Close()
			return
		}
		if r.Header.Get("Range") != "bytes=700000-" {
			t.Errorf("resume asked for %q", r.Header.Get("Range"))
		}
		real(w, r)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "resumed.bin")
	_, st := destCall(t, handleCreateFetch, "POST", "/", "", map[string]any{
		"source_url": srv.URL, "pass": signPass(sourceToken, p), "destination": dest, "size": p.Size,
	})
	id := st["id"].(string)
	st = waitFetch(t, id)
	sum := sha256.Sum256(data)
	if st["status"] != "received" || st["sha256"] != hex.EncodeToString(sum[:]) || calls.Load() != 2 {
		t.Fatalf("resume: %v after %d calls", st, calls.Load())
	}
	destCall(t, handleCommitFetch, "POST", "/", id, nil)
	if got, _ := os.ReadFile(dest); !bytes.Equal(got, data) {
		t.Fatal("resumed file differs")
	}
}

func TestHashMatchesFileContents(t *testing.T) {
	path, data, _ := sourceFile(t, 5000)
	rec := httptest.NewRecorder()
	handleHash(rec, httptest.NewRequest("GET", "/?"+url.Values{"path": {path}}.Encode(), nil))
	sum := sha256.Sum256(data)
	if !strings.Contains(rec.Body.String(), hex.EncodeToString(sum[:])) {
		t.Fatalf("hash: %s", rec.Body.String())
	}
}

func TestHolderMatchingTreatsMappedIPv4AsTheSameAddress(t *testing.T) {
	if !holderMatches([]string{"192.168.0.5"}, "::ffff:192.168.0.5") || holderMatches([]string{"192.168.0.5"}, "192.168.0.50") || holderMatches([]string{"nas"}, "192.168.0.5") {
		t.Fatal("holder matching")
	}
}
