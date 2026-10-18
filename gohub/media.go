package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// LANHub Media integration. The media service (cmd/lanhub-media, its own
// container) turns videos the browser can't play into ones it can. The hub
// stays in charge of access: the browser only ever talks to the hub, and the
// media service reads a file through a signed, expiring, single-file link
// back to the hub, never with an agent token.
//
// Configured with MEDIA_URL (where the media service is), MEDIA_SECRET
// (shared with it) and MEDIA_SOURCE_BASE (the hub's address as the media
// service sees it). Without them, previews fall back to direct play only.

type mediaConfig struct {
	url, secret, sourceBase string
}

func (app *App) mediaEnabled() bool {
	return app.media.url != "" && app.media.secret != "" && app.media.sourceBase != ""
}

// sourceKey signs media-source links. Derived from APP_KEY so nothing else
// needs configuring; a link signed for one file can't be reused for another.
func (app *App) sourceKey() []byte {
	m := hmac.New(sha256.New, app.store.enc.key)
	m.Write([]byte("lanhub-media-source-v1"))
	return m.Sum(nil)
}

func (app *App) signSource(machineID int64, path string, exp int64) string {
	m := hmac.New(sha256.New, app.sourceKey())
	fmt.Fprintf(m, "%d\x00%s\x00%d", machineID, path, exp)
	return hex.EncodeToString(m.Sum(nil))
}

// sourceURL is what the media service reads the file from. Valid 12 hours,
// long enough for a film, short enough to be useless later.
func (app *App) sourceURL(machineID int64, path string) string {
	exp := time.Now().Add(12 * time.Hour).Unix()
	q := url.Values{"m": {strconv.FormatInt(machineID, 10)}, "p": {path}, "exp": {strconv.FormatInt(exp, 10)}, "sig": {app.signSource(machineID, path, exp)}}
	return strings.TrimRight(app.media.sourceBase, "/") + "/media-source?" + q.Encode()
}

// mediaSource serves a signed link: no session, just a valid signature for
// exactly this machine and file. Range requests go through to the agent so
// the media service can seek.
func (app *App) mediaSource(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	id, _ := strconv.ParseInt(q.Get("m"), 10, 64)
	exp, _ := strconv.ParseInt(q.Get("exp"), 10, 64)
	path := q.Get("p")
	if id == 0 || path == "" || time.Now().Unix() > exp ||
		!hmac.Equal([]byte(app.signSource(id, path, exp)), []byte(q.Get("sig"))) {
		http.Error(w, "invalid or expired link", http.StatusForbidden)
		return
	}
	m, err := app.store.Machine(id)
	if err != nil {
		http.Error(w, "no such machine", http.StatusNotFound)
		return
	}
	ag, err := app.store.Agent(m)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	st, err := ag.OpenRange(r.Context(), "/api/download", path, r.Header.Get("Range"))
	if err != nil {
		status := agentStatus(err)
		if status == 0 || status >= 500 {
			status = http.StatusBadGateway
		}
		http.Error(w, err.Error(), status)
		return
	}
	defer st.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	// An agent older than 1.2.0 ignores Range and sends the whole file. ffmpeg
	// asked for a specific part (an MP4 keeps its index at the end), so give
	// it exactly that part by skipping ahead in the stream. Slower than an
	// agent that seeks, but never wrong.
	if want := r.Header.Get("Range"); want != "" && st.Resp.StatusCode == http.StatusOK {
		total, err := strconv.ParseInt(st.Resp.Header.Get("Content-Length"), 10, 64)
		from, to, ok := parseRange(want, total)
		if err != nil || !ok {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", total))
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		if _, err := io.CopyN(io.Discard, st, from); err != nil {
			return
		}
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", from, to, total))
		w.Header().Set("Content-Length", strconv.FormatInt(to-from+1, 10))
		w.WriteHeader(http.StatusPartialContent)
		io.CopyBuffer(w, io.LimitReader(st, to-from+1), make([]byte, 1<<20))
		return
	}
	for _, k := range []string{"Content-Length", "Content-Range", "Last-Modified"} {
		if v := st.Resp.Header.Get(k); v != "" {
			w.Header().Set(k, v)
		}
	}
	w.Header().Set("Accept-Ranges", "bytes") // answered here even when the agent can't
	w.WriteHeader(st.Resp.StatusCode)
	io.CopyBuffer(w, st, make([]byte, 1<<20))
}

// parseRange reads a single "bytes=a-b", "bytes=a-" or "bytes=-n" range
// against a file of total bytes. Multiple ranges aren't supported.
func parseRange(h string, total int64) (from, to int64, ok bool) {
	spec, found := strings.CutPrefix(h, "bytes=")
	if !found || strings.Contains(spec, ",") || total <= 0 {
		return 0, 0, false
	}
	a, b, _ := strings.Cut(spec, "-")
	switch {
	case a == "": // last n bytes
		n, err := strconv.ParseInt(b, 10, 64)
		if err != nil || n <= 0 {
			return 0, 0, false
		}
		return max(total-n, 0), total - 1, true
	default:
		f, err := strconv.ParseInt(a, 10, 64)
		if err != nil || f < 0 || f >= total {
			return 0, 0, false
		}
		t := total - 1
		if b != "" {
			if t2, err := strconv.ParseInt(b, 10, 64); err == nil && t2 < t {
				t = t2
			}
			if t < f {
				return 0, 0, false
			}
		}
		return f, t, true
	}
}

type mediaPlan struct {
	Mode      string  `json:"mode"`
	Reason    string  `json:"reason"`
	Duration  float64 `json:"duration"`
	Container string  `json:"container"`
	Video     string  `json:"video,omitempty"`
	Audio     string  `json:"audio,omitempty"`
	Width     int     `json:"width,omitempty"`
	Height    int     `json:"height,omitempty"`
	HW        bool    `json:"hw"`
	Seekable  bool    `json:"seekable"` // the agent supports Range
}

type planCache struct {
	mu    sync.Mutex
	items map[string]cachedPlan
}

type cachedPlan struct {
	plan mediaPlan
	at   time.Time
}

var mediaHTTP = &http.Client{Transport: &http.Transport{DialContext: agentDialer.DialContext, ResponseHeaderTimeout: 60 * time.Second}}

// apiMediaInfo asks the media service how to play a file (cached briefly).
func (app *App) apiMediaInfo(w http.ResponseWriter, r *http.Request) {
	m, ag, ok := app.machineFor(w, r, "read_only")
	if !ok {
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		writeJSON(w, 400, map[string]string{"message": "No file was given."})
		return
	}
	if !app.mediaEnabled() {
		writeJSON(w, 200, mediaPlan{Mode: "direct", Reason: "LANHub Media isn't set up, so only formats the browser plays itself will work."})
		return
	}
	key := fmt.Sprintf("%d\x00%s", m.ID, path)
	app.plans.mu.Lock()
	if c, ok := app.plans.items[key]; ok && time.Since(c.at) < 10*time.Minute {
		app.plans.mu.Unlock()
		writeJSON(w, 200, c.plan)
		return
	}
	app.plans.mu.Unlock()

	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	health, _ := ag.Health(ctx)
	body, _ := json.Marshal(map[string]string{"source": app.sourceURL(m.ID, path), "filename": baseName(path)})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(app.media.url, "/")+"/probe", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+app.media.secret)
	req.Header.Set("Content-Type", "application/json")
	resp, err := mediaHTTP.Do(req)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"message": "LANHub Media can't be reached: " + shortNetErr(err)})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		var e struct{ Message string }
		json.NewDecoder(resp.Body).Decode(&e)
		if e.Message == "" {
			e.Message = fmt.Sprintf("LANHub Media answered %d.", resp.StatusCode)
		}
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": e.Message})
		return
	}
	var plan mediaPlan
	if json.NewDecoder(resp.Body).Decode(&plan) != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"message": "LANHub Media sent an unreadable answer."})
		return
	}
	plan.Seekable = health.Has("range")
	app.plans.mu.Lock()
	app.plans.items[key] = cachedPlan{plan, time.Now()}
	app.plans.mu.Unlock()
	writeJSON(w, 200, plan)
}

// play streams a converted or repackaged video from the media service to
// the browser, starting at ?start= seconds. Closing the preview cancels the
// request, which stops the conversion.
func (app *App) play(w http.ResponseWriter, r *http.Request) {
	m, _, ok := app.machineFor(w, r, "read_only")
	if !ok {
		return
	}
	q := r.URL.Query()
	path, mode := q.Get("path"), q.Get("mode")
	if !app.mediaEnabled() || path == "" || (mode != "remux" && mode != "audio" && mode != "transcode") {
		app.errorPage(w, r, http.StatusBadRequest, "This video can't be converted here.")
		return
	}
	start, _ := strconv.ParseFloat(q.Get("start"), 64)
	u := strings.TrimRight(app.media.url, "/") + "/stream?" + url.Values{
		"mode": {mode}, "source": {app.sourceURL(m.ID, path)}, "start": {strconv.FormatFloat(max(start, 0), 'f', 2, 64)}, "audio": {q.Get("audio")},
	}.Encode()
	req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, u, nil)
	req.Header.Set("Authorization", "Bearer "+app.media.secret)
	resp, err := mediaHTTP.Do(req)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			app.errorPage(w, r, http.StatusBadGateway, "LANHub Media can't be reached: "+shortNetErr(err))
		}
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		var e struct{ Message string }
		json.NewDecoder(resp.Body).Decode(&e)
		if e.Message == "" {
			e.Message = fmt.Sprintf("LANHub Media answered %d.", resp.StatusCode)
		}
		app.errorPage(w, r, resp.StatusCode, e.Message)
		return
	}
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	fl, _ := w.(http.Flusher)
	buf := make([]byte, 256<<10)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if fl != nil {
				fl.Flush()
			}
		}
		if err != nil {
			return
		}
	}
}
