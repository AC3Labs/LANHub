// Command agentbundle builds the agent for every platform the hub hands
// out, signs each build with LANHub's release key, and writes them (gzipped)
// with a manifest into the hub's agentbuilds folder, which the hub embeds.
//
//	go run ./cmd/agentbundle -agent ../agent -out agentbuilds
//
// The key is a base64 Ed25519 seed in $LANHUB_AGENT_SIGNING_KEY, or in the
// file named by $LANHUB_AGENT_SIGNING_KEY_FILE. Without one the builds are
// still bundled (for the install commands), just unsigned: agents then
// can't update themselves from that hub.
package main

import (
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

type build struct {
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
	Signature string `json:"signature,omitempty"`
}

type manifest struct {
	Version string  `json:"version"`
	Builds  []build `json:"builds"`
}

var platforms = [][2]string{{"linux", "amd64"}, {"linux", "arm64"}, {"windows", "amd64"}}

// UpdateMessage must match updateMessage in the agent's selfupdate.go.
func UpdateMessage(version, goos, goarch, sha string) []byte {
	return []byte("lanhub-agent-update\n" + version + "\n" + goos + "/" + goarch + "\n" + sha + "\n")
}

func main() {
	agentDir := flag.String("agent", "../agent", "the agent's source folder")
	out := flag.String("out", "agentbuilds", "where to write the bundle")
	flag.Parse()

	src, err := os.ReadFile(filepath.Join(*agentDir, "main.go"))
	if err != nil {
		log.Fatal(err)
	}
	m := regexp.MustCompile(`const agentVersion = "([^"]+)"`).FindSubmatch(src)
	if m == nil {
		log.Fatal("can't find agentVersion in the agent's main.go")
	}
	version := string(m[1])

	key, err := signingKey()
	if err != nil {
		log.Fatal(err)
	}
	if key == nil {
		log.Print("no signing key: bundling unsigned agents (they can't be pushed as updates)")
	}

	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatal(err)
	}
	absOut, _ := filepath.Abs(*out)
	man := manifest{Version: version}
	for _, p := range platforms {
		name := "lanhub-agent-" + p[0] + "-" + p[1]
		if p[0] == "windows" {
			name += ".exe"
		}
		bin := filepath.Join(absOut, name)
		cmd := exec.Command("go", "build", "-trimpath", "-ldflags", "-s -w", "-o", bin, ".")
		cmd.Dir = *agentDir
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+p[0], "GOARCH="+p[1])
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			log.Fatalf("building %s: %v", name, err)
		}
		b, err := pack(bin)
		if err != nil {
			log.Fatal(err)
		}
		b.OS, b.Arch, b.Name = p[0], p[1], name
		if key != nil {
			b.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, UpdateMessage(version, p[0], p[1], b.SHA256)))
		}
		man.Builds = append(man.Builds, b)
		fmt.Printf("%s %s (%d bytes)\n", name, b.SHA256, b.Size)
	}
	raw, _ := json.MarshalIndent(man, "", "  ")
	if err := os.WriteFile(filepath.Join(absOut, "manifest.json"), append(raw, '\n'), 0o644); err != nil {
		log.Fatal(err)
	}
}

// pack gzips a build next to itself, removes the original and returns its
// size and checksum.
func pack(bin string) (build, error) {
	f, err := os.Open(bin)
	if err != nil {
		return build{}, err
	}
	defer os.Remove(bin)
	defer f.Close()
	gzf, err := os.Create(bin + ".gz")
	if err != nil {
		return build{}, err
	}
	defer gzf.Close()
	zw, _ := gzip.NewWriterLevel(gzf, gzip.BestCompression)
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(zw, h), f)
	if err != nil {
		return build{}, err
	}
	if err := zw.Close(); err != nil {
		return build{}, err
	}
	return build{Size: n, SHA256: hex.EncodeToString(h.Sum(nil))}, nil
}

func signingKey() (ed25519.PrivateKey, error) {
	val := os.Getenv("LANHUB_AGENT_SIGNING_KEY")
	if f := os.Getenv("LANHUB_AGENT_SIGNING_KEY_FILE"); val == "" && f != "" {
		raw, err := os.ReadFile(f)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		val = string(raw)
	}
	val = strings.TrimSpace(val)
	if val == "" {
		return nil, nil
	}
	seed, err := base64.StdEncoding.DecodeString(val)
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("the signing key must be a base64 Ed25519 seed (%d bytes)", ed25519.SeedSize)
	}
	return ed25519.NewKeyFromSeed(seed), nil
}
