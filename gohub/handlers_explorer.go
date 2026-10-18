package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

func (app *App) explorer(w http.ResponseWriter, r *http.Request) {
	machines, err := app.store.AccessibleMachines(userFrom(r).ID, "sort")
	if err != nil {
		app.errorPage(w, r, 500, "Couldn't load machines.")
		return
	}
	type paneMachine struct {
		ID       int64  `json:"id"`
		Name     string `json:"name"`
		OS       string `json:"os"`
		Color    string `json:"color"`
		CanWrite bool   `json:"canWrite"`
	}
	list := make([]paneMachine, 0, len(machines))
	for _, m := range machines {
		list = append(list, paneMachine{m.ID, m.Name, m.OS, string(safeColor(m.Color)), m.Role == "full"})
	}
	// ?machine=ID opens that machine's pane first, as in the PHP hub.
	first, _ := strconv.ParseInt(r.URL.Query().Get("machine"), 10, 64)
	app.render(w, r, http.StatusOK, "explorer", "Explorer", map[string]any{"Machines": list, "First": first})
}

func (app *App) apiMachines(w http.ResponseWriter, r *http.Request) {
	machines, _ := app.store.AccessibleMachines(userFrom(r).ID, "sort")
	out := []map[string]any{}
	for _, m := range machines {
		out = append(out, map[string]any{"id": m.ID, "name": m.Name, "os": m.OS, "color": string(safeColor(m.Color)), "canWrite": m.Role == "full", "status": m.Status})
	}
	writeJSON(w, http.StatusOK, out)
}

// machineFor loads {id} and checks the user's access level.
func (app *App) machineFor(w http.ResponseWriter, r *http.Request, atLeast string) (*Machine, *AgentClient, bool) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return app.machineByID(w, r, id, atLeast)
}

func (app *App) machineByID(w http.ResponseWriter, r *http.Request, id int64, atLeast string) (*Machine, *AgentClient, bool) {
	m, err := app.store.Machine(id)
	if err != nil {
		app.errorPage(w, r, http.StatusNotFound, "That machine doesn't exist.")
		return nil, nil, false
	}
	if !app.store.CanAccess(userFrom(r).ID, m.ID, atLeast) {
		app.errorPage(w, r, http.StatusForbidden, "You don't have access to do that on "+m.Name+".")
		return nil, nil, false
	}
	ag, err := app.store.Agent(m)
	if err != nil {
		app.errorPage(w, r, http.StatusBadGateway, err.Error())
		return nil, nil, false
	}
	return m, ag, true
}

func agentFail(w http.ResponseWriter, err error) {
	status := agentStatus(err)
	if status == 0 || status >= 500 {
		status = http.StatusBadGateway
	}
	writeJSON(w, status, map[string]string{"message": err.Error()})
}

// apiList returns a folder's entries, or the drives when no path is given.
// Like loadPane(), it also records whether the machine answered.
func (app *App) apiList(w http.ResponseWriter, r *http.Request) {
	m, ag, ok := app.machineFor(w, r, "read_only")
	if !ok {
		return
	}
	path := r.URL.Query().Get("path")
	var entries []Entry
	var err error
	if path == "" {
		var drives []Drive
		if drives, err = ag.Drives(r.Context()); err == nil {
			for _, d := range drives {
				entries = append(entries, Entry{Name: d.Label, Path: d.Path, Type: "drive"})
			}
		}
	} else {
		entries, err = ag.List(r.Context(), path)
	}
	if err != nil {
		if agentStatus(err) == 0 {
			app.store.SetMachineStatus(m.ID, false)
		}
		agentFail(w, err)
		return
	}
	app.store.SetMachineStatus(m.ID, true)
	if entries == nil {
		entries = []Entry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
}

func (app *App) apiMkdir(w http.ResponseWriter, r *http.Request) {
	_, ag, ok := app.machineFor(w, r, "full")
	if !ok {
		return
	}
	var body struct{ Path string }
	if readJSON(r, &body) != nil || body.Path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "path is required."})
		return
	}
	if err := ag.Mkdir(r.Context(), body.Path); err != nil {
		agentFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"path": body.Path})
}

func (app *App) apiRename(w http.ResponseWriter, r *http.Request) {
	_, ag, ok := app.machineFor(w, r, "full")
	if !ok {
		return
	}
	var body struct {
		Path    string `json:"path"`
		NewName string `json:"new_name"`
	}
	if readJSON(r, &body) != nil || body.Path == "" || body.NewName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "path and new_name are required."})
		return
	}
	if err := ag.Rename(r.Context(), body.Path, body.NewName); err != nil {
		agentFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (app *App) apiDelete(w http.ResponseWriter, r *http.Request) {
	_, ag, ok := app.machineFor(w, r, "full")
	if !ok {
		return
	}
	var body struct{ Path string }
	if readJSON(r, &body) != nil || body.Path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "path is required."})
		return
	}
	if err := ag.Delete(r.Context(), body.Path); err != nil {
		agentFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// apiUpload streams each uploaded file straight on to the agent, part by
// part, without storing it on the hub.
func (app *App) apiUpload(w http.ResponseWriter, r *http.Request) {
	_, ag, ok := app.machineFor(w, r, "full")
	if !ok {
		return
	}
	dir := r.URL.Query().Get("path")
	if dir == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "Open a folder before uploading."})
		return
	}
	mr, err := r.MultipartReader()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "Expected a file upload."})
		return
	}
	var uploaded []Entry
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "The upload was interrupted."})
			return
		}
		name := part.FileName()
		if part.FormName() != "files" || name == "" {
			part.Close()
			continue
		}
		e, err := ag.Upload(r.Context(), dir, name, part)
		part.Close()
		if err != nil {
			agentFail(w, err)
			return
		}
		uploaded = append(uploaded, *e)
	}
	writeJSON(w, http.StatusOK, map[string]any{"uploaded": uploaded})
}

// ---- search ----

func (app *App) apiSearch(w http.ResponseWriter, r *http.Request) {
	m, ag, ok := app.machineFor(w, r, "read_only")
	if !ok {
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "Type something to search for."})
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		path = m.RootPath()
	}
	res, err := ag.Search(r.Context(), path, q)
	if err != nil {
		agentFail(w, err)
		return
	}
	if res.Entries == nil {
		res.Entries = []Entry{}
	}
	writeJSON(w, http.StatusOK, res)
}

// dedupeDrives drops a drive whose path is inside another drive on the
// same machine (Linux reports "/" and "/home/<user>"), which made the PHP
// hub report every home-folder match twice. See docs/SEARCH_SPEC.md §6.
func dedupeDrives(drives []Drive, sep string) []Drive {
	norm := func(p string) string {
		if strings.HasSuffix(p, sep) {
			return p
		}
		return p + sep
	}
	var out []Drive
	for i, d := range drives {
		inside := false
		for j, o := range drives {
			if i != j && norm(d.Path) != norm(o.Path) && strings.HasPrefix(norm(d.Path), norm(o.Path)) {
				inside = true
				break
			}
			if i > j && norm(d.Path) == norm(o.Path) {
				inside = true // exact duplicate
				break
			}
		}
		if !inside {
			out = append(out, d)
		}
	}
	return out
}

type searchEvent struct {
	Type      string  `json:"type"` // start | results | failed | machine_done | done
	MachineID int64   `json:"machine_id,omitempty"`
	Machine   string  `json:"machine,omitempty"`
	Color     string  `json:"color,omitempty"`
	Entries   []Entry `json:"entries,omitempty"`
	Truncated bool    `json:"truncated,omitempty"`
	Reason    string  `json:"reason,omitempty"`
	Count     int     `json:"count,omitempty"`
}

// apiGlobalSearch searches every accessible machine at once and streams
// results (newline-delimited JSON) as each drive answers. Machines run in
// parallel, with up to 2 drives at a time per machine.
func (app *App) apiGlobalSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "Type something to search for."})
		return
	}
	machines, _ := app.store.AccessibleMachines(userFrom(r).ID, "name")
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, _ := w.(http.Flusher)
	events := make(chan searchEvent, 64)

	var mu sync.Mutex
	send := func(ev searchEvent) {
		mu.Lock()
		defer mu.Unlock()
		select {
		case events <- ev:
		case <-r.Context().Done():
		}
	}
	go func() {
		var wg sync.WaitGroup
		for _, m := range machines {
			wg.Add(1)
			go func() {
				defer wg.Done()
				app.searchMachine(r.Context(), m, q, send)
			}()
		}
		wg.Wait()
		close(events)
	}()

	enc := json.NewEncoder(w)
	enc.Encode(searchEvent{Type: "start", Count: len(machines)})
	if flusher != nil {
		flusher.Flush()
	}
	for ev := range events {
		if enc.Encode(ev) != nil {
			return
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
	enc.Encode(searchEvent{Type: "done"})
}

func (app *App) searchMachine(ctx context.Context, m *Machine, q string, send func(searchEvent)) {
	base := searchEvent{MachineID: m.ID, Machine: m.Name, Color: string(safeColor(m.Color))}
	fail := func(err error) {
		ev := base
		ev.Type, ev.Reason = "failed", err.Error()
		send(ev)
	}
	ag, err := app.store.Agent(m)
	if err != nil {
		fail(err)
		return
	}
	drives, err := ag.Drives(ctx)
	if err != nil {
		fail(err)
		return
	}
	drives = dedupeDrives(drives, m.Separator())
	sem := make(chan struct{}, 2)
	var wg sync.WaitGroup
	var once sync.Once
	for _, d := range drives {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			res, err := ag.Search(ctx, d.Path, q)
			if err != nil {
				once.Do(func() { fail(err) }) // one failure line per machine
				return
			}
			ev := base
			ev.Type, ev.Entries, ev.Truncated = "results", res.Entries, res.Truncated
			send(ev)
		}()
	}
	wg.Wait()
	ev := base
	ev.Type = "machine_done"
	send(ev)
}

// ---- drag and drop ----

var crossDevice = regexp.MustCompile(`(?i)cross-device|different disk drive|not same device`)

// dropRefused answers a drop that won't run, and logs why (so a refusal can
// be diagnosed from the container log, not just the user's screen).
func dropRefused(w http.ResponseWriter, r *http.Request, status int, msg string) {
	logf("drop refused for %s (%d): %s", userFrom(r).Email, status, msg)
	writeJSON(w, status, map[string]string{"message": msg})
}

func (app *App) apiDrop(w http.ResponseWriter, r *http.Request) {
	var b struct {
		SourceMachine int64   `json:"source_machine"`
		SourcePath    string  `json:"source_path"`
		Name          string  `json:"name"`
		TargetMachine int64   `json:"target_machine"`
		TargetDir     *string `json:"target_dir"`
		Copy          bool    `json:"copy"`
		IsDir         bool    `json:"is_dir"`
	}
	if readJSON(r, &b) != nil || b.SourcePath == "" || b.Name == "" || strings.ContainsAny(b.Name, `/\`) {
		dropRefused(w, r, http.StatusBadRequest, "Invalid drop.")
		return
	}
	srcLevel := "full"
	if b.Copy {
		srcLevel = "read_only"
	}
	src, srcAgent, ok := app.machineByID(w, r, b.SourceMachine, srcLevel)
	if !ok {
		return
	}
	dst, _, ok := app.machineByID(w, r, b.TargetMachine, "full")
	if !ok {
		return
	}
	// No folder means the pane was showing its drive list. Don't quietly
	// aim at the top of the system drive: ask for a real destination.
	if b.TargetDir == nil || *b.TargetDir == "" {
		dropRefused(w, r, http.StatusUnprocessableEntity, "Open a folder on "+dst.Name+" first, or drop onto one of its drives or folders.")
		return
	}
	sep := dst.Separator()
	dir := *b.TargetDir
	destination := strings.TrimRight(dir, sep) + sep + b.Name
	if src.ID == dst.ID && destination == b.SourcePath {
		writeJSON(w, http.StatusOK, map[string]string{"result": "nothing to do"})
		return
	}
	if b.IsDir && src.ID == dst.ID && strings.HasPrefix(strings.TrimRight(destination, sep)+sep, strings.TrimRight(b.SourcePath, sep)+sep) {
		dropRefused(w, r, http.StatusUnprocessableEntity, "Can't put a folder inside itself.")
		return
	}

	if src.ID != dst.ID {
		id, err := app.relays.Start(src, b.SourcePath, dst, destination, b.Copy, b.IsDir, userFrom(r).ID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"result": "relay", "relay_id": id})
		return
	}

	var err error
	if b.Copy {
		err = srcAgent.Copy(r.Context(), b.SourcePath, destination)
	} else if err = srcAgent.Move(r.Context(), b.SourcePath, destination); err != nil && crossDevice.MatchString(err.Error()) {
		// A rename can't cross drives: copy, then delete the original
		// only once the copy succeeded.
		if err = srcAgent.Copy(r.Context(), b.SourcePath, destination); err == nil {
			err = srcAgent.Delete(r.Context(), b.SourcePath)
		}
	}
	if err != nil {
		logf("drop failed for %s: %v", userFrom(r).Email, err)
		agentFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"result": "done"})
}

// ---- transfers panel ----

type panelJob struct {
	Kind      string  `json:"kind"`
	Label     string  `json:"label"`
	Status    string  `json:"status"`
	Percent   int     `json:"percent"`
	Error     *string `json:"error"`
	Color     string  `json:"color"`
	CreatedAt string  `json:"created_at"`
	Bytes     int64   `json:"bytes"`
}

func baseName(p string) string {
	p = strings.TrimRight(strings.ReplaceAll(p, `\`, "/"), "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// apiTransfers mirrors Transfers\Panel: agents' own queued jobs plus the
// hub's relays from the last 10 minutes, finished ones for 30 s.
func (app *App) apiTransfers(w http.ResponseWriter, r *http.Request) {
	machines, _ := app.store.AccessibleMachines(userFrom(r).ID, "sort")
	var mu sync.Mutex
	jobs := []panelJob{}
	ids := []string{}
	for _, m := range machines {
		ids = append(ids, strconv.FormatInt(m.ID, 10))
	}
	forEachMachine(machines, func(m *Machine) {
		ag, err := app.store.Agent(m)
		if err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
		defer cancel()
		list, err := ag.Transfers(ctx)
		if err != nil {
			return
		}
		for _, t := range list {
			created, _ := time.Parse(time.RFC3339, t.CreatedAt)
			if t.Status != "pending" && t.Status != "running" && time.Since(created) > 30*time.Second {
				continue
			}
			pct := 0
			if t.BytesTotal > 0 {
				pct = int(min(100, t.BytesDone*100/t.BytesTotal))
			} else if t.Status == "done" {
				pct = 100
			}
			mu.Lock()
			jobs = append(jobs, panelJob{Kind: t.Kind, Label: baseName(t.Destination), Status: t.Status, Percent: pct, Error: t.Error, Color: string(safeColor(m.Color)), CreatedAt: t.CreatedAt})
			mu.Unlock()
		}
	})
	if len(ids) > 0 {
		rows, err := app.store.db.Query(`SELECT r.kind, r.destination_path, r.status, r.error, m.color, r.created_at, r.completed_at, coalesce(r.bytes_transferred, 0)
			FROM relay_transfers r JOIN machines m ON m.id = r.destination_machine_id
			WHERE r.destination_machine_id IN (`+strings.Join(ids, ",")+`) AND r.created_at >= ?
			ORDER BY r.created_at DESC LIMIT 50`, time.Now().UTC().Add(-10*time.Minute).Format(dbTime))
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var kind, dest, status, color, created string
				var errMsg, completed nullStr
				var bytes int64
				if rows.Scan(&kind, &dest, &status, &errMsg, &color, &created, &completed, &bytes) != nil {
					continue
				}
				running := status == "queued" || status == "downloading" || status == "uploading"
				if !running {
					if c := parseDBTime(completed.NullString); c != nil && time.Since(*c) > 30*time.Second {
						continue
					}
				}
				pct := map[string]int{"queued": 10, "downloading": 40, "uploading": 75, "done": 100, "error": 100}[status]
				if total := app.relays.Total(dest); running && total > 0 {
					pct = int(min(99, bytes*100/total))
				}
				st := status
				if running {
					st = "running"
				}
				var e *string
				if errMsg.Valid {
					e = &errMsg.String
				}
				ct, _ := parseTimeText(created)
				jobs = append(jobs, panelJob{Kind: kind, Label: baseName(dest), Status: st, Percent: pct, Error: e, Color: string(safeColor(color)), CreatedAt: ct.Format(time.RFC3339), Bytes: bytes})
			}
		}
	}
	sortJobs(jobs)
	writeJSON(w, http.StatusOK, jobs)
}

func sortJobs(jobs []panelJob) {
	for i := 1; i < len(jobs); i++ {
		for j := i; j > 0 && jobs[j].CreatedAt > jobs[j-1].CreatedAt; j-- {
			jobs[j], jobs[j-1] = jobs[j-1], jobs[j]
		}
	}
}

// ---- download and preview (streamed) ----

func (app *App) download(w http.ResponseWriter, r *http.Request) {
	app.streamFromAgent(w, r, "/api/download")
}

func (app *App) preview(w http.ResponseWriter, r *http.Request) {
	app.streamFromAgent(w, r, "/api/preview")
}

// streamFromAgent passes an agent's file response to the browser as it
// arrives, never holding the file. The agent's Content-Length goes through
// so a cut-short transfer shows as failed in the browser.
func (app *App) streamFromAgent(w http.ResponseWriter, r *http.Request, endpoint string) {
	_, ag, ok := app.machineFor(w, r, "read_only")
	if !ok {
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		app.errorPage(w, r, http.StatusBadRequest, "No file was given.")
		return
	}
	st, err := ag.Open(r.Context(), endpoint, path)
	if err != nil {
		status := agentStatus(err)
		switch status {
		case 401, 403, 404, 415:
		default:
			status = http.StatusBadGateway
		}
		app.errorPage(w, r, status, err.Error())
		return
	}
	defer st.Close()

	h := w.Header()
	if endpoint == "/api/download" {
		h.Set("Content-Type", "application/octet-stream")
		h.Set("Content-Disposition", attachmentHeader(baseName(path)))
		h.Set("X-Content-Type-Options", "nosniff")
	} else {
		for _, k := range []string{"Content-Type", "Content-Disposition", "X-Content-Type-Options"} {
			if v := st.Resp.Header.Get(k); v != "" {
				h.Set(k, v)
			}
		}
		// A previewed SVG or HTML file is served from the hub's own origin;
		// even opened directly (not in the sandboxed preview frame) it
		// mustn't run scripts as the hub.
		h.Set("Content-Security-Policy", "sandbox")
	}
	if cl := st.Resp.Header.Get("Content-Length"); cl != "" {
		h.Set("Content-Length", cl)
	}
	h.Set("Cache-Control", "private, no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	buf := make([]byte, 1<<20)
	if _, err := io.CopyBuffer(w, st, buf); err != nil && !errors.Is(err, context.Canceled) {
		// Headers are already sent; the short body tells the browser.
		logf("stream of %s stopped: %v", path, err)
	}
}

// mediaTypes are the video and audio files browsers can play directly.
// Anything else (e.g. .avi) isn't offered as playable.
var mediaTypes = map[string]string{
	"mp4": "video/mp4", "m4v": "video/mp4", "mov": "video/mp4", "webm": "video/webm", "mkv": "video/webm", "ogv": "video/ogg",
	"mp3": "audio/mpeg", "m4a": "audio/mp4", "aac": "audio/aac", "wav": "audio/wav", "ogg": "audio/ogg", "oga": "audio/ogg",
	"opus": "audio/ogg", "flac": "audio/flac",
}

func mediaType(name string) string {
	i := strings.LastIndex(name, ".")
	if i < 0 {
		return ""
	}
	return mediaTypes[strings.ToLower(name[i+1:])]
}

// media streams a video or audio file for the browser's own player. The
// agent's preview endpoint refuses media, so this reads /api/download and
// labels it with the right type. Seek requests (Range) go to the agent; when
// it answers with part of the file, that's passed on, otherwise the whole
// file plays from the start.
func (app *App) mediaFile(w http.ResponseWriter, r *http.Request) {
	_, ag, ok := app.machineFor(w, r, "read_only")
	if !ok {
		return
	}
	path := r.URL.Query().Get("path")
	ctype := mediaType(baseName(path))
	if path == "" || ctype == "" {
		app.errorPage(w, r, http.StatusUnsupportedMediaType, "This file can't be played in the browser.")
		return
	}
	st, err := ag.OpenRange(r.Context(), "/api/download", path, r.Header.Get("Range"))
	if err != nil {
		status := agentStatus(err)
		if status != 403 && status != 404 {
			status = http.StatusBadGateway
		}
		app.errorPage(w, r, status, err.Error())
		return
	}
	defer st.Close()
	h := w.Header()
	h.Set("Content-Type", ctype)
	h.Set("Content-Disposition", "inline")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "sandbox")
	h.Set("Cache-Control", "private, no-store")
	for _, k := range []string{"Content-Length", "Content-Range", "Accept-Ranges"} {
		if v := st.Resp.Header.Get(k); v != "" {
			h.Set(k, v)
		}
	}
	status := http.StatusOK
	if st.Resp.StatusCode == http.StatusPartialContent {
		status = http.StatusPartialContent
	}
	w.WriteHeader(status)
	if _, err := io.CopyBuffer(w, st, make([]byte, 1<<20)); err != nil && !errors.Is(err, context.Canceled) {
		logf("media stream of %s stopped: %v", path, err)
	}
}

// attachmentHeader gives an ASCII fallback plus the real UTF-8 name.
func attachmentHeader(name string) string {
	fallback := []rune{}
	for _, c := range name {
		if c < 0x20 || c > 0x7e || strings.ContainsRune(`"\%/`, c) {
			c = '_'
		}
		fallback = append(fallback, c)
	}
	fb := string(fallback)
	if fb == "" {
		fb = "download"
	}
	return mime.FormatMediaType("attachment", map[string]string{"filename": fb}) + "; filename*=utf-8''" + pathEscapeRFC5987(name)
}

func pathEscapeRFC5987(s string) string {
	const ok = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!#$&+-.^_`|~"
	var b strings.Builder
	for _, c := range []byte(s) {
		if strings.IndexByte(ok, c) >= 0 {
			b.WriteByte(c)
		} else {
			b.WriteString("%" + strings.ToUpper(strconv.FormatInt(int64(c)|0x100, 16)[1:]))
		}
	}
	return b.String()
}
