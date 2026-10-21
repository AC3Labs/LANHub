package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// AgentError is an agent's own error response (Status > 0) or a failure
// to reach it at all (Status 0).
type AgentError struct {
	Status  int
	Message string
}

func (e *AgentError) Error() string { return e.Message }

func agentStatus(err error) int {
	var ae *AgentError
	if errors.As(err, &ae) {
		return ae.Status
	}
	return 0
}

type Entry struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Type     string `json:"type"`
	Size     *int64 `json:"size"`
	Modified string `json:"modified"`
	Hidden   bool   `json:"hidden"`
}

type Drive struct {
	Path  string `json:"path"`
	Label string `json:"label"`
	Free  int64  `json:"free"`
	Total int64  `json:"total"`
}

type AgentTransfer struct {
	ID          string  `json:"id"`
	Kind        string  `json:"kind"`
	Source      string  `json:"source"`
	Destination string  `json:"destination"`
	Status      string  `json:"status"`
	BytesDone   int64   `json:"bytes_done"`
	BytesTotal  int64   `json:"bytes_total"`
	Error       *string `json:"error"`
	CreatedAt   string  `json:"created_at"`
}

type ActivityEntry struct {
	Time   string `json:"time"`
	Method string `json:"method"`
	Path   string `json:"path"`
	Status int    `json:"status"`
	Client string `json:"client"`
}

// Connect quickly or not at all: an asleep or firewalled machine must cost
// 3 s, not 15, since pages talk to several agents.
var agentDialer = &net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}

func newAgentTransport(insecure bool) *http.Transport {
	return &http.Transport{
		DialContext:           agentDialer.DialContext,
		MaxIdleConnsPerHost:   8,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: 0,
		// Agents serve self-signed certificates on a private LAN; only
		// skipped for private addresses, as the PHP hub does.
		TLSClientConfig: &tls.Config{InsecureSkipVerify: insecure},
	}
}

var (
	agentHTTP         = &http.Client{Transport: newAgentTransport(false)}
	agentHTTPInsecure = &http.Client{Transport: newAgentTransport(true)}
)

type AgentClient struct {
	m     *Machine
	token string
}

func (s *Store) Agent(m *Machine) (*AgentClient, error) {
	tok, err := s.AgentToken(m)
	if err != nil {
		return nil, &AgentError{Message: "The agent token for " + m.Name + " can't be decrypted (wrong APP_KEY?)."}
	}
	return &AgentClient{m: m, token: tok}, nil
}

func (a *AgentClient) client() *http.Client {
	if a.m.UseTLS && isPrivateHost(a.m.Host) {
		return agentHTTPInsecure
	}
	return agentHTTP
}

func isPrivateHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast())
}

func (a *AgentClient) newRequest(ctx context.Context, method, path string, query url.Values, body io.Reader) (*http.Request, error) {
	u := a.m.BaseURL() + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+a.token)
	req.Header.Set("Accept", "application/json")
	return req, nil
}

// do sends a request and turns transport failures and non-2xx answers into
// AgentError, using the agent's {"message": ...} when it sent one.
func (a *AgentClient) do(req *http.Request) (*http.Response, error) {
	resp, err := a.client().Do(req)
	if err != nil {
		msg := fmt.Sprintf("Couldn't reach %s: %s", a.m.Name, shortNetErr(err))
		return nil, &AgentError{Status: 0, Message: msg}
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		var body struct {
			Message string `json:"message"`
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		json.Unmarshal(raw, &body)
		msg := body.Message
		if msg == "" {
			msg = fmt.Sprintf("Agent request to %s failed with status %d.", a.m.Name, resp.StatusCode)
		}
		return nil, &AgentError{Status: resp.StatusCode, Message: msg}
	}
	return resp, nil
}

func shortNetErr(err error) string {
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "connection timed out"
	}
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i >= 0 {
		return s[i+2:]
	}
	return s
}

// getJSON is for quick API calls: 15 s for the whole request.
func (a *AgentClient) getJSON(ctx context.Context, path string, query url.Values, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := a.newRequest(ctx, http.MethodGet, path, query, nil)
	if err != nil {
		return err
	}
	resp, err := a.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(out)
}

func (a *AgentClient) postJSON(ctx context.Context, path string, payload any, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	raw, _ := json.Marshal(payload)
	req, err := a.newRequest(ctx, http.MethodPost, path, nil, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil {
		io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// AgentHealth is /api/health. Features lists what an agent can do beyond
// the 1.1.0 API (1.2.0 adds "range"); older agents send none.
type AgentHealth struct {
	Hostname string   `json:"hostname"`
	OS       string   `json:"os"`
	Arch     string   `json:"arch"` // agent 1.4.0+
	Version  string   `json:"version"`
	Features []string `json:"features"`
	// Addresses are the machine's own IPs (agent 1.3.0+), used to name the
	// destination in a direct-transfer pass.
	Addresses []string `json:"addresses"`
}

func (h AgentHealth) Has(feature string) bool {
	for _, f := range h.Features {
		if f == feature {
			return true
		}
	}
	return false
}

func (a *AgentClient) Health(ctx context.Context) (AgentHealth, error) {
	var out AgentHealth
	return out, a.getJSON(ctx, "/api/health", nil, &out)
}

func (a *AgentClient) Drives(ctx context.Context) ([]Drive, error) {
	var out struct {
		Drives []Drive `json:"drives"`
	}
	return out.Drives, a.getJSON(ctx, "/api/drives", nil, &out)
}

func (a *AgentClient) Netstats(ctx context.Context) (sent, recv int64, err error) {
	var out struct {
		Sent int64 `json:"bytes_sent"`
		Recv int64 `json:"bytes_recv"`
	}
	err = a.getJSON(ctx, "/api/netstats", nil, &out)
	return out.Sent, out.Recv, err
}

func (a *AgentClient) List(ctx context.Context, path string) ([]Entry, error) {
	var out struct {
		Entries []Entry `json:"entries"`
	}
	return out.Entries, a.getJSON(ctx, "/api/list", url.Values{"path": {path}}, &out)
}

type SearchResult struct {
	Entries   []Entry `json:"entries"`
	Truncated bool    `json:"truncated"`
}

// Search waits up to 30 s: the agent caps its own walk at 10 s, and a slow
// disk can take a while to answer after that.
func (a *AgentClient) Search(ctx context.Context, path, query string) (SearchResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var out SearchResult
	req, err := a.newRequest(ctx, http.MethodGet, "/api/search", url.Values{"path": {path}, "query": {query}}, nil)
	if err != nil {
		return out, err
	}
	resp, err := a.do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	return out, json.NewDecoder(resp.Body).Decode(&out)
}

func (a *AgentClient) Activity(ctx context.Context, limit int) ([]ActivityEntry, error) {
	var out struct {
		Entries []ActivityEntry `json:"entries"`
	}
	return out.Entries, a.getJSON(ctx, "/api/activity", url.Values{"limit": {fmt.Sprint(limit)}}, &out)
}

func (a *AgentClient) Transfers(ctx context.Context) ([]AgentTransfer, error) {
	var out struct {
		Transfers []AgentTransfer `json:"transfers"`
	}
	return out.Transfers, a.getJSON(ctx, "/api/transfers", nil, &out)
}

func (a *AgentClient) Mkdir(ctx context.Context, path string) error {
	return a.postJSON(ctx, "/api/mkdir", map[string]any{"path": path}, nil)
}

func (a *AgentClient) Rename(ctx context.Context, path, newName string) error {
	return a.postJSON(ctx, "/api/rename", map[string]any{"path": path, "new_name": newName}, nil)
}

func (a *AgentClient) Move(ctx context.Context, src, dst string) error {
	return a.postJSON(ctx, "/api/move", map[string]any{"source": src, "destination": dst}, nil)
}

// Copy on the same machine can take a while (it's the agent doing a real
// copy), so it gets the transfer treatment: no overall limit.
func (a *AgentClient) Copy(ctx context.Context, src, dst string) error {
	raw, _ := json.Marshal(map[string]any{"source": src, "destination": dst})
	req, err := a.newRequest(ctx, http.MethodPost, "/api/copy", nil, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func (a *AgentClient) Delete(ctx context.Context, path string) error {
	return a.postJSON(ctx, "/api/delete", map[string]any{"path": path, "recursive": true}, nil)
}

// ---- streaming ----

// stallTimeout gives up on a transfer that sends nothing for this long;
// there is no overall limit, so a big file on a slow link still finishes.
const stallTimeout = 60 * time.Second

// idleReader cancels its context when no bytes have arrived for the stall
// timeout. It wraps response bodies being streamed somewhere else.
type idleReader struct {
	r      io.Reader
	timer  *time.Timer
	cancel context.CancelFunc
	once   sync.Once
}

func newIdleReader(r io.Reader, cancel context.CancelFunc) *idleReader {
	ir := &idleReader{r: r, cancel: cancel}
	ir.timer = time.AfterFunc(stallTimeout, cancel)
	return ir
}

func (ir *idleReader) Read(p []byte) (int, error) {
	n, err := ir.r.Read(p)
	if n > 0 {
		ir.timer.Reset(stallTimeout)
	}
	return n, err
}

func (ir *idleReader) stop() { ir.once.Do(func() { ir.timer.Stop() }) }

// Stream is an open download from an agent. Close it when done.
type Stream struct {
	Resp   *http.Response
	Body   *idleReader
	cancel context.CancelFunc
}

func (s *Stream) Read(p []byte) (int, error) { return s.Body.Read(p) }

func (s *Stream) Close() error {
	s.Body.stop()
	s.cancel()
	return s.Resp.Body.Close()
}

// Open streams /api/download or /api/preview. The connect timeout still
// applies; after that only the stall timeout does.
func (a *AgentClient) Open(ctx context.Context, endpoint, path string) (*Stream, error) {
	return a.OpenRange(ctx, endpoint, path, "")
}

// OpenRange is Open with an optional HTTP Range header passed to the agent.
// Agent 1.1.0 ignores it and sends the whole file (200); an agent that
// honours it answers 206, which the caller passes on.
func (a *AgentClient) OpenRange(ctx context.Context, endpoint, path, rangeHeader string) (*Stream, error) {
	ctx, cancel := context.WithCancel(ctx)
	req, err := a.newRequest(ctx, http.MethodGet, endpoint, url.Values{"path": {path}}, nil)
	if err != nil {
		cancel()
		return nil, err
	}
	if rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}
	resp, err := a.do(req)
	if err != nil {
		cancel()
		return nil, err
	}
	return &Stream{Resp: resp, Body: newIdleReader(resp.Body, cancel), cancel: cancel}, nil
}

// Upload streams r to the agent's /api/upload as multipart without
// buffering the file on the hub. Returns the agent's entry for the new file.
func (a *AgentClient) Upload(ctx context.Context, dir, filename string, r io.Reader) (*Entry, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	src := newIdleReader(r, cancel)
	ahead := newPrefetchReader(src, 1<<20, 8) // read up to 8 MB ahead of the upload
	go func() {
		defer src.stop()
		defer ahead.Close()
		part, err := mw.CreateFormFile("file", filename)
		if err == nil {
			_, err = io.CopyBuffer(part, ahead, make([]byte, 1<<20))
		}
		if err == nil {
			err = mw.Close()
		}
		pw.CloseWithError(err)
	}()
	req, err := a.newRequest(ctx, http.MethodPost, "/api/upload", url.Values{"path": {dir}}, pr)
	if err != nil {
		pr.CloseWithError(err)
		return nil, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := a.do(req)
	if err != nil {
		pr.CloseWithError(err)
		return nil, err
	}
	defer resp.Body.Close()
	var e Entry
	if err := json.NewDecoder(resp.Body).Decode(&e); err != nil {
		return nil, &AgentError{Message: "The agent's upload reply couldn't be read."}
	}
	return &e, nil
}
