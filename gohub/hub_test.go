package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---- test harness: a real hub over the PHP schema, fake agents over HTTP ----

type testHub struct {
	t     *testing.T
	app   *App
	srv   *httptest.Server
	store *Store
}

func newTestHub(t *testing.T) *testHub {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "database.sqlite")
	schema, err := []byte(schemaSQL), error(nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range strings.Split(string(schema), ";\n") {
		if strings.TrimSpace(stmt) != "" {
			if _, err := raw.Exec(stmt); err != nil {
				t.Fatalf("schema: %v\n%s", err, stmt)
			}
		}
	}
	raw.Close()
	enc, _ := NewEncrypter(testKey)
	store, err := openStore(dbPath, enc)
	if err != nil {
		t.Fatal(err)
	}
	app, err := newApp(Config{AppName: "LANHub", Version: "test"}, store)
	if err != nil {
		t.Fatal(err)
	}
	h := &testHub{t: t, app: app, store: store}
	h.srv = httptest.NewServer(app.routes())
	t.Cleanup(h.srv.Close)
	return h
}

func (h *testHub) exec(q string, args ...any) int64 {
	h.t.Helper()
	res, err := h.store.db.Exec(q, args...)
	if err != nil {
		h.t.Fatalf("%v: %s", err, q)
	}
	id, _ := res.LastInsertId()
	return id
}

const totpSecret = "JBSWY3DPEHPK3PXP"

func (h *testHub) user(email string, admin bool) int64 {
	secret, _ := h.store.enc.EncryptString(totpSecret)
	return h.exec(`INSERT INTO users (name, email, password, is_admin, two_factor_secret, two_factor_confirmed_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, strings.Split(email, "@")[0], email, hashPassword("password-123"), admin, secret, nowDB(), nowDB(), nowDB())
}

// machine registers a machine whose agent is the given test server.
func (h *testHub) machine(name string, agent *httptest.Server, os string) int64 {
	u, _ := url.Parse(agent.URL)
	port, _ := strconv.Atoi(u.Port())
	tok, _ := h.store.enc.EncryptString("tok-" + name)
	return h.exec(`INSERT INTO machines (name, host, port, os, agent_token, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		name, u.Hostname(), port, os, tok, nowDB(), nowDB())
}

func (h *testHub) grant(userID, machineID int64, role string) {
	h.exec(`INSERT INTO machine_user (machine_id, user_id, role, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`, machineID, userID, role, nowDB(), nowDB())
}

type client struct {
	h    *testHub
	http *http.Client
	csrf string
}

func (h *testHub) client() *client {
	jar, _ := cookiejar.New(nil)
	return &client{h: h, http: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

var csrfField = regexp.MustCompile(`name="_token" value="([^"]+)"`)
var csrfMeta = regexp.MustCompile(`name="csrf-token" content="([^"]+)"`)

func (c *client) get(path string) (*http.Response, string) {
	c.h.t.Helper()
	resp, err := c.http.Get(c.h.srv.URL + path)
	if err != nil {
		c.h.t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if m := csrfField.FindSubmatch(body); m != nil {
		c.csrf = string(m[1])
	}
	if m := csrfMeta.FindSubmatch(body); m != nil {
		c.csrf = string(m[1])
	}
	return resp, string(body)
}

func (c *client) postForm(path string, form url.Values) (*http.Response, string) {
	c.h.t.Helper()
	if form.Get("_token") == "" {
		form.Set("_token", c.csrf)
	}
	resp, err := c.http.PostForm(c.h.srv.URL+path, form)
	if err != nil {
		c.h.t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, string(body)
}

func (c *client) postJSON(path string, v any) (*http.Response, map[string]any) {
	c.h.t.Helper()
	raw, _ := json.Marshal(v)
	req, _ := http.NewRequest(http.MethodPost, c.h.srv.URL+path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-CSRF-Token", c.csrf)
	resp, err := c.http.Do(req)
	if err != nil {
		c.h.t.Fatal(err)
	}
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()
	return resp, out
}

// login goes through password + TOTP like a browser would.
func (c *client) login(email string) {
	c.h.t.Helper()
	c.get("/login")
	resp, _ := c.postForm("/login", url.Values{"email": {email}, "password": {"password-123"}})
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/verify-2fa" {
		c.h.t.Fatalf("login step 1: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	c.get("/verify-2fa")
	code, _ := totpCode(totpSecret, time.Now().Unix()/30)
	resp, _ = c.postForm("/verify-2fa", url.Values{"code": {code}})
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/dashboard" {
		c.h.t.Fatalf("login step 2: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	c.get("/explorer") // picks up the new session's CSRF token
}

// ---- fake agent ----

type fakeAgent struct {
	mu       sync.Mutex
	files    map[string][]byte // path -> content
	dirs     map[string]bool
	cutShort map[string]int // path -> bytes to send before dropping the connection
	failList map[string]bool
	uploads  []string
	deleted  []string
	activity []ActivityEntry
	failMkdir bool
	honourRange bool
	activityCalls int
	srv      *httptest.Server
	token    string

	direct *fakeDirect // nil: an agent older than 1.3.0 (no fetches)
}

func newFakeAgent(t *testing.T, token string) *fakeAgent {
	a := &fakeAgent{files: map[string][]byte{}, dirs: map[string]bool{"/": true}, cutShort: map[string]int{}, failList: map[string]bool{}, token: token}
	a.srv = httptest.NewServer(http.HandlerFunc(a.serve))
	t.Cleanup(a.srv.Close)
	return a
}

func (a *fakeAgent) serve(w http.ResponseWriter, r *http.Request) {
	if a.direct != nil && a.serveDirect(w, r) {
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+a.token {
		w.WriteHeader(401)
		json.NewEncoder(w).Encode(map[string]string{"message": "Invalid or missing agent token."})
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	q := r.URL.Query()
	fail := func(code int, msg string) {
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(map[string]string{"message": msg})
	}
	switch r.URL.Path {
	case "/api/health":
		if a.direct != nil {
			json.NewEncoder(w).Encode(map[string]any{"hostname": "fake", "os": "linux", "version": "1.3.0", "features": []string{"range", "fetch"}})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"hostname": "fake", "os": "linux", "version": "1.2.0", "features": []string{"range"}})
	case "/api/drives":
		json.NewEncoder(w).Encode(map[string]any{"drives": []Drive{{Path: "/", Label: "/", Free: 10, Total: 20}, {Path: "/home/me", Label: "/home/me", Free: 10, Total: 20}}})
	case "/api/list":
		p := q.Get("path")
		if a.failList[p] {
			fail(403, "Permission denied reading "+p+".")
			return
		}
		if !a.dirs[p] {
			fail(404, p+" is not a directory.")
			return
		}
		entries := []Entry{}
		prefix := strings.TrimRight(p, "/") + "/"
		for d := range a.dirs {
			if d != p && strings.HasPrefix(d, prefix) && !strings.Contains(d[len(prefix):], "/") {
				entries = append(entries, Entry{Name: d[len(prefix):], Path: d, Type: "dir"})
			}
		}
		for f, b := range a.files {
			if strings.HasPrefix(f, prefix) && !strings.Contains(f[len(prefix):], "/") {
				n := int64(len(b))
				entries = append(entries, Entry{Name: f[len(prefix):], Path: f, Type: "file", Size: &n})
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"entries": entries})
	case "/api/download", "/api/preview":
		b, ok := a.files[q.Get("path")]
		if !ok {
			fail(404, q.Get("path")+" is not a file.")
			return
		}
		if rng := r.Header.Get("Range"); a.honourRange && rng != "" {
			var from, to int
			fmt.Sscanf(rng, "bytes=%d-%d", &from, &to)
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", from, to, len(b)))
			w.Header().Set("Content-Length", strconv.Itoa(to-from+1))
			w.WriteHeader(http.StatusPartialContent)
			w.Write(b[from : to+1])
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(b)))
		w.Header().Set("Content-Type", "application/octet-stream")
		if n, cut := a.cutShort[q.Get("path")]; cut {
			w.Write(b[:n])
			// Hijack and close so the client sees a short body.
			if hj, ok := w.(http.Hijacker); ok {
				conn, buf, _ := hj.Hijack()
				buf.Flush()
				conn.Close()
			}
			return
		}
		w.Write(b)
	case "/api/upload":
		dir := q.Get("path")
		f, hdr, err := r.FormFile("file")
		if err != nil {
			fail(400, "file is required.")
			return
		}
		b, _ := io.ReadAll(f)
		p := strings.TrimRight(dir, "/") + "/" + hdr.Filename
		a.dirs[dir] = true
		a.files[p] = b
		a.uploads = append(a.uploads, p)
		n := int64(len(b))
		json.NewEncoder(w).Encode(Entry{Name: hdr.Filename, Path: p, Type: "file", Size: &n})
	case "/api/mkdir":
		var body struct{ Path string }
		json.NewDecoder(r.Body).Decode(&body)
		if a.failMkdir {
			fail(500, "mkdir "+body.Path+": permission denied")
			return
		}
		a.dirs[body.Path] = true
		json.NewEncoder(w).Encode(map[string]string{"path": body.Path})
	case "/api/delete":
		var body struct{ Path string }
		json.NewDecoder(r.Body).Decode(&body)
		a.deleted = append(a.deleted, body.Path)
		delete(a.files, body.Path)
		delete(a.dirs, body.Path)
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	case "/api/search":
		entries := []Entry{}
		for f := range a.files {
			if strings.HasPrefix(f, strings.TrimRight(q.Get("path"), "/")+"/") && strings.Contains(strings.ToLower(filepath.Base(f)), strings.ToLower(q.Get("query"))) {
				entries = append(entries, Entry{Name: filepath.Base(f), Path: f, Type: "file"})
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"entries": entries, "truncated": false})
	case "/api/transfers":
		json.NewEncoder(w).Encode(map[string]any{"transfers": []any{}})
	case "/api/activity":
		a.activityCalls++
		json.NewEncoder(w).Encode(map[string]any{"entries": a.activity})
	default:
		fail(404, "no such endpoint")
	}
}

// ---- tests ----

func TestLoginNeedsPasswordThenTOTPAndRefusesReplay(t *testing.T) {
	h := newTestHub(t)
	h.user("ann@example.com", false)
	c := h.client()
	c.get("/login")

	resp, body := c.postForm("/login", url.Values{"email": {"ann@example.com"}, "password": {"wrong"}})
	if resp.StatusCode != 422 || !strings.Contains(body, "do not match") {
		t.Fatalf("wrong password: %d", resp.StatusCode)
	}
	if resp, _ := c.get("/dashboard"); resp.StatusCode != http.StatusFound {
		t.Fatal("dashboard must need login")
	}
	c.login("ann@example.com")
	if resp, _ := c.get("/dashboard"); resp.StatusCode != 200 {
		t.Fatalf("after login: %d", resp.StatusCode)
	}

	// The same code can't log in a second time (replay guard). Replay the
	// exact step that was used, so a 30 s boundary can't make this flaky.
	u, _ := h.store.UserByEmail("ann@example.com")
	c2 := h.client()
	c2.get("/login")
	c2.postForm("/login", url.Values{"email": {"ann@example.com"}, "password": {"password-123"}})
	c2.get("/verify-2fa")
	code, _ := totpCode(totpSecret, u.TwoFactorLastUsedStep)
	if resp, body := c2.postForm("/verify-2fa", url.Values{"code": {code}}); resp.StatusCode != 422 || !strings.Contains(body, "incorrect or has expired") {
		t.Fatalf("replayed code accepted: %d", resp.StatusCode)
	}
}

func TestLoginLocksOutAfterFiveWrongPasswords(t *testing.T) {
	h := newTestHub(t)
	h.user("bob@example.com", false)
	c := h.client()
	c.get("/login")
	for i := 0; i < 5; i++ {
		c.postForm("/login", url.Values{"email": {"bob@example.com"}, "password": {"nope"}})
	}
	_, body := c.postForm("/login", url.Values{"email": {"bob@example.com"}, "password": {"password-123"}})
	if !strings.Contains(body, "Too many login attempts") {
		t.Fatal("sixth attempt should be locked out even with the right password")
	}
}

func TestPendingInviteCantLogIn(t *testing.T) {
	h := newTestHub(t)
	id := h.user("new@example.com", false)
	h.exec(`UPDATE users SET invite_token = 'abc', invite_token_expires_at = ? WHERE id = ?`, time.Now().Add(time.Hour).UTC().Format(dbTime), id)
	c := h.client()
	c.get("/login")
	if _, body := c.postForm("/login", url.Values{"email": {"new@example.com"}, "password": {"password-123"}}); !strings.Contains(body, "not been activated") {
		t.Fatal("pending users must not get past the password step")
	}
}

func TestPostsWithoutCSRFTokenAreRejected(t *testing.T) {
	h := newTestHub(t)
	h.user("ann@example.com", false)
	c := h.client()
	c.login("ann@example.com")
	resp, _ := c.http.PostForm(h.srv.URL+"/logout", url.Values{})
	if resp.StatusCode != 419 {
		t.Fatalf("logout without token: %d", resp.StatusCode)
	}
	if resp, _ := c.postForm("/logout", url.Values{}); resp.StatusCode != http.StatusFound {
		t.Fatalf("logout with token: %d", resp.StatusCode)
	}
}

func TestMachineAccessRulesOnFileOperations(t *testing.T) {
	h := newTestHub(t)
	ann := h.user("ann@example.com", true) // admin, but admins get no implicit access
	agent := newFakeAgent(t, "tok-box")
	agent.files["/data/a.txt"] = []byte("hello")
	agent.dirs["/data"] = true
	box := h.machine("box", agent.srv, "linux")
	c := h.client()
	c.login("ann@example.com")

	if resp, _ := c.get("/api/machines/" + fmt.Sprint(box) + "/list?path=/data"); resp.StatusCode != 403 {
		t.Fatalf("no grant: list should be 403, got %d", resp.StatusCode)
	}
	if resp, _ := c.get("/machines/" + fmt.Sprint(box) + "/download?path=/data/a.txt"); resp.StatusCode != 403 {
		t.Fatalf("no grant: download should be 403, got %d", resp.StatusCode)
	}
	h.grant(ann, box, "read_only")
	if resp, _ := c.get("/api/machines/" + fmt.Sprint(box) + "/list?path=/data"); resp.StatusCode != 200 {
		t.Fatalf("read_only: list should work, got %d", resp.StatusCode)
	}
	for _, op := range []string{"mkdir", "delete", "rename"} {
		if resp, _ := c.postJSON("/api/machines/"+fmt.Sprint(box)+"/"+op, map[string]string{"path": "/data/x", "new_name": "y"}); resp.StatusCode != 403 {
			t.Fatalf("read_only: %s should be 403, got %d", op, resp.StatusCode)
		}
	}
	h.exec(`UPDATE machine_user SET role = 'full' WHERE user_id = ? AND machine_id = ?`, ann, box)
	if resp, _ := c.postJSON("/api/machines/"+fmt.Sprint(box)+"/mkdir", map[string]string{"path": "/data/new"}); resp.StatusCode != 200 {
		t.Fatalf("full: mkdir should work, got %d", resp.StatusCode)
	}
}

func TestDownloadStreamsWithHeadersAndMapsErrors(t *testing.T) {
	h := newTestHub(t)
	ann := h.user("ann@example.com", false)
	agent := newFakeAgent(t, "tok-box")
	agent.files["/data/report café.pdf"] = bytes.Repeat([]byte("x"), 3<<20)
	agent.dirs["/data"] = true
	box := h.machine("box", agent.srv, "linux")
	h.grant(ann, box, "read_only")
	c := h.client()
	c.login("ann@example.com")

	resp, err := c.http.Get(h.srv.URL + "/machines/" + fmt.Sprint(box) + "/download?path=" + url.QueryEscape("/data/report café.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || len(body) != 3<<20 {
		t.Fatalf("download: %d, %d bytes", resp.StatusCode, len(body))
	}
	if resp.Header.Get("Content-Length") != strconv.Itoa(3<<20) {
		t.Fatalf("Content-Length should be passed through, got %q", resp.Header.Get("Content-Length"))
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, `filename="report caf_.pdf"`) || !strings.Contains(cd, "filename*=utf-8''report%20caf%C3%A9.pdf") {
		t.Fatalf("Content-Disposition: %s", cd)
	}
	if resp, _ := c.get("/machines/" + fmt.Sprint(box) + "/preview?path=" + url.QueryEscape("/data/report café.pdf")); resp.Header.Get("Content-Security-Policy") != "sandbox" {
		t.Fatalf("previews must be sandboxed, got %q", resp.Header.Get("Content-Security-Policy"))
	}
	if resp, _ := c.get("/machines/" + fmt.Sprint(box) + "/download?path=/data/missing"); resp.StatusCode != 404 {
		t.Fatalf("missing file: %d", resp.StatusCode)
	}
	agent.srv.Close()
	if resp, _ := c.get("/machines/" + fmt.Sprint(box) + "/download?path=/data/a"); resp.StatusCode != 502 {
		t.Fatalf("unreachable agent: %d", resp.StatusCode)
	}
}

func waitRelay(t *testing.T, h *testHub, id int64) (status, errMsg string, bytes int64) {
	t.Helper()
	for i := 0; i < 200; i++ {
		var e sql.NullString
		h.store.db.QueryRow(`SELECT status, error, coalesce(bytes_transferred, 0) FROM relay_transfers WHERE id = ?`, id).Scan(&status, &e, &bytes)
		if status == "done" || status == "error" {
			return status, e.String, bytes
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("relay didn't finish")
	return
}

func relaySetup(t *testing.T) (*testHub, *client, *fakeAgent, *fakeAgent, int64, int64) {
	h := newTestHub(t)
	ann := h.user("ann@example.com", false)
	src, dst := newFakeAgent(t, "tok-src"), newFakeAgent(t, "tok-dst")
	s, d := h.machine("src", src.srv, "linux"), h.machine("dst", dst.srv, "linux")
	h.grant(ann, s, "full")
	h.grant(ann, d, "full")
	c := h.client()
	c.login("ann@example.com")
	return h, c, src, dst, s, d
}

func TestRelayCopiesAFileAndMoveDeletesTheSourceOnlyAfterwards(t *testing.T) {
	h, c, src, dst, s, d := relaySetup(t)
	src.dirs["/in"] = true
	src.files["/in/big.bin"] = bytes.Repeat([]byte("ab"), 1<<20)
	dst.dirs["/out"] = true

	resp, out := c.postJSON("/api/drop", map[string]any{"source_machine": s, "source_path": "/in/big.bin", "name": "big.bin", "target_machine": d, "target_dir": "/out", "copy": false})
	if resp.StatusCode != 202 {
		t.Fatalf("drop: %d %v", resp.StatusCode, out)
	}
	status, msg, n := waitRelay(t, h, int64(out["relay_id"].(float64)))
	if status != "done" || n != 2<<20 {
		t.Fatalf("relay: %s %q %d", status, msg, n)
	}
	if !bytes.Equal(dst.files["/out/big.bin"], bytes.Repeat([]byte("ab"), 1<<20)) {
		t.Fatal("destination content differs")
	}
	if len(src.deleted) != 1 || src.deleted[0] != "/in/big.bin" {
		t.Fatalf("a move deletes the source after success: %v", src.deleted)
	}
}

func TestRelayFailsAndKeepsTheSourceWhenTheStreamIsCutShort(t *testing.T) {
	h, c, src, _, s, d := relaySetup(t)
	src.dirs["/in"] = true
	src.files["/in/big.bin"] = bytes.Repeat([]byte("z"), 1<<20)
	src.cutShort["/in/big.bin"] = 4096

	_, out := c.postJSON("/api/drop", map[string]any{"source_machine": s, "source_path": "/in/big.bin", "name": "big.bin", "target_machine": d, "target_dir": "/out", "copy": false})
	status, msg, _ := waitRelay(t, h, int64(out["relay_id"].(float64)))
	if status != "error" {
		t.Fatalf("a cut-short stream must fail the relay, got %s", status)
	}
	if len(src.deleted) != 0 {
		t.Fatalf("a failed move must not delete the source: %v (%s)", src.deleted, msg)
	}
}

func TestFolderRelayRefusesToMergeAndFailsOnUnreadableSubfolders(t *testing.T) {
	h, c, src, dst, s, d := relaySetup(t)
	src.dirs["/in/photos"] = true
	src.dirs["/in/photos/2026"] = true
	src.files["/in/photos/a.jpg"] = []byte("A")
	src.files["/in/photos/2026/b.jpg"] = []byte("B")
	dst.dirs["/out"] = true

	_, out := c.postJSON("/api/drop", map[string]any{"source_machine": s, "source_path": "/in/photos", "name": "photos", "target_machine": d, "target_dir": "/out", "copy": true, "is_dir": true})
	if status, msg, _ := waitRelay(t, h, int64(out["relay_id"].(float64))); status != "done" {
		t.Fatalf("folder copy: %s %s", status, msg)
	}
	if string(dst.files["/out/photos/2026/b.jpg"]) != "B" || string(dst.files["/out/photos/a.jpg"]) != "A" {
		t.Fatalf("folder contents not recreated: %v", dst.uploads)
	}

	// Again: the destination folder now exists, so it must refuse to merge.
	_, out = c.postJSON("/api/drop", map[string]any{"source_machine": s, "source_path": "/in/photos", "name": "photos", "target_machine": d, "target_dir": "/out", "copy": true, "is_dir": true})
	if status, msg, _ := waitRelay(t, h, int64(out["relay_id"].(float64))); status != "error" || !strings.Contains(msg, "already exists") {
		t.Fatalf("merge into existing folder: %s %q", status, msg)
	}

	// An unreadable subfolder fails a move, and nothing is deleted.
	src.failList["/in/photos/2026"] = true
	_, out = c.postJSON("/api/drop", map[string]any{"source_machine": s, "source_path": "/in/photos", "name": "photos2", "target_machine": d, "target_dir": "/out", "copy": false, "is_dir": true})
	if status, _, _ := waitRelay(t, h, int64(out["relay_id"].(float64))); status != "error" || len(src.deleted) != 0 {
		t.Fatalf("unreadable subfolder: %s, deleted %v", status, src.deleted)
	}
}

func TestDropNeedsFullAccessOnTheTarget(t *testing.T) {
	h, c, _, _, s, d := relaySetup(t)
	h.exec(`UPDATE machine_user SET role = 'read_only' WHERE machine_id = ?`, d)
	if resp, _ := c.postJSON("/api/drop", map[string]any{"source_machine": s, "source_path": "/in/x", "name": "x", "target_machine": d, "target_dir": "/out", "copy": true}); resp.StatusCode != 403 {
		t.Fatalf("read-only target: %d", resp.StatusCode)
	}
}

func TestDedupeDrivesDropsNestedAndDuplicateDrives(t *testing.T) {
	got := dedupeDrives([]Drive{{Path: "/"}, {Path: "/home/nas"}, {Path: "/mnt/data"}}, "/")
	if len(got) != 1 || got[0].Path != "/" {
		t.Fatalf("linux: %v", got)
	}
	got = dedupeDrives([]Drive{{Path: `C:\`}, {Path: `D:\`}, {Path: `D:\`}}, `\`)
	if len(got) != 2 {
		t.Fatalf("windows: %v", got)
	}
}

func TestGlobalSearchStreamsResultsAndReportsFailures(t *testing.T) {
	h := newTestHub(t)
	ann := h.user("ann@example.com", false)
	good := newFakeAgent(t, "tok-good")
	good.files["/home/me/invoice-1.pdf"] = []byte("x")
	good.dirs["/home/me"] = true
	gid := h.machine("good", good.srv, "linux")
	bad := newFakeAgent(t, "tok-bad")
	bid := h.machine("bad", bad.srv, "linux")
	bad.srv.Close()
	h.grant(ann, gid, "read_only")
	h.grant(ann, bid, "read_only")
	c := h.client()
	c.login("ann@example.com")

	resp, body := c.get("/api/search?q=invoice")
	if resp.StatusCode != 200 {
		t.Fatalf("search: %d", resp.StatusCode)
	}
	var hits, failed, done int
	for _, line := range strings.Split(strings.TrimSpace(body), "\n") {
		var ev searchEvent
		json.Unmarshal([]byte(line), &ev)
		switch ev.Type {
		case "results":
			hits += len(ev.Entries)
		case "failed":
			failed++
		case "done":
			done++
		}
	}
	// "/" and "/home/me" are both drives on the fake agent; the file must
	// still be reported once.
	if hits != 1 || failed != 1 || done != 1 {
		t.Fatalf("hits %d failed %d done %d\n%s", hits, failed, done, body)
	}
}

func TestUploadStreamsToTheAgent(t *testing.T) {
	h := newTestHub(t)
	ann := h.user("ann@example.com", false)
	agent := newFakeAgent(t, "tok-box")
	agent.dirs["/data"] = true
	box := h.machine("box", agent.srv, "linux")
	h.grant(ann, box, "full")
	c := h.client()
	c.login("ann@example.com")

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, _ := mw.CreateFormFile("files", "notes.txt")
	part.Write([]byte("uploaded through the hub"))
	mw.Close()
	req, _ := http.NewRequest(http.MethodPost, h.srv.URL+"/api/machines/"+fmt.Sprint(box)+"/upload?path=/data", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-CSRF-Token", c.csrf)
	resp, err := c.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 || string(agent.files["/data/notes.txt"]) != "uploaded through the hub" {
		t.Fatalf("upload: %d %v", resp.StatusCode, agent.uploads)
	}
}

func TestStaleRelaysAreMarkedFailedOnStartup(t *testing.T) {
	h, _, _, _, s, d := relaySetup(t)
	id := h.exec(`INSERT INTO relay_transfers (source_machine_id, destination_machine_id, source_path, destination_path, kind, status, created_at, updated_at)
		VALUES (?, ?, '/a', '/b', 'move', 'downloading', ?, ?)`, s, d, nowDB(), nowDB())
	h.app.relays.RecoverStale()
	var status string
	h.store.db.QueryRow(`SELECT status FROM relay_transfers WHERE id = ?`, id).Scan(&status)
	if status != "error" {
		t.Fatalf("got %s", status)
	}
}

func TestDropOntoADriveListIsRefusedNotAimedAtTheRoot(t *testing.T) {
	_, c, _, _, s, d := relaySetup(t)
	resp, out := c.postJSON("/api/drop", map[string]any{"source_machine": s, "source_path": "/in/photos", "name": "photos", "target_machine": d, "target_dir": nil, "copy": true, "is_dir": true})
	if resp.StatusCode != 422 || !strings.Contains(fmt.Sprint(out["message"]), "Open a folder on dst first") {
		t.Fatalf("got %d %v", resp.StatusCode, out)
	}
}

func TestFolderRelayExplainsAFolderItCantCreate(t *testing.T) {
	h, c, src, dst, s, d := relaySetup(t)
	src.dirs["/in/photos"] = true
	dst.dirs["/out"] = true
	dst.failMkdir = true
	_, out := c.postJSON("/api/drop", map[string]any{"source_machine": s, "source_path": "/in/photos", "name": "photos", "target_machine": d, "target_dir": "/out", "copy": true, "is_dir": true})
	status, msg, _ := waitRelay(t, h, int64(out["relay_id"].(float64)))
	if status != "error" || !strings.Contains(msg, "Couldn't create /out/photos on dst") || !strings.Contains(msg, "permission denied") {
		t.Fatalf("got %s %q", status, msg)
	}
}

func TestMediaStreamsVideoWithItsTypeAndPassesRangeThrough(t *testing.T) {
	h := newTestHub(t)
	ann := h.user("ann@example.com", false)
	agent := newFakeAgent(t, "tok-box")
	agent.dirs["/v"] = true
	agent.files["/v/clip.mp4"] = bytes.Repeat([]byte("v"), 4096)
	agent.files["/v/notes.txt"] = []byte("x")
	box := h.machine("box", agent.srv, "linux")
	h.grant(ann, box, "read_only")
	c := h.client()
	c.login("ann@example.com")

	resp, body := c.get("/machines/" + fmt.Sprint(box) + "/media?path=/v/clip.mp4")
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "video/mp4" || len(body) != 4096 {
		t.Fatalf("video: %d %s %d bytes", resp.StatusCode, resp.Header.Get("Content-Type"), len(body))
	}
	if resp, _ := c.get("/machines/" + fmt.Sprint(box) + "/media?path=/v/notes.txt"); resp.StatusCode != 415 {
		t.Fatalf("non-media files must be refused, got %d", resp.StatusCode)
	}
	// An agent that honours Range gets the request and its 206 is passed on.
	agent.honourRange = true
	req, _ := http.NewRequest(http.MethodGet, h.srv.URL+"/machines/"+fmt.Sprint(box)+"/media?path=/v/clip.mp4", nil)
	req.Header.Set("Range", "bytes=100-199")
	resp2, err := c.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	part, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	if resp2.StatusCode != 206 || len(part) != 100 || resp2.Header.Get("Content-Range") != "bytes 100-199/4096" {
		t.Fatalf("range: %d %d bytes %q", resp2.StatusCode, len(part), resp2.Header.Get("Content-Range"))
	}
}
