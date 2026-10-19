package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Updating LANHub from its own Settings page.
//
// A container can't replace its own image, and handing the hub the Docker
// socket would make it root on the server. Instead the hub downloads the new
// hub and LANHub Media programs out of the same published images that
// `docker compose pull` would fetch (anonymously, every blob checked against
// its sha256 digest), saves them in its data volume, and restarts into the
// new hub. LANHub Media picks up its new program from the shared volume the
// next time it's idle.
//
// If a new hub fails to start three times, the program built into the image
// takes over again and the update is marked as failed. Pulling a newer image
// makes any older staged program redundant; it's removed on start.

const (
	defaultUpdateImage = "ghcr.io/ac3labs/lanhub"
	defaultNotesURL    = "https://raw.githubusercontent.com/AC3Labs/LANHub/{rev}/CHANGELOG.md"
	hubBinaryPath      = "lanhub-hub"                 // path inside the hub image
	mediaBinaryPath    = "usr/local/bin/lanhub-media" // path inside the media image
	maxBinarySize      = 200 << 20
	updateCheckEvery   = 6 * time.Hour
	stagedFailAfter    = 3
)

// ---- versions ----

// compareVersions compares dotted numeric versions ("0.12.0.1"). Anything
// that isn't a number (like "dev") counts as 0.
func compareVersions(a, b string) int {
	pa, pb := strings.Split(strings.TrimPrefix(a, "v"), "."), strings.Split(strings.TrimPrefix(b, "v"), ".")
	for i := 0; i < max(len(pa), len(pb)); i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// ---- registry client (OCI distribution API) ----

type registryClient struct {
	base, repo string
	http       *http.Client
	token      string
}

// newRegistryClient takes an image reference like ghcr.io/ac3labs/lanhub, or
// http://127.0.0.1:5000/lanhub for a plain-HTTP test registry.
func newRegistryClient(ref string) *registryClient {
	scheme := "https://"
	if s, rest, ok := strings.Cut(ref, "://"); ok {
		scheme, ref = s+"://", rest
	}
	host, repo, _ := strings.Cut(ref, "/")
	return &registryClient{base: scheme + host, repo: repo, http: &http.Client{Timeout: 10 * time.Minute}}
}

var challengeParam = regexp.MustCompile(`(\w+)="([^"]*)"`)

// get fetches a registry path, answering a bearer-token challenge once with
// an anonymous token (public images need nothing more).
func (c *registryClient) get(ctx context.Context, path, accept string) (*http.Response, error) {
	for attempt := 0; attempt < 2; attempt++ {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
		if accept != "" {
			req.Header.Set("Accept", accept)
		}
		if c.token != "" {
			req.Header.Set("Authorization", "Bearer "+c.token)
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			ch := resp.Header.Get("WWW-Authenticate")
			resp.Body.Close()
			if err := c.fetchToken(ctx, ch); err != nil {
				return nil, err
			}
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("the registry answered %s for %s", resp.Status, path)
		}
		return resp, nil
	}
	return nil, errors.New("the registry refused access")
}

func (c *registryClient) fetchToken(ctx context.Context, challenge string) error {
	if !strings.HasPrefix(strings.ToLower(challenge), "bearer ") {
		return fmt.Errorf("the registry wants a login this hub doesn't have (%s)", challenge)
	}
	p := map[string]string{}
	for _, m := range challengeParam.FindAllStringSubmatch(challenge, -1) {
		p[m[1]] = m[2]
	}
	if p["realm"] == "" {
		return errors.New("the registry's login challenge has no realm")
	}
	q := url.Values{}
	if p["service"] != "" {
		q.Set("service", p["service"])
	}
	if p["scope"] != "" {
		q.Set("scope", p["scope"])
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, p["realm"]+"?"+q.Encode(), nil)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var t struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&t) != nil {
		return fmt.Errorf("couldn't get a registry token (%s)", resp.Status)
	}
	c.token = t.Token
	if c.token == "" {
		c.token = t.AccessToken
	}
	return nil
}

// readVerified reads a small document and checks it against its digest
// (when one is known).
func (c *registryClient) readVerified(ctx context.Context, path, accept, digest string) ([]byte, string, error) {
	resp, err := c.get(ctx, path, accept)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, "", err
	}
	if digest != "" && !digestMatches(digest, b) {
		return nil, "", fmt.Errorf("%s doesn't match its checksum", path)
	}
	return b, resp.Header.Get("Content-Type"), nil
}

func digestMatches(digest string, b []byte) bool {
	algo, want, ok := strings.Cut(digest, ":")
	if !ok || algo != "sha256" {
		return false
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]) == want
}

type imageLayer struct {
	Digest    string `json:"digest"`
	MediaType string `json:"mediaType"`
	Size      int64  `json:"size"`
}

type imageInfo struct {
	Tag, Version, Revision string
	Layers                 []imageLayer
}

const manifestAccept = "application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, " +
	"application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json"

// image reads a tag's manifest (choosing linux/amd64 from a multi-platform
// index) and the version labels from its config.
func (c *registryClient) image(ctx context.Context, tag string) (*imageInfo, error) {
	type manifest struct {
		Manifests []struct {
			Digest   string `json:"digest"`
			Platform struct {
				OS           string `json:"os"`
				Architecture string `json:"architecture"`
			} `json:"platform"`
		} `json:"manifests"`
		Config imageLayer   `json:"config"`
		Layers []imageLayer `json:"layers"`
	}
	b, _, err := c.readVerified(ctx, "/v2/"+c.repo+"/manifests/"+tag, manifestAccept, "")
	if err != nil {
		return nil, err
	}
	var m manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("unreadable manifest for %s: %w", tag, err)
	}
	if len(m.Manifests) > 0 {
		digest := ""
		for _, e := range m.Manifests {
			if e.Platform.OS == "linux" && e.Platform.Architecture == "amd64" {
				digest = e.Digest
				break
			}
		}
		if digest == "" {
			return nil, fmt.Errorf("%s has no linux/amd64 image", tag)
		}
		if b, _, err = c.readVerified(ctx, "/v2/"+c.repo+"/manifests/"+digest, manifestAccept, digest); err != nil {
			return nil, err
		}
		m = manifest{}
		if err := json.Unmarshal(b, &m); err != nil {
			return nil, err
		}
	}
	if m.Config.Digest == "" || len(m.Layers) == 0 {
		return nil, fmt.Errorf("%s's manifest has no layers", tag)
	}
	cb, _, err := c.readVerified(ctx, "/v2/"+c.repo+"/blobs/"+m.Config.Digest, "", m.Config.Digest)
	if err != nil {
		return nil, err
	}
	var cfg struct {
		Config struct {
			Labels map[string]string `json:"Labels"`
		} `json:"config"`
	}
	json.Unmarshal(cb, &cfg)
	return &imageInfo{Tag: tag, Layers: m.Layers,
		Version:  cfg.Config.Labels["org.opencontainers.image.version"],
		Revision: cfg.Config.Labels["org.opencontainers.image.revision"]}, nil
}

// extractFile finds one file in an image (newest layer first), writes it to
// dst and returns its sha256. Each layer read is checked against its digest
// before the file is accepted.
func (c *registryClient) extractFile(ctx context.Context, img *imageInfo, want, dst string, progress func(int64)) (string, error) {
	for i := len(img.Layers) - 1; i >= 0; i-- {
		l := img.Layers[i]
		if !strings.HasSuffix(l.MediaType, "gzip") {
			continue
		}
		sum, found, err := c.scanLayer(ctx, l, want, dst, progress)
		if err != nil {
			os.Remove(dst)
			return "", err
		}
		if found {
			return sum, nil
		}
	}
	return "", fmt.Errorf("%s isn't in the %s image", want, img.Tag)
}

func (c *registryClient) scanLayer(ctx context.Context, l imageLayer, want, dst string, progress func(int64)) (string, bool, error) {
	resp, err := c.get(ctx, "/v2/"+c.repo+"/blobs/"+l.Digest, "")
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()
	blobHash := sha256.New()
	body := io.TeeReader(&progressReader{r: io.LimitReader(resp.Body, 2<<30), fn: progress}, blobHash)
	gz, err := gzip.NewReader(body)
	if err != nil {
		return "", false, fmt.Errorf("layer %s: %w", l.Digest[:19], err)
	}
	tr := tar.NewReader(gz)
	fileSum, found := "", false
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", false, fmt.Errorf("layer %s: %w", l.Digest[:19], err)
		}
		name := strings.TrimPrefix(strings.TrimPrefix(h.Name, "./"), "/")
		if found || name != want || h.Typeflag != tar.TypeReg {
			continue
		}
		if h.Size > maxBinarySize {
			return "", false, fmt.Errorf("%s is unexpectedly large (%d bytes)", want, h.Size)
		}
		f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
		if err != nil {
			return "", false, err
		}
		fh := sha256.New()
		_, err = io.Copy(io.MultiWriter(f, fh), io.LimitReader(tr, maxBinarySize))
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return "", false, err
		}
		fileSum, found = hex.EncodeToString(fh.Sum(nil)), true
	}
	// Read to the end so the whole layer's digest can be checked.
	if _, err := io.Copy(io.Discard, body); err != nil {
		return "", false, err
	}
	if "sha256:"+hex.EncodeToString(blobHash.Sum(nil)) != l.Digest {
		return "", false, fmt.Errorf("a downloaded layer doesn't match its checksum (%s)", l.Digest[:19])
	}
	return fileSum, found, nil
}

type progressReader struct {
	r  io.Reader
	fn func(int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 && p.fn != nil {
		p.fn(int64(n))
	}
	return n, err
}

// ---- staged programs ----

// stagedManifest sits next to a staged program and says what it is.
type stagedManifest struct {
	Version      string    `json:"version"`
	Revision     string    `json:"revision,omitempty"`
	SHA256       string    `json:"sha256"`
	StagedAt     time.Time `json:"staged_at"`
	Attempts     int       `json:"attempts"`
	Failed       bool      `json:"failed,omitempty"`
	FailedReason string    `json:"failed_reason,omitempty"`
}

func readStaged(dir string) *stagedManifest {
	b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return nil
	}
	var m stagedManifest
	if json.Unmarshal(b, &m) != nil || m.Version == "" {
		return nil
	}
	return &m
}

func writeStaged(dir string, m *stagedManifest) error {
	b, _ := json.MarshalIndent(m, "", "  ")
	tmp := filepath.Join(dir, "manifest.json.tmp")
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, "manifest.json"))
}

func fileSHA256(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil))
}

func hubUpdateDir(dbPath string) string { return filepath.Join(filepath.Dir(dbPath), "update", "hub") }

// stagedHubToRun decides, at start-up, whether to hand over to a staged hub.
// It returns the program to exec, or "" to carry on with this one.
func stagedHubToRun(dir, running string) string {
	if os.Getenv("LANHUB_STAGED") == "1" {
		return "" // already the staged program
	}
	m := readStaged(dir)
	if m == nil {
		return ""
	}
	bin := filepath.Join(dir, "lanhub-hub")
	switch {
	case compareVersions(m.Version, running) <= 0:
		log.Printf("update: the image's %s is the same as or newer than the staged %s; removing the staged copy", running, m.Version)
		os.RemoveAll(dir)
		return ""
	case m.Failed:
		return ""
	case m.Attempts >= stagedFailAfter:
		m.Failed, m.FailedReason = true, fmt.Sprintf("it didn't start properly after %d tries, so LANHub went back to %s", m.Attempts, running)
		writeStaged(dir, m)
		log.Printf("update: %s %s", m.Version, m.FailedReason)
		return ""
	case fileSHA256(bin) != m.SHA256:
		m.Failed, m.FailedReason = true, "the staged program doesn't match its checksum"
		writeStaged(dir, m)
		log.Printf("update: %s %s", m.Version, m.FailedReason)
		return ""
	}
	m.Attempts++
	writeStaged(dir, m)
	log.Printf("update: starting the installed update %s (try %d of %d)", m.Version, m.Attempts, stagedFailAfter)
	return bin
}

// execStaged replaces this process with a staged hub. It only returns on
// failure.
func execStaged(bin string) error {
	env := []string{"LANHUB_STAGED=1", "LANHUB_IMAGE_VERSION=" + imageVersion()}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "LANHUB_STAGED=") && !strings.HasPrefix(kv, "LANHUB_IMAGE_VERSION=") {
			env = append(env, kv)
		}
	}
	return syscall.Exec(bin, append([]string{bin}, os.Args[1:]...), env)
}

// imageVersion is the version of the program built into the image (which
// may differ from hubVersion when a staged update is running).
func imageVersion() string {
	if v := os.Getenv("LANHUB_IMAGE_VERSION"); v != "" {
		return v
	}
	return hubVersion
}

// markHealthy resets the start counter once a staged hub has been up for a
// minute, and only then records the update as done.
func (u *updater) markHealthy(ctx context.Context) {
	if os.Getenv("LANHUB_STAGED") != "1" {
		return
	}
	select {
	case <-ctx.Done():
		return
	case <-time.After(time.Minute):
	}
	if m := readStaged(u.hubDir); m != nil && m.Version == hubVersion && m.Attempts != 0 {
		m.Attempts = 0
		writeStaged(u.hubDir, m)
		log.Printf("update: %s is running fine", hubVersion)
		s := u.app.store.Setting
		u.app.setSetting("update_last_result", fmt.Sprintf("Updated from %s to %s on %s.%s",
			s("update_from"), hubVersion, m.StagedAt.Format("2006-01-02 15:04 UTC"), s("update_media_note")))
	}
}

// ---- the updater ----

type updater struct {
	app       *App
	image     string
	notesURL  string
	hubDir    string
	mediaDir  string // "" when LANHub Media's program can't be updated from here
	restartTo chan string

	mu     sync.Mutex
	phase  string // "", "downloading", "restarting", "failed"
	detail string
	target string
	bytes  int64
}

func newUpdater(app *App) *updater {
	u := &updater{app: app, image: getenv("LANHUB_UPDATE_IMAGE", defaultUpdateImage),
		notesURL: getenv("LANHUB_UPDATE_NOTES", defaultNotesURL), hubDir: hubUpdateDir(app.cfg.DBPath),
		restartTo: make(chan string, 1)}
	if app.cfg.MediaSecretFile != "" && app.mediaEnabled() {
		u.mediaDir = filepath.Join(filepath.Dir(app.cfg.MediaSecretFile), "update")
	}
	return u
}

type updateStatus struct {
	Phase   string `json:"phase"`
	Detail  string `json:"detail"`
	Target  string `json:"target"`
	Bytes   int64  `json:"bytes"`
	Version string `json:"version"`
}

func (u *updater) status() updateStatus {
	u.mu.Lock()
	defer u.mu.Unlock()
	return updateStatus{Phase: u.phase, Detail: u.detail, Target: u.target, Bytes: u.bytes, Version: hubVersion}
}

func (u *updater) set(phase, detail string) {
	u.mu.Lock()
	u.phase, u.detail = phase, detail
	u.mu.Unlock()
	if phase == "failed" {
		log.Printf("update: failed: %s", detail)
	}
}

// latest is the newest version found by the last check, if it's newer than
// this hub.
func (u *updater) latest() string {
	v := u.app.store.Setting("update_latest")
	if compareVersions(v, hubVersion) > 0 {
		return v
	}
	return ""
}

// check asks the registry what's published. With notify, admins are emailed
// (once per version) when it's newer.
func (u *updater) check(ctx context.Context, notify bool) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	set := u.app.setSetting
	set("update_checked_at", nowDB())
	c := newRegistryClient(u.image)
	img, err := c.image(ctx, "hub")
	if err == nil && img.Version == "" {
		err = errors.New("the published image doesn't say which version it is")
	}
	if err != nil {
		set("update_check_error", err.Error())
		return "", err
	}
	set("update_check_error", "")
	set("update_latest", img.Version)
	set("update_latest_revision", img.Revision)
	if compareVersions(img.Version, hubVersion) > 0 {
		set("update_notes", u.releaseNotes(ctx, img.Revision, img.Version))
		if notify && u.app.store.Setting("update_notified") != img.Version {
			u.notify(img.Version)
		}
	} else {
		set("update_notes", "")
	}
	return img.Version, nil
}

// releaseNotes pulls the CHANGELOG sections between this version and the
// new one from the release's own commit.
func (u *updater) releaseNotes(ctx context.Context, rev, latest string) string {
	if rev == "" || u.notesURL == "" || u.notesURL == "off" {
		return ""
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, strings.ReplaceAll(u.notesURL, "{rev}", rev), nil)
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	return changelogBetween(string(b), hubVersion, latest)
}

var changelogHeading = regexp.MustCompile(`(?m)^## \[([0-9.]+)\][^\n]*$`)

// changelogBetween returns the sections for versions newer than from, up to
// and including to.
func changelogBetween(text, from, to string) string {
	locs := changelogHeading.FindAllStringSubmatchIndex(text, -1)
	var out []string
	for i, loc := range locs {
		v := text[loc[2]:loc[3]]
		end := len(text)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		if compareVersions(v, from) > 0 && compareVersions(v, to) <= 0 {
			out = append(out, strings.TrimSpace(text[loc[0]:end]))
		}
	}
	s := strings.Join(out, "\n\n")
	if len(s) > 12000 {
		s = s[:12000] + "\n…"
	}
	return s
}

// notify emails every active admin, and pushes to ntfy when it's set up.
func (u *updater) notify(version string) {
	u.app.setSetting("update_notified", version)
	u.app.notify(fmt.Sprintf("LANHub %s is available (this hub runs %s). Update it from Settings → Updates.", version, hubVersion))
	if u.app.store.Setting("update_email") == "0" || !u.app.mailer.Configured() {
		return
	}
	link := ""
	if base := u.app.emailBaseURL(); base != "" {
		link = base + "/settings?tab=updates&install=" + url.QueryEscape(version)
	}
	users, _ := u.app.store.Users()
	for _, usr := range users {
		if !usr.IsAdmin || usr.IsPending() {
			continue
		}
		text, html := updateEmail(usr.Name, version, hubVersion, link, u.app.store.Setting("update_notes"))
		if err := u.app.mailer.SendHTML(usr.Email, "LANHub "+version+" is available", text, html); err != nil {
			log.Printf("update: couldn't email %s: %v", usr.Email, err)
		}
	}
}

func (u *updater) autoCheck(ctx context.Context) {
	wait := 2 * time.Minute
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = updateCheckEvery
		if u.app.store.Setting("update_auto_check") == "0" {
			continue
		}
		if v, err := u.check(ctx, true); err != nil {
			log.Printf("update: automatic check failed: %v", err)
		} else if compareVersions(v, hubVersion) > 0 {
			log.Printf("update: %s is available (running %s)", v, hubVersion)
		}
	}
}

// install downloads and stages target, then asks main to restart into it.
// It runs in the background; status() reports progress.
func (u *updater) install(target string) error {
	u.mu.Lock()
	if u.phase == "downloading" || u.phase == "restarting" {
		u.mu.Unlock()
		return errors.New("an update is already being installed")
	}
	u.phase, u.detail, u.target, u.bytes = "downloading", "Downloading the new hub…", target, 0
	u.mu.Unlock()
	go func() {
		if err := u.doInstall(target); err != nil {
			u.set("failed", err.Error())
			u.app.setSetting("update_last_result", "Update to "+target+" failed: "+err.Error())
		}
	}()
	return nil
}

func (u *updater) doInstall(target string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	c := newRegistryClient(u.image)
	progress := func(n int64) { u.mu.Lock(); u.bytes += n; u.mu.Unlock() }

	img, err := c.image(ctx, "hub")
	if err != nil {
		return err
	}
	if img.Version != target {
		return fmt.Errorf("the published version is now %s, not %s; check for updates again", img.Version, target)
	}
	if err := os.MkdirAll(u.hubDir, 0o755); err != nil {
		return fmt.Errorf("can't save the update: %w", err)
	}
	newHub := filepath.Join(u.hubDir, "lanhub-hub.new")
	hubSum, err := c.extractFile(ctx, img, hubBinaryPath, newHub, progress)
	if err != nil {
		return err
	}
	if err := checkBinaryVersion(newHub, target); err != nil {
		os.Remove(newHub)
		return err
	}

	mediaNote := ""
	if u.mediaDir != "" {
		u.set("downloading", "Downloading the new LANHub Media…")
		if err := u.stageMedia(ctx, c, target, progress); err != nil {
			// The hub update still goes ahead; Media keeps its current version.
			mediaNote = " LANHub Media wasn't updated: " + err.Error()
			log.Printf("update: %s", strings.TrimSpace(mediaNote))
		}
	}

	if err := os.Rename(newHub, filepath.Join(u.hubDir, "lanhub-hub")); err != nil {
		return err
	}
	// Attempts starts at 1: this hub execs the new one directly.
	if err := writeStaged(u.hubDir, &stagedManifest{Version: target, Revision: img.Revision, SHA256: hubSum, StagedAt: time.Now().UTC(), Attempts: 1}); err != nil {
		return err
	}
	// Recorded as done by the new hub once it has run for a minute.
	u.app.setSetting("update_last_result", "")
	u.app.setSetting("update_from", hubVersion)
	u.app.setSetting("update_media_note", mediaNote)
	u.set("restarting", "Restarting into "+target+"…")
	log.Printf("update: %s staged; restarting into it", target)
	time.Sleep(1500 * time.Millisecond) // let the status poll see "restarting"
	u.restartTo <- filepath.Join(u.hubDir, "lanhub-hub")
	return nil
}

func (u *updater) stageMedia(ctx context.Context, c *registryClient, target string, progress func(int64)) error {
	img, err := c.image(ctx, "media")
	if err != nil {
		return err
	}
	if img.Version != target {
		return fmt.Errorf("its image is %q, not %s", img.Version, target)
	}
	if err := os.MkdirAll(u.mediaDir, 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(u.mediaDir, "lanhub-media.new")
	sum, err := c.extractFile(ctx, img, mediaBinaryPath, tmp, progress)
	if err != nil {
		return err
	}
	if err := checkBinaryVersion(tmp, target); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, filepath.Join(u.mediaDir, "lanhub-media")); err != nil {
		return err
	}
	return writeStaged(u.mediaDir, &stagedManifest{Version: target, Revision: img.Revision, SHA256: sum, StagedAt: time.Now().UTC()})
}

// checkBinaryVersion runs a downloaded program with --version: proof it
// runs here at all, and is the version it claims.
func checkBinaryVersion(bin, want string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "--version").Output()
	got := strings.TrimSpace(string(out))
	if err != nil {
		return fmt.Errorf("the downloaded %s won't run: %v", filepath.Base(bin), err)
	}
	if got != want {
		return fmt.Errorf("the downloaded %s says it's %q, expected %s", filepath.Base(bin), got, want)
	}
	return nil
}

// watchMedia follows LANHub Media after an update was staged for it. If it
// stops answering for three minutes, the staged program is marked failed so
// Media's next restart uses the one in its image.
func (u *updater) watchMedia(ctx context.Context) {
	if u.mediaDir == "" {
		return
	}
	m := readStaged(u.mediaDir)
	if m == nil || m.Failed || time.Since(m.StagedAt) > 30*time.Minute {
		return
	}
	var downSince time.Time
	for i := 0; i < 80; i++ {
		select {
		case <-ctx.Done():
			return
		case <-time.After(15 * time.Second):
		}
		v, err := u.app.mediaVersion(ctx)
		switch {
		case err == nil && v == m.Version:
			log.Printf("update: LANHub Media is running %s", v)
			return
		case err == nil:
			downSince = time.Time{} // still on the old version, probably busy
		case downSince.IsZero():
			downSince = time.Now()
		case time.Since(downSince) > 3*time.Minute:
			m.Failed, m.FailedReason = true, "LANHub Media stopped answering after switching to it, so it went back to the version in its image"
			writeStaged(u.mediaDir, m)
			log.Printf("update: media %s %s", m.Version, m.FailedReason)
			return
		}
	}
}

// mediaVersion asks LANHub Media which version it runs.
func (app *App) mediaVersion(ctx context.Context) (string, error) {
	if app.media.url == "" {
		return "", errors.New("LANHub Media isn't set up")
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(app.media.url, "/")+"/healthz", nil)
	resp, err := mediaHTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var h struct {
		Version string `json:"version"`
	}
	json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&h)
	return h.Version, nil
}

// emailBaseURL is the address links in emails use.
func (app *App) emailBaseURL() string {
	if app.cfg.AppURL != "" && !strings.Contains(app.cfg.AppURL, "localhost") {
		return app.cfg.AppURL
	}
	return strings.TrimRight(app.store.Setting("public_url"), "/")
}

func updateEmail(name, version, current, link, notes string) (string, string) {
	var t strings.Builder
	fmt.Fprintf(&t, "Hi %s,\n\nLANHub %s is available. This hub runs %s.\n\n", name, version, current)
	if link != "" {
		fmt.Fprintf(&t, "Update LANHub: %s\n\n", link)
	} else {
		t.WriteString("Update it from Settings → Updates in LANHub.\n\n")
	}
	if notes != "" {
		t.WriteString("What's new:\n\n" + notes + "\n\n")
	}
	t.WriteString("You get this email because you're an admin. Turn it off under Settings → Updates.\n")

	esc := func(s string) string {
		r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
		return r.Replace(s)
	}
	var h bytes.Buffer
	h.WriteString(`<!DOCTYPE html><html><body style="margin:0;padding:24px;background:#f5f5f4;font-family:-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;color:#292524">`)
	h.WriteString(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0"><tr><td align="center">`)
	h.WriteString(`<table role="presentation" width="560" cellpadding="0" cellspacing="0" style="max-width:560px;background:#ffffff;border:1px solid #e7e5e4;border-radius:10px">`)
	fmt.Fprintf(&h, `<tr><td style="padding:28px 32px 8px"><p style="margin:0 0 4px;font-size:13px;color:#78716c">LANHub</p><h1 style="margin:0;font-size:22px">LANHub %s is available</h1></td></tr>`, esc(version))
	fmt.Fprintf(&h, `<tr><td style="padding:12px 32px;font-size:15px;line-height:1.5">Hi %s, a new version of LANHub is ready. This hub runs <b>%s</b>.</td></tr>`, esc(name), esc(current))
	if link != "" {
		fmt.Fprintf(&h, `<tr><td style="padding:12px 32px 20px"><a href="%s" style="display:inline-block;background:#b08d57;color:#000000;text-decoration:none;font-weight:600;font-size:15px;padding:12px 22px;border-radius:8px">Update LANHub</a></td></tr>`, esc(link))
	} else {
		h.WriteString(`<tr><td style="padding:12px 32px 20px;font-size:15px">Update it from <b>Settings → Updates</b> in LANHub.</td></tr>`)
	}
	if notes != "" {
		fmt.Fprintf(&h, `<tr><td style="padding:0 32px 20px"><p style="margin:0 0 6px;font-weight:600">What's new</p><pre style="margin:0;white-space:pre-wrap;font-family:ui-monospace,Menlo,Consolas,monospace;font-size:12px;line-height:1.5;background:#fafaf9;border:1px solid #e7e5e4;border-radius:6px;padding:12px">%s</pre></td></tr>`, esc(notes))
	}
	h.WriteString(`<tr><td style="padding:0 32px 28px;font-size:12px;color:#78716c">You get this email because you're a LANHub admin. Turn it off under Settings → Updates.</td></tr>`)
	h.WriteString(`</table></td></tr></table></body></html>`)
	return t.String(), h.String()
}
