package main

import (
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"
)

// The message the release key signs; must match updateMessage in the
// agent's selfupdate.go and UpdateMessage in cmd/agentbundle.
func signedMessage(version, goos, goarch, sha string) []byte {
	return []byte("lanhub-agent-update\n" + version + "\n" + goos + "/" + goarch + "\n" + sha + "\n")
}

// testBundle is a hub's agentbuilds folder with one Linux build, signed with
// key (or unsigned when key is nil).
func testBundle(t *testing.T, version string, program []byte, key ed25519.PrivateKey) *agentBundle {
	t.Helper()
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	zw.Write(program)
	zw.Close()
	sum := sha256.Sum256(program)
	b := agentBuild{OS: "linux", Arch: "amd64", Name: "lanhub-agent-linux-amd64", Size: int64(len(program)), SHA256: hex.EncodeToString(sum[:])}
	if key != nil {
		b.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, signedMessage(version, "linux", "amd64", b.SHA256)))
	}
	man, _ := json.Marshal(map[string]any{"version": version, "builds": []agentBuild{b}})
	bundle := loadAgentBundle(fstest.MapFS{
		"agentbuilds/manifest.json":               {Data: man},
		"agentbuilds/lanhub-agent-linux-amd64.gz": {Data: gz.Bytes()},
		"agentbuilds/README.md":                   {Data: []byte("x")},
	})
	if bundle == nil {
		t.Fatal("bundle didn't load")
	}
	return bundle
}

func TestHubWithoutAgentBuildsStillRuns(t *testing.T) {
	if loadAgentBundle(fstest.MapFS{"agentbuilds/README.md": {Data: []byte("x")}}) != nil {
		t.Fatal("a folder without a manifest is no bundle")
	}
	h := newTestHub(t)
	h.app.agentBuilds = nil
	c := h.client()
	if resp, _ := c.get("/agent/install.sh"); resp.StatusCode != 404 {
		t.Fatalf("install.sh without builds: %d", resp.StatusCode)
	}
}

func TestAgentInstallScriptsAndDownloads(t *testing.T) {
	h := newTestHub(t)
	program := []byte("agent program bytes")
	h.app.agentBuilds = testBundle(t, "1.4.0", program, nil)
	c := h.client()

	resp, script := c.get("/agent/install.sh")
	sum := sha256.Sum256(program)
	if resp.StatusCode != 200 || !strings.Contains(script, `HUB="`+h.srv.URL+`"`) || !strings.Contains(script, `VERSION="1.4.0"`) ||
		!strings.Contains(script, hex.EncodeToString(sum[:])) || strings.Contains(script, "{{%") {
		t.Fatalf("install.sh: %d\n%s", resp.StatusCode, script)
	}
	resp, ps := c.get("/agent/install.ps1")
	if resp.StatusCode != 200 || !strings.Contains(ps, `$Hub = "`+h.srv.URL+`"`) || !strings.Contains(ps, `$Sum = ""`) {
		t.Fatalf("install.ps1 (no Windows build): %d\n%s", resp.StatusCode, ps)
	}

	resp, body := c.get("/agent/download/lanhub-agent-linux-amd64")
	if resp.StatusCode != 200 || body != string(program) || resp.Header.Get("X-LANHub-SHA256") != hex.EncodeToString(sum[:]) {
		t.Fatalf("download: %d %q", resp.StatusCode, body)
	}
	if resp, _ := c.get("/agent/download/..%2fmanifest.json"); resp.StatusCode != 404 {
		t.Fatalf("download of another file: %d", resp.StatusCode)
	}

	// A Host header that could break out of the script's quotes is refused.
	req, _ := http.NewRequest(http.MethodGet, h.srv.URL+"/agent/install.sh", nil)
	req.Host = `evil"; rm -rf /; echo "`
	r2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r2.Body.Close()
	if r2.StatusCode != 400 {
		t.Fatalf("odd Host: %d", r2.StatusCode)
	}
}

// updatableAgent is an agent that can update itself: it checks what the hub
// sends like the real one does, then comes back as the new version.
type updatableAgent struct {
	mu       sync.Mutex
	version  string
	features []string
	pub      ed25519.PublicKey
	got      []byte
	srv      *httptest.Server
}

func newUpdatableAgent(t *testing.T, name, version string, pub ed25519.PublicKey, features ...string) *updatableAgent {
	a := &updatableAgent{version: version, features: features, pub: pub}
	a.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok-"+name {
			w.WriteHeader(401)
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		switch r.URL.Path {
		case "/api/health":
			json.NewEncoder(w).Encode(map[string]any{"hostname": name, "os": "linux", "arch": "amd64", "version": a.version, "features": a.features})
		case "/api/self-update":
			body, _ := io.ReadAll(r.Body)
			sig, _ := base64.StdEncoding.DecodeString(r.Header.Get("X-LANHub-Signature"))
			sum := sha256.Sum256(body)
			v := r.Header.Get("X-LANHub-Version")
			if hex.EncodeToString(sum[:]) != r.Header.Get("X-LANHub-SHA256") || !ed25519.Verify(a.pub, signedMessage(v, "linux", "amd64", hex.EncodeToString(sum[:])), sig) {
				w.WriteHeader(403)
				json.NewEncoder(w).Encode(map[string]string{"message": "That agent isn't signed by LANHub's release key, so it wasn't installed."})
				return
			}
			a.got = body
			json.NewEncoder(w).Encode(map[string]any{"ok": true})
			// "Restarts" as the new version a moment later.
			go func() {
				time.Sleep(50 * time.Millisecond)
				a.mu.Lock()
				a.version = v
				a.mu.Unlock()
			}()
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(a.srv.Close)
	return a
}

func TestAgentUpdateFromTheHub(t *testing.T) {
	old := agentUpdatePoll
	agentUpdatePoll = 20 * time.Millisecond
	defer func() { agentUpdatePoll = old }()

	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	program := []byte("the new agent")
	h := newTestHub(t)
	h.app.agentBuilds = testBundle(t, "1.4.0", program, priv)
	adminID := h.user("admin@example.com", true)
	memberID := h.user("member@example.com", false)

	current := newUpdatableAgent(t, "current", "1.4.0", pub, "range", "fetch", "self-update")
	behind := newUpdatableAgent(t, "behind", "1.3.9", pub, "range", "fetch", "self-update")
	legacy := newUpdatableAgent(t, "legacy", "1.2.0", pub, "range")
	idCurrent := h.machine("current", current.srv, "linux")
	idBehind := h.machine("behind", behind.srv, "linux")
	idLegacy := h.machine("legacy", legacy.srv, "linux")
	for _, id := range []int64{idCurrent, idBehind, idLegacy} {
		h.grant(adminID, id, "full")
	}
	h.grant(memberID, idBehind, "full")

	admin := h.client()
	admin.login("admin@example.com")
	status := func(id int64) map[string]any {
		t.Helper()
		resp, err := admin.http.Get(h.srv.URL + "/api/agents/" + itoa(id) + "/status")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		json.NewDecoder(resp.Body).Decode(&out)
		return out
	}
	// The pages offer it: install commands, and update buttons for admins.
	if _, page := admin.get("/agents"); !strings.Contains(page, "Install an agent") || !strings.Contains(page, "update-all-agents") || !strings.Contains(page, "Update agent") {
		t.Fatal("agents page lacks the install and update controls")
	}
	if _, page := admin.get("/dashboard"); !strings.Contains(page, "/agent/install.sh") {
		t.Fatal("Add Machine lacks the install commands")
	}
	if s := status(idCurrent); s["update_to"] != nil {
		t.Fatalf("current agent offered an update: %v", s)
	}
	if s := status(idBehind); s["update_to"] != "1.4.0" || s["can_push"] != true {
		t.Fatalf("behind agent: %v", s)
	}
	if s := status(idLegacy); s["update_to"] != "1.4.0" || s["can_push"] != false || !strings.Contains(s["update_note"].(string), "by hand") {
		t.Fatalf("legacy agent: %v", s)
	}

	// Members can't update agents.
	member := h.client()
	member.login("member@example.com")
	if _, page := member.get("/agents"); strings.Contains(page, "Update agent") || !strings.Contains(page, "Install an agent") {
		t.Fatal("members see install commands but no update buttons")
	}
	if resp, _ := member.postJSON("/api/agents/"+itoa(idBehind)+"/update", nil); resp.StatusCode != 403 {
		t.Fatalf("member update: %d", resp.StatusCode)
	}

	// An agent older than 1.4.0 can't take it from here.
	if resp, out := admin.postJSON("/api/agents/"+itoa(idLegacy)+"/update", nil); resp.StatusCode != 422 || !strings.Contains(out["message"].(string), "by hand") {
		t.Fatalf("legacy update: %d %v", resp.StatusCode, out)
	}
	// The real thing: the signed build arrives and the agent comes back new.
	resp, out := admin.postJSON("/api/agents/"+itoa(idBehind)+"/update", nil)
	if resp.StatusCode != 200 || out["version"] != "1.4.0" {
		t.Fatalf("update: %d %v", resp.StatusCode, out)
	}
	if !bytes.Equal(behind.got, program) {
		t.Fatalf("agent got %q", behind.got)
	}
	if s := status(idBehind); s["update_to"] != nil {
		t.Fatalf("after the update: %v", s)
	}
	if resp, out := admin.postJSON("/api/agents/"+itoa(idCurrent)+"/update", nil); resp.StatusCode != 200 || !strings.Contains(out["message"].(string), "already up to date") {
		t.Fatalf("current update: %d %v", resp.StatusCode, out)
	}

	// A hub whose builds aren't signed says so instead of pushing.
	h.app.agentBuilds = testBundle(t, "1.5.0", program, nil)
	if resp, out := admin.postJSON("/api/agents/"+itoa(idBehind)+"/update", nil); resp.StatusCode != 422 || !strings.Contains(out["message"].(string), "aren't signed") {
		t.Fatalf("unsigned update: %d %v", resp.StatusCode, out)
	}
	// Signed by another key: the agent refuses, and the hub reports it.
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	h.app.agentBuilds = testBundle(t, "1.5.0", program, other)
	if resp, out := admin.postJSON("/api/agents/"+itoa(idBehind)+"/update", nil); resp.StatusCode != 422 || !strings.Contains(out["message"].(string), "release key") {
		t.Fatalf("foreign key update: %d %v", resp.StatusCode, out)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
