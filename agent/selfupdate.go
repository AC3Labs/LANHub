package main

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// --- POST /api/self-update ---
//
// The hub sends a new agent program as the request body. The agent only
// installs it if it carries a signature from LANHub's release key (the
// public half is below; the private half only exists in the project's
// GitHub Actions secrets), so the agent token alone — or a hub someone else
// controls — can't make an agent run a program of their choosing.
//
// Then it checks the checksum, refuses older versions, runs the new
// program once (`version`) to be sure this machine will start it (Windows
// Smart App Control blocks unsigned programs it hasn't seen), swaps it in
// and exits, so systemd (Restart=always) or the Windows service's
// restart-on-failure starts the new one. The previous program is kept as
// lanhub-agent(.exe).old.

// updatePublicKey verifies agent updates. Builds of a fork that publish
// their own agents set their own key here (and in their build secrets).
var updatePublicKey = "dfgs6angsP9rlD8BPcy9nCCR5JZojzAmwOykbGQL32o="

const maxAgentSize = 100 << 20

// updateMessage is what the release key signs: the program's identity, so
// a signature can't be reused for another version, platform or file.
func updateMessage(version, goos, goarch, sha string) []byte {
	return []byte("lanhub-agent-update\n" + version + "\n" + goos + "/" + goarch + "\n" + sha + "\n")
}

// Swapped out in tests.
var (
	agentExecutable = func() (string, error) {
		exe, err := os.Executable()
		if err != nil {
			return "", err
		}
		return filepath.EvalSymlinks(exe)
	}
	restartAgent = func() {
		time.Sleep(500 * time.Millisecond)
		os.Exit(1)
	}
)

func handleSelfUpdate(w http.ResponseWriter, r *http.Request) {
	wantSum := strings.ToLower(strings.TrimSpace(r.Header.Get("X-LANHub-SHA256")))
	version := strings.TrimSpace(r.Header.Get("X-LANHub-Version"))
	sig, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(r.Header.Get("X-LANHub-Signature")))
	if len(wantSum) != 64 || version == "" || len(sig) != ed25519.SignatureSize {
		writeError(w, 400, "Send the new agent with its SHA-256, version and signature.")
		return
	}
	pub, err := base64.StdEncoding.DecodeString(updatePublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		writeError(w, 501, "This agent was built without an update key, so it can only be updated by running its installer again.")
		return
	}
	if !ed25519.Verify(pub, updateMessage(version, runtime.GOOS, runtime.GOARCH, wantSum), sig) {
		writeError(w, 403, "That agent isn't signed by LANHub's release key, so it wasn't installed.")
		return
	}
	if compareVersions(version, agentVersion) <= 0 {
		writeError(w, 409, fmt.Sprintf("This agent is already version %s; it only updates to a newer one.", agentVersion))
		return
	}

	exe, err := agentExecutable()
	if err != nil {
		writeError(w, 500, "Can't find this agent's own program: "+err.Error())
		return
	}
	dir := filepath.Dir(exe)
	pattern := ".lanhub-agent-new-*"
	if runtime.GOOS == "windows" {
		pattern += ".exe" // Windows only runs programs ending in .exe
	}
	tmp, err := os.CreateTemp(dir, pattern)
	if err != nil {
		writeError(w, 500, fmt.Sprintf("Can't write to %s (%v). Run the installer again to update this agent.", dir, err))
		return
	}
	newPath := tmp.Name()
	keep := false
	defer func() {
		if !keep {
			os.Remove(newPath)
		}
	}()

	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(r.Body, maxAgentSize+1))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		writeError(w, 500, "Couldn't receive the new agent: "+err.Error())
		return
	}
	if n > maxAgentSize {
		writeError(w, 413, "The new agent is too big.")
		return
	}
	if hex.EncodeToString(h.Sum(nil)) != wantSum {
		writeError(w, 400, "The new agent arrived damaged (its checksum doesn't match), so nothing was changed.")
		return
	}
	os.Chmod(newPath, 0o755)

	if err := checkNewAgent(newPath, version); err != nil {
		writeError(w, 422, err.Error())
		return
	}

	old := exe + ".old"
	os.Remove(old)
	// A running program can be renamed (on Windows too), just not
	// overwritten.
	if err := os.Rename(exe, old); err != nil {
		writeError(w, 500, fmt.Sprintf("Can't replace %s (%v). Run the installer again to update this agent.", exe, err))
		return
	}
	if err := os.Rename(newPath, exe); err != nil {
		os.Rename(old, exe)
		writeError(w, 500, "Can't put the new agent in place: "+err.Error())
		return
	}
	keep = true
	writeJSON(w, 200, map[string]any{"ok": true, "from": agentVersion, "to": version})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	go restartAgent()
}

// checkNewAgent runs the new program once and expects its version back.
func checkNewAgent(path, want string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "version").CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		if strings.Contains(strings.ToLower(msg+" "+err.Error()), "application control") {
			return fmt.Errorf("Windows blocked the new agent (Smart App Control or another Application Control policy), so this agent was left as it is")
		}
		return fmt.Errorf("the new agent wouldn't run on this machine (%s), so this agent was left as it is", msg)
	}
	if got := strings.TrimSpace(string(out)); got != want {
		return fmt.Errorf("the new agent says it's version %q, expected %q, so this agent was left as it is", got, want)
	}
	return nil
}

// compareVersions compares dotted numeric versions ("1.4.0").
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
