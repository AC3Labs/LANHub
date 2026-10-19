package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCompareVersions(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"0.12.0.0", "0.11.0.1", 1}, {"0.11.0.1", "0.12.0.0", -1}, {"0.12.0.0", "0.12.0.0", 0},
		{"0.12", "0.12.0.0", 0}, {"0.10.0.0", "0.9.3.0", 1}, {"v1.0", "0.99", 1}, {"dev", "0.0.0.1", -1},
	} {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestVersionFileMatchesTheHub(t *testing.T) {
	b, err := os.ReadFile("../VERSION")
	if err != nil {
		t.Fatal(err)
	}
	if v := strings.TrimSpace(string(b)); v != hubVersion {
		t.Fatalf("VERSION says %s but hubVersion is %s; the image label and the program would disagree", v, hubVersion)
	}
}

func TestChangelogBetweenPicksTheNewSections(t *testing.T) {
	text := "# Changelog\n\n## [0.13.0.0] - x\nthirteen\n\n## [0.12.1.0] - x\ntwelve-one\n\n## [0.12.0.0] - x\ntwelve\n"
	got := changelogBetween(text, "0.12.0.0", "0.12.1.0")
	if !strings.Contains(got, "twelve-one") || strings.Contains(got, "thirteen") || strings.Contains(got, "\ntwelve\n") {
		t.Fatalf("got %q", got)
	}
}

// ---- a fake registry that behaves like ghcr.io ----

type fakeRegistry struct {
	srv      *httptest.Server
	blobs    map[string][]byte
	tags     map[string][]byte // tag -> index or manifest
	token    string
	tamper   bool // serve a layer that doesn't match its digest
	requests int
}

func digestOf(b []byte) string {
	s := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(s[:])
}

func layerWith(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
		tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

// addImage publishes tag with the given layers, wrapped in a multi-platform
// index the way buildx publishes (an attestation entry included).
func (r *fakeRegistry) addImage(tag, version string, layers ...[]byte) {
	cfg, _ := json.Marshal(map[string]any{"config": map[string]any{"Labels": map[string]string{
		"org.opencontainers.image.version": version, "org.opencontainers.image.revision": "abc123"}}})
	r.blobs[digestOf(cfg)] = cfg
	var ls []map[string]any
	for _, l := range layers {
		r.blobs[digestOf(l)] = l
		ls = append(ls, map[string]any{"mediaType": "application/vnd.oci.image.layer.v1.tar+gzip", "digest": digestOf(l), "size": len(l)})
	}
	man, _ := json.Marshal(map[string]any{"schemaVersion": 2, "mediaType": "application/vnd.oci.image.manifest.v1+json",
		"config": map[string]any{"mediaType": "application/vnd.oci.image.config.v1+json", "digest": digestOf(cfg), "size": len(cfg)}, "layers": ls})
	r.blobs[digestOf(man)] = man
	idx, _ := json.Marshal(map[string]any{"schemaVersion": 2, "mediaType": "application/vnd.oci.image.index.v1+json", "manifests": []map[string]any{
		{"digest": "sha256:" + strings.Repeat("0", 64), "platform": map[string]string{"os": "unknown", "architecture": "unknown"}},
		{"digest": digestOf(man), "platform": map[string]string{"os": "linux", "architecture": "amd64"}},
	}})
	r.tags[tag] = idx
}

func newFakeRegistry(t *testing.T) *fakeRegistry {
	r := &fakeRegistry{blobs: map[string][]byte{}, tags: map[string][]byte{}, token: "anon-token"}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.requests++
		if req.URL.Path == "/token" {
			if req.URL.Query().Get("scope") != "repository:ac3labs/lanhub:pull" {
				http.Error(w, "bad scope", 400)
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"token": r.token})
			return
		}
		if req.Header.Get("Authorization") != "Bearer "+r.token {
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="%s/token",service="fake",scope="repository:ac3labs/lanhub:pull"`, r.srv.URL))
			http.Error(w, "unauthorized", 401)
			return
		}
		rest, ok := strings.CutPrefix(req.URL.Path, "/v2/ac3labs/lanhub/")
		if !ok {
			http.NotFound(w, req)
			return
		}
		kind, ref, _ := strings.Cut(rest, "/")
		var body []byte
		if kind == "manifests" {
			body = r.tags[ref]
			if body == nil {
				body = r.blobs[ref]
			}
		} else {
			body = r.blobs[ref]
			if r.tamper && len(body) > 0 && strings.Contains(req.URL.Path, "blobs") && body[0] == 0x1f {
				body = append([]byte{}, body...)
				body[len(body)-1] ^= 0xff
			}
		}
		if body == nil {
			http.NotFound(w, req)
			return
		}
		w.Write(body)
	}))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *fakeRegistry) ref() string {
	return "http://" + strings.TrimPrefix(r.srv.URL, "http://") + "/ac3labs/lanhub"
}

func TestRegistryClientReadsVersionAndExtractsTheProgram(t *testing.T) {
	reg := newFakeRegistry(t)
	reg.addImage("hub", "0.99.0.0",
		layerWith(t, map[string]string{"etc/ssl/certs/ca-certificates.crt": "certs"}),
		layerWith(t, map[string]string{"lanhub-hub": "#!/bin/sh\necho 0.99.0.0\n"}),
		layerWith(t, map[string]string{"data/.keep": ""}))
	c := newRegistryClient(reg.ref())
	img, err := c.image(context.Background(), "hub")
	if err != nil {
		t.Fatal(err)
	}
	if img.Version != "0.99.0.0" || img.Revision != "abc123" || len(img.Layers) != 3 {
		t.Fatalf("image = %+v", img)
	}
	dst := filepath.Join(t.TempDir(), "hub")
	sum, err := c.extractFile(context.Background(), img, "lanhub-hub", dst, nil)
	if err != nil {
		t.Fatal(err)
	}
	if fileSHA256(dst) != sum || sum == "" {
		t.Fatalf("checksum %s vs file %s", sum, fileSHA256(dst))
	}
	if _, err := c.extractFile(context.Background(), img, "nope", dst+"2", nil); err == nil {
		t.Fatal("found a file that isn't there")
	}

	reg.tamper = true
	if _, err := c.extractFile(context.Background(), img, "lanhub-hub", dst+"3", nil); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("a tampered layer was accepted (err %v)", err)
	}
	if _, err := os.Stat(dst + "3"); err == nil {
		t.Fatal("a file from a tampered layer was left behind")
	}
}

func TestStagedHubStartupRules(t *testing.T) {
	t.Setenv("LANHUB_STAGED", "")
	dir := t.TempDir()
	bin := filepath.Join(dir, "lanhub-hub")
	os.WriteFile(bin, []byte("new hub"), 0o755)
	stage := func(m stagedManifest) {
		m.SHA256 = fileSHA256(bin)
		writeStaged(dir, &m)
	}

	stage(stagedManifest{Version: "0.13.0.0"})
	if got := stagedHubToRun(dir, "0.12.0.0"); got != bin {
		t.Fatalf("newer staged hub not chosen: %q", got)
	}
	if m := readStaged(dir); m.Attempts != 1 {
		t.Fatalf("attempts = %d", m.Attempts)
	}

	stage(stagedManifest{Version: "0.13.0.0", Attempts: stagedFailAfter})
	if got := stagedHubToRun(dir, "0.12.0.0"); got != "" {
		t.Fatal("a hub that failed three starts was tried again")
	}
	if m := readStaged(dir); !m.Failed || !strings.Contains(m.FailedReason, "0.12.0.0") {
		t.Fatalf("not marked failed: %+v", m)
	}

	stage(stagedManifest{Version: "0.13.0.0"})
	os.WriteFile(bin, []byte("corrupted"), 0o755)
	if got := stagedHubToRun(dir, "0.12.0.0"); got != "" || !readStaged(dir).Failed {
		t.Fatal("a staged hub that doesn't match its checksum was run")
	}

	os.WriteFile(bin, []byte("new hub"), 0o755)
	stage(stagedManifest{Version: "0.12.0.0"})
	if got := stagedHubToRun(dir, "0.12.0.0"); got != "" {
		t.Fatal("ran a staged hub that isn't newer than the image")
	}
	if _, err := os.Stat(dir); err == nil {
		t.Fatal("the redundant staged copy wasn't removed")
	}

	os.MkdirAll(dir, 0o755)
	os.WriteFile(bin, []byte("new hub"), 0o755)
	stage(stagedManifest{Version: "0.13.0.0"})
	t.Setenv("LANHUB_STAGED", "1")
	if got := stagedHubToRun(dir, "0.13.0.0"); got != "" {
		t.Fatal("the staged hub tried to exec itself")
	}
}

func TestUpdateEmailIsMultipartWithAButton(t *testing.T) {
	text, html := updateEmail("Andrew", "0.13.0.0", "0.12.0.0", "https://hub.example/settings?tab=updates&install=0.13.0.0", "## [0.13.0.0]\n- <new> thing")
	if !strings.Contains(text, "https://hub.example/settings?tab=updates&install=0.13.0.0") {
		t.Fatal("text part has no link")
	}
	if !strings.Contains(html, ">Update LANHub</a>") || !strings.Contains(html, "&amp;install=0.13.0.0") || strings.Contains(html, "<new>") {
		t.Fatalf("html part: %s", html)
	}
	raw := buildMessage("LANHub <lab@example.com>", "a@example.com", "LANHub 0.13.0.0 is available", text, html)
	msg, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	mt, params, _ := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if mt != "multipart/alternative" {
		t.Fatalf("content type %s", mt)
	}
	mr := multipart.NewReader(msg.Body, params["boundary"])
	var types []string
	for {
		p, err := mr.NextPart()
		if err != nil {
			break
		}
		types = append(types, p.Header.Get("Content-Type"))
		b := new(bytes.Buffer)
		b.ReadFrom(p) // multipart decodes quoted-printable
		if strings.HasPrefix(p.Header.Get("Content-Type"), "text/html") && !strings.Contains(b.String(), "Update LANHub") {
			t.Fatal("decoded html part lost its button")
		}
	}
	if len(types) != 2 {
		t.Fatalf("parts: %v", types)
	}
	for _, line := range strings.Split(raw, "\r\n") {
		if len(line) > 998 {
			t.Fatalf("line longer than SMTP allows (%d)", len(line))
		}
	}
}

func TestUpdatesTabBannerAndPermissions(t *testing.T) {
	h := newTestHub(t)
	h.user("admin@example.com", true)
	h.user("user@example.com", false)
	h.app.setSetting("update_latest", "0.99.0.0")
	h.app.setSetting("update_notes", "## [0.99.0.0]\n- shiny")

	admin := h.client()
	admin.login("admin@example.com")
	_, body := admin.get("/dashboard")
	if !strings.Contains(body, "LANHub 0.99.0.0</b> is available") {
		t.Fatal("admins don't see the update banner")
	}
	resp, body := admin.get("/settings?tab=updates")
	if resp.StatusCode != 200 || !strings.Contains(body, "Update to 0.99.0.0") || !strings.Contains(body, "shiny") || !strings.Contains(body, `aria-current="page">Updates`) {
		t.Fatalf("updates tab: %d", resp.StatusCode)
	}
	if _, body := admin.get("/settings"); !strings.Contains(body, "Email (SMTP)") || strings.Contains(body, "Check for updates") {
		t.Fatal("the Email tab should be the default and show only mail settings")
	}

	user := h.client()
	user.login("user@example.com")
	if _, body := user.get("/dashboard"); strings.Contains(body, "is available") {
		t.Fatal("non-admins see the update banner")
	}
	if resp, _ := user.postForm("/settings/updates/install", url.Values{"version": {"0.99.0.0"}}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin install: %d", resp.StatusCode)
	}

	// Without the CSRF token, nothing happens.
	plain := admin.http
	resp, err := plain.PostForm(h.srv.URL+"/settings/updates/install", url.Values{"version": {"0.99.0.0"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 419 || h.app.upd.status().Phase != "" {
		t.Fatalf("install without CSRF: %d, phase %q", resp.StatusCode, h.app.upd.status().Phase)
	}

	// An old version from a stale page is refused.
	admin.postForm("/settings/updates/install", url.Values{"version": {"0.98.0.0"}})
	if h.app.upd.status().Phase != "" {
		t.Fatal("installed a version that isn't the latest")
	}

	// Options are saved.
	admin.postForm("/settings/updates/options", url.Values{"auto_check": {"1"}})
	if h.store.Setting("update_auto_check") != "1" || h.store.Setting("update_email") != "0" {
		t.Fatal("update options weren't saved")
	}
}

func TestCheckAndInstallFromARegistry(t *testing.T) {
	h := newTestHub(t)
	h.user("admin@example.com", true)
	reg := newFakeRegistry(t)
	hubProg := "#!/bin/sh\necho 0.99.0.0\n"
	mediaProg := "#!/bin/sh\necho 0.99.0.0\n"
	reg.addImage("hub", "0.99.0.0", layerWith(t, map[string]string{"lanhub-hub": hubProg}))
	reg.addImage("media", "0.99.0.0", layerWith(t, map[string]string{"etc/alpine-release": "3.22"}),
		layerWith(t, map[string]string{"usr/local/bin/lanhub-media": mediaProg}))

	u := h.app.upd
	u.image, u.notesURL = reg.ref(), "off"
	u.hubDir, u.mediaDir = filepath.Join(t.TempDir(), "hub"), filepath.Join(t.TempDir(), "media")

	v, err := u.check(context.Background(), true)
	if err != nil || v != "0.99.0.0" || u.latest() != "0.99.0.0" || h.store.Setting("update_notified") != "0.99.0.0" {
		t.Fatalf("check: %v %q latest %q", err, v, u.latest())
	}

	if err := u.install("0.99.0.0"); err != nil {
		t.Fatal(err)
	}
	if err := u.install("0.99.0.0"); err == nil {
		t.Fatal("a second install ran at the same time")
	}
	select {
	case bin := <-u.restartTo:
		if bin != filepath.Join(u.hubDir, "lanhub-hub") {
			t.Fatalf("restart into %s", bin)
		}
	case <-time.After(20 * time.Second):
		t.Fatalf("install didn't finish: %+v", u.status())
	}
	hm, mm := readStaged(u.hubDir), readStaged(u.mediaDir)
	if hm == nil || hm.Version != "0.99.0.0" || hm.Attempts != 1 || hm.SHA256 != fileSHA256(filepath.Join(u.hubDir, "lanhub-hub")) {
		t.Fatalf("hub manifest %+v", hm)
	}
	if mm == nil || mm.Version != "0.99.0.0" || mm.SHA256 != fileSHA256(filepath.Join(u.mediaDir, "lanhub-media")) {
		t.Fatalf("media manifest %+v", mm)
	}
	if st := u.status(); st.Phase != "restarting" {
		t.Fatalf("phase %q", st.Phase)
	}

	// A program that lies about its version is refused.
	reg.addImage("hub", "0.99.0.1", layerWith(t, map[string]string{"lanhub-hub": "#!/bin/sh\necho 0.1.0.0\n"}))
	u.mu.Lock()
	u.phase = ""
	u.mu.Unlock()
	u.check(context.Background(), false)
	u.install("0.99.0.1")
	deadline := time.Now().Add(20 * time.Second)
	for u.status().Phase == "downloading" && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if st := u.status(); st.Phase != "failed" || !strings.Contains(st.Detail, "0.1.0.0") {
		t.Fatalf("lying program: %+v", st)
	}
	if readStaged(u.hubDir).Version != "0.99.0.0" {
		t.Fatal("a failed install replaced the staged hub")
	}
}

func TestLoginReturnsToTheIntendedPageOnly(t *testing.T) {
	h := newTestHub(t)
	h.user("admin@example.com", true)
	c := h.client()
	if resp, _ := c.get("/settings?tab=updates&install=0.99.0.0"); resp.Header.Get("Location") != "/login" {
		t.Fatalf("logged-out visit went to %q", resp.Header.Get("Location"))
	}
	c.get("/login")
	c.postForm("/login", url.Values{"email": {"admin@example.com"}, "password": {"password-123"}})
	c.get("/verify-2fa")
	code, _ := totpCode(totpSecret, time.Now().Unix()/30)
	resp, _ := c.postForm("/verify-2fa", url.Values{"code": {code}})
	if got := resp.Header.Get("Location"); got != "/settings?tab=updates&install=0.99.0.0" {
		t.Fatalf("after login went to %q", got)
	}
	for in, want := range map[string]string{"//evil.example/x": "/dashboard", "https://evil.example": "/dashboard", "/\\evil": "/dashboard", "": "/dashboard", "/agents": "/agents", "/a\r\nSet-Cookie: x": "/dashboard"} {
		if got := safeIntended(in); got != want {
			t.Errorf("safeIntended(%q) = %q", in, got)
		}
	}
}
