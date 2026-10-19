package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Direct agent-to-agent transfers (docs/DIRECT_TRANSFER_DESIGN.md): the
// destination agent pulls each file straight from the source agent with a
// short-lived pass the hub signs with the source's token. The hub only
// coordinates and compares checksums; no file bytes pass through it. Any
// problem before a file is committed falls back to the streamed relay.

// passLife is how long a pass works for starting or resuming a download.
// The hub hands the destination a fresh one well before it runs out.
const (
	passLife      = 15 * time.Minute
	passRenewal   = 5 * time.Minute
	fetchPoll     = 500 * time.Millisecond
	fetchLostTime = 60 * time.Second // destination unreachable this long: give up
)

type passFields struct {
	V       int      `json:"v"`
	Job     int64    `json:"job"`
	File    int      `json:"file"`
	Path    string   `json:"path"`
	Size    int64    `json:"size"`
	Mtime   string   `json:"mtime"`
	Holders []string `json:"holders"`
	Expires int64    `json:"expires"`
	Nonce   string   `json:"nonce"`
}

// mintPass signs fields with the source agent's token: only that agent
// (and this hub) can check it, and the destination never sees the token.
func mintPass(sourceToken string, f passFields) string {
	f.V = 1
	f.Expires = time.Now().Add(passLife).Unix()
	nonce := make([]byte, 16)
	rand.Read(nonce)
	f.Nonce = hex.EncodeToString(nonce)
	payload, _ := json.Marshal(f)
	mac := hmac.New(sha256.New, []byte(sourceToken))
	mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// ---- agent calls ----

type agentFetch struct {
	ID     string  `json:"id"`
	Status string  `json:"status"`
	Bytes  int64   `json:"bytes"`
	SHA256 string  `json:"sha256"`
	Error  *string `json:"error"`
}

func (a *AgentClient) CreateFetch(ctx context.Context, sourceURL, pass, dest string, size int64, insecure bool) (agentFetch, error) {
	var out agentFetch
	return out, a.postJSON(ctx, "/api/fetches", map[string]any{
		"source_url": sourceURL, "pass": pass, "destination": dest, "size": size, "insecure": insecure,
	}, &out)
}

func (a *AgentClient) Fetch(ctx context.Context, id string) (agentFetch, error) {
	var out agentFetch
	return out, a.getJSON(ctx, "/api/fetches/"+url.PathEscape(id), nil, &out)
}

func (a *AgentClient) RenewFetchPass(ctx context.Context, id, pass string) error {
	return a.postJSON(ctx, "/api/fetches/"+url.PathEscape(id)+"/pass", map[string]any{"pass": pass}, nil)
}

func (a *AgentClient) CommitFetch(ctx context.Context, id string) (*Entry, error) {
	var e Entry
	if err := a.postJSON(ctx, "/api/fetches/"+url.PathEscape(id)+"/commit", nil, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

func (a *AgentClient) AbortFetch(ctx context.Context, id string) error {
	return a.postJSON(ctx, "/api/fetches/"+url.PathEscape(id)+"/abort", nil, nil)
}

func (a *AgentClient) RetirePass(ctx context.Context, job int64, file int) error {
	return a.postJSON(ctx, "/api/passes/retire", map[string]any{"job": job, "file": file}, nil)
}

// Hash reads the whole file on the agent, so there's no overall time limit.
func (a *AgentClient) Hash(ctx context.Context, path string) (string, error) {
	req, err := a.newRequest(ctx, http.MethodGet, "/api/hash", url.Values{"path": {path}}, nil)
	if err != nil {
		return "", err
	}
	resp, err := a.do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		SHA256 string `json:"sha256"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.SHA256, nil
}

// ---- the relay side ----

// errNoDirect means this file can't go direct, but nothing was written,
// so the relay can safely take over.
type errNoDirect struct{ reason string }

func (e errNoDirect) Error() string { return e.reason }

// canGoDirect asks both agents whether they support fetches. Agents older
// than 1.3.0 don't, and the relay carries on as before.
func (j *relayJob) canGoDirect() bool {
	if j.src.ID == j.dst.ID || j.rm.app.cfg.DirectOff {
		return false
	}
	ctx, cancel := context.WithTimeout(j.ctx, 5*time.Second)
	defer cancel()
	sh, err1 := j.sa.Health(ctx)
	dh, err2 := j.da.Health(ctx)
	if err1 != nil || err2 != nil || !sh.Has("fetch") || !dh.Has("fetch") {
		return false
	}
	// A machine with two connections (say wired and Wi-Fi) may fetch from
	// either address, so the pass names them all.
	j.holders = dh.Addresses
	if net.ParseIP(j.dst.Host) != nil {
		j.holders = append(j.holders, j.dst.Host)
	}
	return true
}

// sourceEntry finds a file's size and modified time by listing its folder,
// for a single dropped file (folder walks already have them).
func (j *relayJob) sourceEntry(path string) (*Entry, error) {
	dir, name := splitDir(path, j.src.Separator())
	entries, err := j.sa.List(j.ctx, dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.Name == name && e.Type == "file" {
			return &e, nil
		}
	}
	return nil, fmt.Errorf("%s was not found on %s", path, j.src.Name)
}

// directFile copies one file agent to agent. It returns errNoDirect when
// the relay should take over (nothing was committed), or a real error.
func (j *relayJob) directFile(src *Entry, dstPath string) error {
	if src.Size == nil {
		return errNoDirect{"the source size is unknown"}
	}
	size := *src.Size
	j.fileN++
	fields := passFields{Job: j.id, File: j.fileN, Path: src.Path, Size: size, Mtime: src.Modified, Holders: j.holders}
	sourceURL := j.src.BaseURL() + "/api/pass-download"
	insecure := j.src.UseTLS && isPrivateHost(j.src.Host)

	f, err := j.da.CreateFetch(j.ctx, sourceURL, mintPass(j.sa.token, fields), dstPath, size, insecure)
	if err != nil {
		if agentStatus(err) == http.StatusConflict {
			return err // the name is taken: the relay would refuse too
		}
		return errNoDirect{err.Error()}
	}
	defer j.sa.RetirePass(context.Background(), j.id, j.fileN)

	j.rm.mu.Lock()
	j.rm.totals[dstPath] = size
	j.rm.mu.Unlock()
	defer func() {
		j.rm.mu.Lock()
		delete(j.rm.totals, dstPath)
		j.rm.mu.Unlock()
	}()
	j.rm.app.store.db.Exec(`UPDATE relay_transfers SET status = 'uploading', updated_at = ? WHERE id = ?`, nowDB(), j.id)

	// The source hashes its copy while the destination receives, so the
	// check at the end costs little extra time.
	type hashResult struct {
		sum string
		err error
	}
	hashCtx, stopHash := context.WithCancel(j.ctx)
	defer stopHash()
	hashed := make(chan hashResult, 1)
	go func() {
		sum, err := j.sa.Hash(hashCtx, src.Path)
		hashed <- hashResult{sum, err}
	}()

	before := j.bytes.Load()
	abort := func(reason error) error {
		j.da.AbortFetch(context.Background(), f.ID)
		j.bytes.Store(before)
		return reason
	}

	renewed := time.Now()
	lastSeen := time.Now()
	for f.Status == "fetching" {
		time.Sleep(fetchPoll)
		next, err := j.da.Fetch(j.ctx, f.ID)
		if err != nil {
			if time.Since(lastSeen) > fetchLostTime {
				return abort(fmt.Errorf("lost contact with %s during the transfer: %w", j.dst.Name, err))
			}
			continue
		}
		lastSeen = time.Now()
		f = next
		j.bytes.Store(before + f.Bytes)
		if time.Since(renewed) > passRenewal {
			if j.da.RenewFetchPass(j.ctx, f.ID, mintPass(j.sa.token, fields)) == nil {
				renewed = time.Now()
			}
		}
	}
	if f.Status != "received" {
		msg := "the fetch stopped (" + f.Status + ")"
		if f.Error != nil {
			msg = *f.Error
		}
		return abort(errNoDirect{msg})
	}

	h := <-hashed
	if h.err != nil {
		return abort(errNoDirect{"the source couldn't check the file: " + h.err.Error()})
	}
	if !strings.EqualFold(h.sum, f.SHA256) {
		return abort(fmt.Errorf("%s: the copy on %s doesn't match the original (checksum mismatch); nothing was kept", src.Name, j.dst.Name))
	}

	entry, err := j.da.CommitFetch(j.ctx, f.ID)
	if err != nil {
		return abort(err)
	}
	if entry.Size != nil && *entry.Size != size {
		return fmt.Errorf("%s: the destination reports %d bytes, %d were sent", src.Name, *entry.Size, size)
	}
	j.bytes.Store(before + size)
	j.directFiles++
	return nil
}

// copyFile sends one file direct when it can, through the hub otherwise.
func (j *relayJob) copyFile(src *Entry, dstPath string) error {
	if j.direct {
		err := j.directFile(src, dstPath)
		var nd errNoDirect
		if !errors.As(err, &nd) {
			return err
		}
		// One failure (say the destination can't reach the source) is
		// likely to repeat, so the rest of this job uses the relay.
		j.direct = false
		logf("relay #%d: direct transfer of %s not possible (%s); relaying through the hub", j.id, src.Path, nd.reason)
	}
	return j.relayFile(src.Path, dstPath)
}
