package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"
)

// Installing and updating agents from the hub.
//
// The hub carries the agent builds that match it (gohub/agentbuilds, made
// and signed by cmd/agentbundle at image build time), so updating the hub —
// by image or from Settings → Updates — brings the matching agents along.
// It serves them with one-line install commands, and pushes them to agents
// from the Agents page. Agents only install a pushed program if it carries
// a signature from LANHub's release key, which the hub doesn't have: it
// just passes on the signature made when the image was built.

//go:embed agentbuilds
var agentBuildsFS embed.FS

//go:embed web/agent/install.sh web/agent/install.ps1
var agentScriptsFS embed.FS

type agentBuild struct {
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
	Signature string `json:"signature"`
}

type agentBundle struct {
	Version string       `json:"version"`
	Builds  []agentBuild `json:"builds"`
	fsys    fs.FS
}

// loadAgentBundle reads agentbuilds/manifest.json; nil when the hub was
// built without agents.
func loadAgentBundle(fsys fs.FS) *agentBundle {
	raw, err := fs.ReadFile(fsys, "agentbuilds/manifest.json")
	if err != nil {
		return nil
	}
	b := &agentBundle{fsys: fsys}
	if json.Unmarshal(raw, b) != nil || b.Version == "" || len(b.Builds) == 0 {
		return nil
	}
	return b
}

func (b *agentBundle) find(goos, goarch string) *agentBuild {
	if b == nil {
		return nil
	}
	for i := range b.Builds {
		if b.Builds[i].OS == goos && b.Builds[i].Arch == goarch {
			return &b.Builds[i]
		}
	}
	return nil
}

func (b *agentBundle) byName(name string) *agentBuild {
	if b == nil {
		return nil
	}
	for i := range b.Builds {
		if b.Builds[i].Name == name {
			return &b.Builds[i]
		}
	}
	return nil
}

func (b *agentBundle) open(build *agentBuild) (io.ReadCloser, error) {
	f, err := b.fsys.Open("agentbuilds/" + build.Name + ".gz")
	if err != nil {
		return nil, err
	}
	zr, err := gzip.NewReader(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	return struct {
		io.Reader
		io.Closer
	}{zr, f}, nil
}

// ---- public: downloads and install scripts ----

// GET /agent/download/{name}
func (app *App) agentDownload(w http.ResponseWriter, r *http.Request) {
	build := app.agentBuilds.byName(r.PathValue("name"))
	if build == nil {
		http.NotFound(w, r)
		return
	}
	rc, err := app.agentBuilds.open(build)
	if err != nil {
		http.Error(w, "This hub's agent build is missing.", http.StatusInternalServerError)
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(build.Size, 10))
	w.Header().Set("Content-Disposition", `attachment; filename="`+build.Name+`"`)
	w.Header().Set("X-LANHub-SHA256", build.SHA256)
	io.Copy(w, rc)
}

var safeHost = regexp.MustCompile(`^([A-Za-z0-9.-]+|\[[0-9A-Fa-f:.]+\])(:[0-9]{1,5})?$`)

// hubURLFor is the address this request reached the hub at, which is one
// the machine running the install script can reach.
func hubURLFor(r *http.Request) (string, bool) {
	if !safeHost.MatchString(r.Host) {
		return "", false
	}
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + r.Host, true
}

// GET /agent/install.sh and /agent/install.ps1
func (app *App) agentInstallScript(w http.ResponseWriter, r *http.Request) {
	name := path.Base(r.URL.Path)
	hub, ok := hubURLFor(r)
	if !ok {
		http.Error(w, "Open this from the hub's address.", http.StatusBadRequest)
		return
	}
	if app.agentBuilds == nil {
		http.Error(w, "This hub was built without agent builds, so it can't install agents. Use the installer in the repo's agent/ folder.", http.StatusNotFound)
		return
	}
	raw, err := agentScriptsFS.ReadFile("web/agent/" + name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	tpl, err := template.New(name).Delims("{{%", "%}}").Parse(string(raw))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	sums := map[string]string{}
	for _, b := range app.agentBuilds.Builds {
		sums[b.OS+"-"+b.Arch] = b.SHA256
	}
	var out bytes.Buffer
	if err := tpl.Execute(&out, map[string]any{"Hub": hub, "Version": app.agentBuilds.Version, "Sums": sums}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(out.Bytes())
}

// ---- admins: push an update to one agent ----

var errAgentUpToDate = errors.New("already up to date")

// agentUpdateInfo says whether an agent is behind the hub's bundled agent
// and whether the hub can update it from here.
func (app *App) agentUpdateInfo(h AgentHealth) (updateTo string, canPush bool, why string) {
	b := app.agentBuilds
	if b == nil || h.Version == "" || compareVersions(h.Version, b.Version) >= 0 {
		return "", false, ""
	}
	build := b.find(h.OS, h.Arch)
	switch {
	case !h.Has("self-update"):
		return b.Version, false, "This agent is older than 1.4.0, so it needs one last install by hand; after that it updates from here."
	case build == nil:
		return b.Version, false, "This hub has no agent build for " + h.OS + "/" + h.Arch + "."
	case build.Signature == "":
		return b.Version, false, "This hub's agent builds aren't signed, so agents won't accept them as updates. Run the install command on the machine instead."
	}
	return b.Version, true, ""
}

var agentUpdating sync.Map // machine id -> true while an update runs

// POST /api/agents/{id}/update
func (app *App) apiAgentUpdate(w http.ResponseWriter, r *http.Request) {
	m, ag, ok := app.machineFor(w, r, "full")
	if !ok {
		return
	}
	if _, busy := agentUpdating.LoadOrStore(m.ID, true); busy {
		writeJSON(w, http.StatusConflict, map[string]string{"message": m.Name + "'s agent is already being updated."})
		return
	}
	defer agentUpdating.Delete(m.ID)
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Minute)
	defer cancel()
	version, err := app.pushAgentUpdate(ctx, ag)
	if errors.Is(err, errAgentUpToDate) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": version, "message": m.Name + "'s agent is already up to date."})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": err.Error()})
		return
	}
	app.store.SetMachineStatus(m.ID, true)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": version, "message": m.Name + "'s agent is now " + version + "."})
}

// pushAgentUpdate sends the matching signed build to an agent and waits for
// it to come back as the new version.
func (app *App) pushAgentUpdate(ctx context.Context, ag *AgentClient) (string, error) {
	h, err := ag.Health(ctx)
	if err != nil {
		return "", err
	}
	if app.agentBuilds != nil && compareVersions(h.Version, app.agentBuilds.Version) >= 0 {
		return h.Version, errAgentUpToDate
	}
	to, canPush, why := app.agentUpdateInfo(h)
	if !canPush {
		if why == "" {
			why = "This hub has no newer agent to send."
		}
		return "", errors.New(why)
	}
	build := app.agentBuilds.find(h.OS, h.Arch)
	rc, err := app.agentBuilds.open(build)
	if err != nil {
		return "", err
	}
	defer rc.Close()

	sendCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	req, err := ag.newRequest(sendCtx, http.MethodPost, "/api/self-update", nil, rc)
	if err != nil {
		return "", err
	}
	req.ContentLength = build.Size
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-LANHub-SHA256", build.SHA256)
	req.Header.Set("X-LANHub-Version", to)
	req.Header.Set("X-LANHub-Signature", build.Signature)
	resp, err := ag.do(req)
	if err != nil {
		var ae *AgentError
		if errors.As(err, &ae) && ae.Status == http.StatusNotFound {
			return "", errors.New("this agent can't update itself; run the install command on the machine")
		}
		return "", err
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	// The agent restarts itself; wait for the new one to answer.
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(agentUpdatePoll):
		}
		if nh, err := ag.Health(ctx); err == nil && nh.Version == to {
			return to, nil
		}
	}
	return "", fmt.Errorf("the agent took the update but didn't come back as %s within 90 seconds; check its service on the machine", to)
}

var agentUpdatePoll = 2 * time.Second
