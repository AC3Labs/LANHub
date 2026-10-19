package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// fakeDirect gives a fakeAgent the 1.3.0 fetch endpoints, enough to drive
// the hub's side of a direct transfer.
type fakeDirect struct {
	mu            sync.Mutex
	fetches       map[string]*fakeFetch
	passDownloads int
	retired       []string
	badHash       bool   // /api/hash answers with the wrong checksum
	sourceURL     string // replaces the source_url a fetch is given (unreachable source)
}

type fakeFetch struct {
	dest, status, errMsg string
	data                 []byte
}

func enableDirect(a *fakeAgent) *fakeDirect {
	a.direct = &fakeDirect{fetches: map[string]*fakeFetch{}}
	return a.direct
}

// serveDirect handles the fetch endpoints; false means "not mine".
func (a *fakeAgent) serveDirect(w http.ResponseWriter, r *http.Request) bool {
	d := a.direct
	reply := func(code int, v any) {
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(v)
	}
	path := r.URL.Path
	if path == "/api/pass-download" {
		// Checked like the real agent: signed with this agent's token.
		body, sig, _ := strings.Cut(r.Header.Get("X-LANHub-Pass"), ".")
		payload, _ := base64.RawURLEncoding.DecodeString(body)
		got, _ := base64.RawURLEncoding.DecodeString(sig)
		mac := hmac.New(sha256.New, []byte(a.token))
		mac.Write(payload)
		var p passFields
		if !hmac.Equal(got, mac.Sum(nil)) || json.Unmarshal(payload, &p) != nil {
			reply(401, map[string]string{"message": "invalid pass."})
			return true
		}
		a.mu.Lock()
		b, ok := a.files[p.Path]
		a.mu.Unlock()
		if !ok || int64(len(b)) != p.Size {
			reply(409, map[string]string{"message": "changed"})
			return true
		}
		d.mu.Lock()
		d.passDownloads++
		d.mu.Unlock()
		w.Write(b)
		return true
	}
	if r.Header.Get("Authorization") != "Bearer "+a.token {
		return false
	}
	switch {
	case path == "/api/hash":
		a.mu.Lock()
		sum := sha256.Sum256(a.files[r.URL.Query().Get("path")])
		a.mu.Unlock()
		if d.badHash {
			sum[0] ^= 0xff
		}
		reply(200, map[string]any{"sha256": hex.EncodeToString(sum[:])})
	case path == "/api/passes/retire":
		var body struct {
			Job  int64
			File int
		}
		json.NewDecoder(r.Body).Decode(&body)
		d.mu.Lock()
		d.retired = append(d.retired, fmt.Sprintf("%d/%d", body.Job, body.File))
		d.mu.Unlock()
		reply(200, map[string]bool{"retired": true})
	case path == "/api/fetches" && r.Method == http.MethodPost:
		var body struct {
			SourceURL   string `json:"source_url"`
			Pass        string `json:"pass"`
			Destination string `json:"destination"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		a.mu.Lock()
		_, exists := a.files[body.Destination]
		a.mu.Unlock()
		if exists {
			reply(409, map[string]string{"message": body.Destination + " already exists."})
			return true
		}
		if d.sourceURL != "" {
			body.SourceURL = d.sourceURL
		}
		f := &fakeFetch{dest: body.Destination, status: "fetching"}
		d.mu.Lock()
		id := fmt.Sprintf("f%d", len(d.fetches)+1)
		d.fetches[id] = f
		d.mu.Unlock()
		go func() {
			req, _ := http.NewRequest(http.MethodGet, body.SourceURL, nil)
			req.Header.Set("X-LANHub-Pass", body.Pass)
			resp, err := http.DefaultClient.Do(req)
			d.mu.Lock()
			defer d.mu.Unlock()
			if err != nil {
				f.status, f.errMsg = "error", err.Error()
				return
			}
			defer resp.Body.Close()
			data, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != 200 {
				f.status, f.errMsg = "error", "source answered "+resp.Status
				return
			}
			f.data, f.status = data, "received"
		}()
		reply(200, map[string]any{"id": id, "status": "fetching"})
	case strings.HasPrefix(path, "/api/fetches/"):
		rest := strings.TrimPrefix(path, "/api/fetches/")
		id, action, _ := strings.Cut(rest, "/")
		d.mu.Lock()
		f := d.fetches[id]
		d.mu.Unlock()
		if f == nil {
			reply(404, map[string]string{"message": "Unknown fetch id."})
			return true
		}
		d.mu.Lock()
		defer d.mu.Unlock()
		switch action {
		case "":
			sum := sha256.Sum256(f.data)
			out := map[string]any{"id": id, "status": f.status, "bytes": len(f.data)}
			if f.status == "received" {
				out["sha256"] = hex.EncodeToString(sum[:])
			}
			if f.errMsg != "" {
				out["error"] = f.errMsg
			}
			reply(200, out)
		case "commit":
			a.mu.Lock()
			defer a.mu.Unlock()
			if _, exists := a.files[f.dest]; exists {
				reply(409, map[string]string{"message": f.dest + " already exists."})
				return true
			}
			a.files[f.dest] = f.data
			f.status = "committed"
			n := int64(len(f.data))
			reply(200, Entry{Name: f.dest, Path: f.dest, Type: "file", Size: &n})
		case "abort":
			f.status, f.data = "aborted", nil
			reply(200, map[string]string{"status": "aborted"})
		case "pass":
			reply(200, map[string]string{"status": f.status})
		}
	default:
		return false
	}
	return true
}

func TestDirectTransferSendsFilesAgentToAgent(t *testing.T) {
	h, c, src, dst, s, d := relaySetup(t)
	sd, dd := enableDirect(src), enableDirect(dst)
	src.dirs["/in/photos"] = true
	src.dirs["/in/photos/2026"] = true
	src.files["/in/photos/a.jpg"] = bytes.Repeat([]byte("A"), 5000)
	src.files["/in/photos/2026/b.jpg"] = []byte("B")
	dst.dirs["/out"] = true

	_, out := c.postJSON("/api/drop", map[string]any{"source_machine": s, "source_path": "/in/photos", "name": "photos", "target_machine": d, "target_dir": "/out", "copy": false, "is_dir": true})
	status, msg, n := waitRelay(t, h, int64(out["relay_id"].(float64)))
	if status != "done" || n != 5001 {
		t.Fatalf("direct folder move: %s %q %d", status, msg, n)
	}
	if !bytes.Equal(dst.files["/out/photos/a.jpg"], bytes.Repeat([]byte("A"), 5000)) {
		t.Fatal("a.jpg didn't arrive")
	}
	if string(dst.files["/out/photos/2026/b.jpg"]) != "B" {
		t.Fatal("b.jpg didn't arrive")
	}
	if len(dst.uploads) != 0 {
		t.Fatalf("files went through the hub: %v", dst.uploads)
	}
	if sd.passDownloads != 2 || len(sd.retired) != 2 || len(dd.fetches) != 2 {
		t.Fatalf("pass downloads %d, retired %d, fetches %d", sd.passDownloads, len(sd.retired), len(dd.fetches))
	}
	if len(src.deleted) != 1 || src.deleted[0] != "/in/photos" {
		t.Fatalf("a move deletes the source folder after success: %v", src.deleted)
	}
}

func TestDirectTransferFallsBackToTheRelay(t *testing.T) {
	// Destination can't reach the source: the hub relays instead.
	h, c, src, dst, s, d := relaySetup(t)
	enableDirect(src)
	enableDirect(dst).sourceURL = "http://127.0.0.1:1/api/pass-download"
	src.dirs["/in"] = true
	src.files["/in/big.bin"] = bytes.Repeat([]byte("ab"), 1<<16)
	src.files["/in/two.bin"] = []byte("two")
	dst.dirs["/out"] = true
	_, out := c.postJSON("/api/drop", map[string]any{"source_machine": s, "source_path": "/in", "name": "in", "target_machine": d, "target_dir": "/out", "copy": true, "is_dir": true})
	if status, msg, n := waitRelay(t, h, int64(out["relay_id"].(float64))); status != "done" || n != 2<<16+3 {
		t.Fatalf("fallback: %s %q %d", status, msg, n)
	}
	if len(dst.uploads) != 2 || len(dst.direct.fetches) != 1 {
		t.Fatalf("expected one failed fetch, then relays for both files: uploads %v, fetches %d", dst.uploads, len(dst.direct.fetches))
	}

	// Only one side is 1.3.0: straight to the relay, no fetch attempted.
	h2, c2, src2, dst2, s2, d2 := relaySetup(t)
	enableDirect(dst2)
	src2.dirs["/in"] = true
	src2.files["/in/x.bin"] = []byte("x")
	_, out = c2.postJSON("/api/drop", map[string]any{"source_machine": s2, "source_path": "/in/x.bin", "name": "x.bin", "target_machine": d2, "target_dir": "/out", "copy": true})
	if status, _, _ := waitRelay(t, h2, int64(out["relay_id"].(float64))); status != "done" || len(dst2.direct.fetches) != 0 || len(dst2.uploads) != 1 {
		t.Fatalf("old source agent: %s, fetches %d, uploads %v", status, len(dst2.direct.fetches), dst2.uploads)
	}
}

func TestDirectTransferRejectsAChecksumMismatchAndKeepsTheSource(t *testing.T) {
	h, c, src, dst, s, d := relaySetup(t)
	enableDirect(src).badHash = true
	enableDirect(dst)
	src.dirs["/in"] = true
	src.files["/in/a.bin"] = []byte("original")
	_, out := c.postJSON("/api/drop", map[string]any{"source_machine": s, "source_path": "/in/a.bin", "name": "a.bin", "target_machine": d, "target_dir": "/out", "copy": false})
	status, msg, _ := waitRelay(t, h, int64(out["relay_id"].(float64)))
	if status != "error" || !strings.Contains(msg, "checksum") {
		t.Fatalf("mismatch: %s %q", status, msg)
	}
	if _, kept := dst.files["/out/a.bin"]; kept || len(src.deleted) != 0 || len(dst.uploads) != 0 {
		t.Fatalf("after a mismatch: dest kept %v, deleted %v, uploads %v", kept, src.deleted, dst.uploads)
	}
}

func TestDirectTransferNeverOverwritesAnExistingFile(t *testing.T) {
	h, c, src, dst, s, d := relaySetup(t)
	enableDirect(src)
	enableDirect(dst)
	src.dirs["/in"] = true
	src.files["/in/a.bin"] = []byte("new")
	dst.dirs["/out"] = true
	dst.files["/out/a.bin"] = []byte("keep me")
	_, out := c.postJSON("/api/drop", map[string]any{"source_machine": s, "source_path": "/in/a.bin", "name": "a.bin", "target_machine": d, "target_dir": "/out", "copy": false})
	if status, msg, _ := waitRelay(t, h, int64(out["relay_id"].(float64))); status != "error" || !strings.Contains(msg, "already exists") {
		t.Fatalf("existing name: %s %q", status, msg)
	}
	if string(dst.files["/out/a.bin"]) != "keep me" || len(dst.uploads) != 0 || len(src.deleted) != 0 {
		t.Fatal("the existing file was replaced or the source deleted")
	}
}

func TestAgentsPageRendersFirstAndEachCardChecksItsAgent(t *testing.T) {
	h, c, src, _, s, _ := relaySetup(t)
	enableDirect(src)
	h.exec(`UPDATE machines SET status = 'offline' WHERE id = ?`, s)
	src.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" && r.Header.Get("X-Slow") == "" {
			t.Errorf("the page itself pinged an agent")
		}
		src.serve(w, r)
	})
	resp, body := c.get("/agents")
	if resp.StatusCode != 200 || !strings.Contains(body, "Checking…") || !strings.Contains(body, "agentCard(") {
		t.Fatalf("agents page: %d", resp.StatusCode)
	}
	src.srv.Config.Handler = http.HandlerFunc(src.serve)

	var out map[string]any
	r, _ := c.http.Get(h.srv.URL + "/api/agents/" + fmt.Sprint(s) + "/status")
	json.NewDecoder(r.Body).Decode(&out)
	r.Body.Close()
	if out["status"] != "online" || out["version"] != "1.3.0" || out["direct"] != true {
		t.Fatalf("status: %v", out)
	}
	var st string
	h.store.db.QueryRow(`SELECT status FROM machines WHERE id = ?`, s).Scan(&st)
	if st != "online" {
		t.Fatalf("status not recorded: %s", st)
	}

	// No access to a machine: no status for it either.
	other := h.machine("hidden", src.srv, "linux")
	if r, _ := c.http.Get(h.srv.URL + "/api/agents/" + fmt.Sprint(other) + "/status"); r.StatusCode != 403 {
		t.Fatalf("status of a machine without access: %d", r.StatusCode)
	}
}
