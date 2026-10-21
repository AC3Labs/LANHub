package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeAgent is a "program" that prints a version, like the real one does
// for `lanhub-agent version`.
func fakeAgent(version string) []byte {
	return []byte("#!/bin/sh\necho " + version + "\n")
}

func TestSelfUpdate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in program is a shell script")
	}
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	oldKey := updatePublicKey
	updatePublicKey = base64.StdEncoding.EncodeToString(pub)
	defer func() { updatePublicKey = oldKey }()

	dir := t.TempDir()
	exe := filepath.Join(dir, "lanhub-agent")
	os.WriteFile(exe, fakeAgent(agentVersion), 0o755)
	agentExecutable = func() (string, error) { return exe, nil }
	restarted := make(chan bool, 1)
	restartAgent = func() { restarted <- true }

	send := func(body []byte, version, sum string, key ed25519.PrivateKey) *httptest.ResponseRecorder {
		if sum == "" {
			s := sha256.Sum256(body)
			sum = hex.EncodeToString(s[:])
		}
		req := httptest.NewRequest(http.MethodPost, "/api/self-update", bytes.NewReader(body))
		req.Header.Set("X-LANHub-SHA256", sum)
		req.Header.Set("X-LANHub-Version", version)
		req.Header.Set("X-LANHub-Signature", base64.StdEncoding.EncodeToString(ed25519.Sign(key, updateMessage(version, runtime.GOOS, runtime.GOARCH, sum))))
		rec := httptest.NewRecorder()
		handleSelfUpdate(rec, req)
		return rec
	}
	unchanged := func(why string) {
		t.Helper()
		if got, _ := os.ReadFile(exe); !bytes.Equal(got, fakeAgent(agentVersion)) {
			t.Fatalf("%s: the agent was replaced", why)
		}
		if left, _ := filepath.Glob(filepath.Join(dir, ".lanhub-agent-new-*")); len(left) > 0 {
			t.Fatalf("%s: left %v behind", why, left)
		}
	}

	next := "9.0.0"
	// Signed by someone else's key: refused.
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	if rec := send(fakeAgent(next), next, "", other); rec.Code != 403 {
		t.Fatalf("foreign key: %d %s", rec.Code, rec.Body)
	}
	unchanged("foreign key")
	// Signed, but the body isn't what was signed.
	good := sha256.Sum256(fakeAgent(next))
	if rec := send([]byte("something else"), next, hex.EncodeToString(good[:]), priv); rec.Code != 400 {
		t.Fatalf("damaged: %d %s", rec.Code, rec.Body)
	}
	unchanged("damaged")
	// Not newer: refused, so an old signed build can't be pushed back.
	if rec := send(fakeAgent(agentVersion), agentVersion, "", priv); rec.Code != 409 {
		t.Fatalf("same version: %d %s", rec.Code, rec.Body)
	}
	unchanged("same version")
	// Signed and newer, but it won't run here.
	if rec := send([]byte("not a program"), next, "", priv); rec.Code != 422 {
		t.Fatalf("won't run: %d %s", rec.Code, rec.Body)
	}
	unchanged("won't run")
	// Says a different version than it was sent as.
	if rec := send(fakeAgent("8.0.0"), next, "", priv); rec.Code != 422 {
		t.Fatalf("wrong version inside: %d %s", rec.Code, rec.Body)
	}
	unchanged("wrong version inside")

	// The real thing: swapped in, old one kept, then a restart.
	rec := send(fakeAgent(next), next, "", priv)
	if rec.Code != 200 {
		t.Fatalf("update: %d %s", rec.Code, rec.Body)
	}
	<-restarted
	if got, _ := os.ReadFile(exe); !bytes.Equal(got, fakeAgent(next)) {
		t.Fatalf("new agent not in place: %q", got)
	}
	if got, _ := os.ReadFile(exe + ".old"); !bytes.Equal(got, fakeAgent(agentVersion)) {
		t.Fatalf("old agent not kept: %q", got)
	}
}

func TestSelfUpdateNeedsAKey(t *testing.T) {
	oldKey := updatePublicKey
	updatePublicKey = ""
	defer func() { updatePublicKey = oldKey }()
	req := httptest.NewRequest(http.MethodPost, "/api/self-update", strings.NewReader("x"))
	req.Header.Set("X-LANHub-SHA256", strings.Repeat("a", 64))
	req.Header.Set("X-LANHub-Version", "9.0.0")
	req.Header.Set("X-LANHub-Signature", base64.StdEncoding.EncodeToString(make([]byte, 64)))
	rec := httptest.NewRecorder()
	handleSelfUpdate(rec, req)
	if rec.Code != 501 {
		t.Fatalf("no key: %d", rec.Code)
	}
}

func TestConfigure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("LANHUB_AGENT_CONFIG", path)
	if err := runConfigure([]string{"--port", "9000"}); err != nil {
		t.Fatal(err)
	}
	if hasToken() {
		t.Fatal("a new config has no token")
	}
	// A config saved by Windows PowerShell 5.1 starts with a byte-order mark
	// and has settings of its own: both survive.
	os.WriteFile(path, []byte("\xef\xbb\xbf{\"token\":\"old\",\"port\":9000,\"allowed_roots\":[\"/srv\"],\"extra\":true}"), 0o600)
	if err := runConfigure([]string{"--token", "new-token"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Token != "new-token" || cfg.Port != 9000 || len(cfg.AllowedRoots) != 1 || !hasToken() {
		t.Fatalf("after configure: %+v", cfg)
	}
	if raw, _ := os.ReadFile(path); !strings.Contains(string(raw), `"extra": true`) || bytes.HasPrefix(raw, []byte("\xef\xbb\xbf")) {
		t.Fatalf("config: %s", raw)
	}
}
