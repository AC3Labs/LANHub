package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Direct agent-to-agent transfers (docs/DIRECT_TRANSFER_DESIGN.md).
//
// The hub asks the destination agent to fetch a file straight from the
// source agent, so the bytes never pass through the hub. The source only
// serves a file to a caller holding a pass the hub signed with the
// source's own token, so no agent ever learns another agent's token.

const partialPrefix = ".lanhub-partial-"

// isPartialName hides in-progress fetches from listings and searches, so
// a half-copied file is never shown as if it were complete.
func isPartialName(name string) bool { return strings.HasPrefix(name, partialPrefix) }

// ---- the pass (source side) ----

type passFields struct {
	V       int    `json:"v"`
	Job     int64  `json:"job"`
	File    int    `json:"file"`
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	Mtime   string `json:"mtime"`
	Holders []string `json:"holders"`
	Expires int64  `json:"expires"`
	Nonce   string `json:"nonce"`
}

func (p passFields) key() string { return fmt.Sprintf("%d/%d", p.Job, p.File) }

// verifyPass checks the signature (HMAC-SHA256 keyed with this agent's
// token) and the expiry. The caller checks everything that depends on
// the file or the request.
func verifyPass(token, raw string) (passFields, error) {
	var p passFields
	body, sig, ok := strings.Cut(raw, ".")
	if !ok {
		return p, errors.New("malformed pass")
	}
	payload, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return p, errors.New("malformed pass")
	}
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return p, errors.New("malformed pass")
	}
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write(payload)
	if !hmac.Equal(got, mac.Sum(nil)) {
		return p, errors.New("invalid pass")
	}
	if err := json.Unmarshal(payload, &p); err != nil || p.V != 1 {
		return p, errors.New("unsupported pass")
	}
	if time.Now().Unix() > p.Expires {
		return p, errors.New("pass expired")
	}
	return p, nil
}

// passBook remembers which passes are streaming right now (one stream per
// nonce) and which job/file pairs the hub has retired.
type passBook struct {
	mu      sync.Mutex
	active  map[string]bool
	retired map[string]time.Time // key -> forget after
}

var passes = &passBook{active: map[string]bool{}, retired: map[string]time.Time{}}

// A retired key is kept well past the longest pass life (15 minutes), so
// any pass minted before it was retired has expired by the time it's
// forgotten.
const retiredKeep = time.Hour

func (b *passBook) begin(p passFields) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	for k, until := range b.retired {
		if now.After(until) {
			delete(b.retired, k)
		}
	}
	if _, gone := b.retired[p.key()]; gone {
		return newError(403, "This pass has been retired.")
	}
	if b.active[p.Nonce] {
		return newError(409, "This pass is already in use.")
	}
	b.active[p.Nonce] = true
	return nil
}

func (b *passBook) end(nonce string) {
	b.mu.Lock()
	delete(b.active, nonce)
	b.mu.Unlock()
}

func (b *passBook) retire(key string) {
	b.mu.Lock()
	b.retired[key] = time.Now().Add(retiredKeep)
	b.mu.Unlock()
}

// --- GET /api/pass-download (no bearer token: the pass is the credential) ---

func handlePassDownload(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, err := verifyPass(cfg.Token, r.Header.Get("X-LANHub-Pass"))
		if err != nil {
			writeError(w, 401, err.Error()+".")
			return
		}
		// Defence in depth only: addresses can be shared or spoofed on a LAN.
		if len(p.Holders) > 0 && !holderMatches(p.Holders, clientIP(r)) {
			writeError(w, 403, "This pass was issued to another machine.")
			return
		}
		filePath, err := resolvePath(p.Path)
		if err != nil {
			handleErr(w, err)
			return
		}
		info, err := os.Stat(filePath)
		if err != nil || info.IsDir() {
			writeError(w, 404, filePath+" is not a file.")
			return
		}
		if info.Size() != p.Size || (p.Mtime != "" && info.ModTime().UTC().Format(time.RFC3339) != p.Mtime) {
			writeError(w, 409, filePath+" changed after the transfer started.")
			return
		}
		if err := passes.begin(p); err != nil {
			handleErr(w, err)
			return
		}
		defer passes.end(p.Nonce)

		w.Header().Set("Content-Type", "application/octet-stream")
		serveFile(w, r, filePath, throttleDelay(cfg))
	}
}

// holderMatches compares addresses as IPs, so "::ffff:192.168.0.5" and
// "192.168.0.5" are the same caller.
func holderMatches(holders []string, client string) bool {
	ip := net.ParseIP(client)
	for _, h := range holders {
		if hip := net.ParseIP(h); hip != nil && ip != nil && hip.Equal(ip) {
			return true
		}
	}
	return false
}

// localAddresses lists this machine's own IP addresses (not loopback) for
// /api/health, so the hub can name every address a fetch might come from
// when a machine has more than one network connection.
func localAddresses() []string {
	out := []string{}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return out
	}
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok || ipn.IP.IsLoopback() || ipn.IP.IsLinkLocalUnicast() {
			continue
		}
		out = append(out, ipn.IP.String())
	}
	return out
}

// --- POST /api/passes/retire ---

func handleRetirePass(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Job  int64 `json:"job"`
		File int   `json:"file"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, 400, "invalid request body.")
		return
	}
	passes.retire(passFields{Job: body.Job, File: body.File}.key())
	writeJSON(w, 200, map[string]bool{"retired": true})
}

// --- GET /api/hash ---

// The hub compares this with the destination's hash of what it received,
// so neither agent has to vouch for the other.
func handleHash(w http.ResponseWriter, r *http.Request) {
	filePath, err := resolvePath(queryParam(r, "path"))
	if err != nil {
		handleErr(w, err)
		return
	}
	f, err := os.Open(filePath)
	if err != nil {
		writeError(w, 404, filePath+" is not a file.")
		return
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil || info.IsDir() {
		writeError(w, 404, filePath+" is not a file.")
		return
	}
	h := sha256.New()
	n, err := io.CopyBuffer(h, readerWithContext{r.Context(), f}, make([]byte, 1<<20))
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"path": filePath, "size": n, "sha256": hex.EncodeToString(h.Sum(nil))})
}

// readerWithContext stops a long read (hashing a big file) once the
// request that wanted it has gone away.
type readerWithContext struct {
	ctx context.Context
	r   io.Reader
}

func (c readerWithContext) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// ---- fetches (destination side) ----

type fetchJob struct {
	mu       sync.Mutex
	ID       string `json:"id"`
	Source   string `json:"-"`
	pass     string
	insecure bool
	Dest     string `json:"destination"`
	partial  string
	Size     int64   `json:"size"`
	Status   string  `json:"status"` // fetching, received, error, committed, aborted
	Bytes    int64   `json:"bytes"`
	SHA256   string  `json:"sha256,omitempty"`
	Error    *string `json:"error"`
	cancel   context.CancelFunc
	done     chan struct{}
	finished time.Time
}

func (f *fetchJob) snapshot() map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return map[string]any{
		"id": f.ID, "destination": f.Dest, "size": f.Size, "status": f.Status,
		"bytes": f.Bytes, "sha256": f.SHA256, "error": f.Error,
	}
}

type fetchBook struct {
	mu   sync.Mutex
	jobs map[string]*fetchJob
}

var fetches = &fetchBook{jobs: map[string]*fetchJob{}}

func (b *fetchBook) get(id string) *fetchJob {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.jobs[id]
}

// add stores a job and forgets ones that finished over an hour ago.
func (b *fetchBook) add(f *fetchJob) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for id, old := range b.jobs {
		old.mu.Lock()
		stale := !old.finished.IsZero() && time.Since(old.finished) > time.Hour
		old.mu.Unlock()
		if stale {
			delete(b.jobs, id)
		}
	}
	b.jobs[f.ID] = f
}

func (b *fetchBook) usesPartial(path string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, f := range b.jobs {
		if f.partial == path {
			return true
		}
	}
	return false
}

// sweepStalePartials deletes leftovers from fetches that never finished
// (agent restarted mid-copy), but only in the folder being written to and
// only when over a day old.
func sweepStalePartials(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !isPartialName(e.Name()) {
			continue
		}
		p := filepath.Join(dir, e.Name())
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > 24*time.Hour && !fetches.usesPartial(p) {
			os.Remove(p)
		}
	}
}

var fetchDialer = &net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}

func fetchClient(insecure bool) *http.Client {
	return &http.Client{Transport: &http.Transport{
		DialContext: fetchDialer.DialContext,
		// Agents serve self-signed certificates; the hub only asks for this
		// when the source is on a private address.
		TLSClientConfig: &tls.Config{InsecureSkipVerify: insecure},
	}}
}

// --- POST /api/fetches ---

func handleCreateFetch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SourceURL   string `json:"source_url"`
		Pass        string `json:"pass"`
		Destination string `json:"destination"`
		Size        int64  `json:"size"`
		Insecure    bool   `json:"insecure"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, 400, "invalid request body.")
		return
	}
	if !strings.HasPrefix(body.SourceURL, "http://") && !strings.HasPrefix(body.SourceURL, "https://") {
		writeError(w, 400, "source_url must be an http(s) URL.")
		return
	}
	name := filepath.Base(body.Destination)
	if body.Destination == "" || name == "." || name == string(filepath.Separator) || isPartialName(name) {
		writeError(w, 400, "destination must be a file path.")
		return
	}
	dir, err := resolvePath(filepath.Dir(body.Destination))
	if err != nil {
		handleErr(w, err)
		return
	}
	dest := filepath.Join(dir, name)
	if _, err := os.Lstat(dest); err == nil {
		writeError(w, 409, dest+" already exists.")
		return
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	sweepStalePartials(dir)

	id := randomHex(16)
	partial := filepath.Join(dir, partialPrefix+id)
	out, err := os.OpenFile(partial, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	f := &fetchJob{
		ID: id, Source: body.SourceURL, pass: body.Pass, insecure: body.Insecure,
		Dest: dest, partial: partial, Size: body.Size, Status: "fetching",
		cancel: cancel, done: make(chan struct{}),
	}
	fetches.add(f)
	go f.run(ctx, out)
	writeJSON(w, 200, f.snapshot())
}

// fetchStall gives up on a source that sends nothing for this long; there's
// no overall limit, so a big file still finishes.
const fetchStall = 60 * time.Second

const fetchAttempts = 5

// permanentErr is an answer from the source that retrying won't change
// (bad pass, file changed, gone).
type permanentErr struct{ error }

func (f *fetchJob) run(ctx context.Context, out *os.File) {
	defer close(f.done)
	h := sha256.New()
	var written int64
	var err error
	for attempt := 1; attempt <= fetchAttempts; attempt++ {
		written, err = f.pull(ctx, out, h, written)
		if err == nil || ctx.Err() != nil {
			break
		}
		var perm permanentErr
		if errors.As(err, &perm) {
			break
		}
		select {
		case <-ctx.Done():
		case <-time.After(2 * time.Second):
		}
	}
	closeErr := out.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil && f.Size >= 0 && written != f.Size {
		err = fmt.Errorf("only %d of %d bytes arrived", written, f.Size)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.finished = time.Now()
	if f.Status == "aborted" {
		return // abort deletes the partial file once this returns
	}
	if err != nil {
		msg := err.Error()
		if ctx.Err() != nil {
			msg = "cancelled"
		}
		f.Error = &msg
		f.Status = "error"
		os.Remove(f.partial)
		logActivity("FETCH", f.Dest, 500, f.Source)
		return
	}
	f.SHA256 = hex.EncodeToString(h.Sum(nil))
	f.Status = "received"
}

// pull makes one request to the source, resuming at offset with a Range
// header. A source that answers 200 instead of 206 sends the whole file,
// so the partial file and hash start over.
func (f *fetchJob) pull(ctx context.Context, out *os.File, h hash.Hash, offset int64) (int64, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.Source, nil)
	if err != nil {
		return offset, permanentErr{err}
	}
	f.mu.Lock()
	req.Header.Set("X-LANHub-Pass", f.pass)
	f.mu.Unlock()
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	resp, err := fetchClient(f.insecure).Do(req)
	if err != nil {
		return offset, err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusPartialContent && offset > 0:
	case resp.StatusCode == http.StatusOK:
		if offset > 0 {
			if err := out.Truncate(0); err != nil {
				return offset, permanentErr{err}
			}
			if _, err := out.Seek(0, io.SeekStart); err != nil {
				return offset, permanentErr{err}
			}
			h.Reset()
			offset = 0
			f.setBytes(0)
		}
	default:
		var body struct {
			Message string `json:"message"`
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		json.Unmarshal(raw, &body)
		msg := body.Message
		if msg == "" {
			msg = fmt.Sprintf("the source answered %d", resp.StatusCode)
		}
		e := fmt.Errorf("source: %s", msg)
		if resp.StatusCode >= 500 {
			return offset, e
		}
		return offset, permanentErr{e}
	}

	timer := time.AfterFunc(fetchStall, cancel)
	defer timer.Stop()
	buf := make([]byte, 1<<20)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			timer.Reset(fetchStall)
			if _, err := out.Write(buf[:n]); err != nil {
				return offset, permanentErr{err} // disk full and the like
			}
			h.Write(buf[:n])
			offset += int64(n)
			f.setBytes(offset)
		}
		if readErr == io.EOF {
			return offset, nil
		}
		if readErr != nil {
			return offset, readErr
		}
	}
}

func (f *fetchJob) setBytes(n int64) {
	f.mu.Lock()
	f.Bytes = n
	f.mu.Unlock()
}

// --- GET /api/fetches/{id} ---

func handleGetFetch(w http.ResponseWriter, r *http.Request) {
	f := fetches.get(r.PathValue("id"))
	if f == nil {
		writeError(w, 404, "Unknown fetch id.")
		return
	}
	writeJSON(w, 200, f.snapshot())
}

// --- POST /api/fetches/{id}/pass ---

// The hub hands over a fresh pass while a long fetch is still going, so a
// reconnect after the first pass expired still works.
func handleRenewFetchPass(w http.ResponseWriter, r *http.Request) {
	f := fetches.get(r.PathValue("id"))
	if f == nil {
		writeError(w, 404, "Unknown fetch id.")
		return
	}
	var body struct {
		Pass string `json:"pass"`
	}
	if err := decodeBody(r, &body); err != nil || body.Pass == "" {
		writeError(w, 400, "pass is required.")
		return
	}
	f.mu.Lock()
	f.pass = body.Pass
	f.mu.Unlock()
	writeJSON(w, 200, f.snapshot())
}

// --- POST /api/fetches/{id}/commit ---

func handleCommitFetch(w http.ResponseWriter, r *http.Request) {
	f := fetches.get(r.PathValue("id"))
	if f == nil {
		writeError(w, 404, "Unknown fetch id.")
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Status == "committed" {
		writeJSON(w, 200, toEntry(f.Dest))
		return
	}
	if f.Status != "received" {
		writeError(w, 409, "The fetch isn't ready to commit (status "+f.Status+").")
		return
	}
	if _, err := os.Lstat(f.Dest); err == nil {
		writeError(w, 409, f.Dest+" already exists.")
		return
	}
	if err := os.Rename(f.partial, f.Dest); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	f.Status = "committed"
	writeJSON(w, 200, toEntry(f.Dest))
}

// --- POST /api/fetches/{id}/abort ---

func handleAbortFetch(w http.ResponseWriter, r *http.Request) {
	f := fetches.get(r.PathValue("id"))
	if f == nil {
		writeError(w, 404, "Unknown fetch id.")
		return
	}
	f.mu.Lock()
	if f.Status == "committed" {
		f.mu.Unlock()
		writeError(w, 409, "The fetch was already committed.")
		return
	}
	f.Status = "aborted"
	f.mu.Unlock()
	f.cancel()
	<-f.done
	os.Remove(f.partial)
	writeJSON(w, 200, f.snapshot())
}
