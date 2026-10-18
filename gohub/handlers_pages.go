package main

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"net/mail"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ---- Activity ----

// The page opens with just the filter bar; nothing is fetched from the
// agents until "Show activity" is pressed. Agents log every request, and
// the routine checks the hub makes on its own (the transfers panel every
// 2 s, dashboard drive and network polls) drown out everything a person
// did, so those are left out unless "Include background checks" is ticked.

type activityRow struct {
	ActivityEntry
	Machine *Machine
	When    time.Time
	Action  string // "Downloaded", "Listed folder", ...
	File    string // file or folder name from the request's path
	Folder  string // where it is
	By      string // who asked: a machine's name, "LANHub hub", or the IP
	Shown   string // the logged path, decoded so it reads plainly
}

// readablePath decodes a logged request ("/api/download?path=C%3A%5CTemp")
// into "/api/download?path=C:\Temp" for display. It's text, not a link, so
// there's no reason to keep the URL encoding.
func readablePath(raw string) string {
	endpoint, query, ok := strings.Cut(raw, "?")
	if !ok {
		return raw
	}
	parts := strings.Split(query, "&")
	for i, part := range parts {
		k, v, _ := strings.Cut(part, "=")
		if dk, err := url.QueryUnescape(k); err == nil {
			k = dk
		}
		if dv, err := url.QueryUnescape(v); err == nil {
			v = dv
		}
		parts[i] = k + "=" + v
	}
	return endpoint + "?" + strings.Join(parts, "&")
}

type activityFilter struct {
	Show       bool
	Machine    int64
	Method     string
	Status     string // "", "2xx", "3xx", "4xx", "5xx"
	Background bool
}

var backgroundPaths = map[string]bool{
	"/api/transfers": true, "/api/drives": true, "/api/netstats": true, "/api/activity": true, "/api/health": true,
}

var activityActions = map[string]string{
	"/api/list": "Listed folder", "/api/download": "Downloaded", "/api/preview": "Previewed", "/api/upload": "Uploaded to",
	"/api/search": "Searched", "/api/mkdir": "Created folder", "/api/rename": "Renamed", "/api/move": "Moved",
	"/api/copy": "Copied", "/api/delete": "Deleted", "/api/transfers": "Checked transfers", "/api/drives": "Read drives",
	"/api/netstats": "Read network stats", "/api/activity": "Read activity",
}

const activityPerMachine = 3000 // the agent reads its whole current log either way
const activityShown = 300

// describeRequest turns "/api/download?path=C:\x\a.pdf" into what was done,
// to what, and where.
func describeRequest(raw string) (endpoint, action, file, folder string) {
	endpoint, query, _ := strings.Cut(raw, "?")
	action = activityActions[endpoint]
	if action == "" {
		action = endpoint
	}
	q, _ := url.ParseQuery(query)
	p := q.Get("path")
	if strings.HasPrefix(endpoint, "/api/transfers/") {
		action = "Checked a transfer"
	}
	if p == "" {
		return
	}
	norm := strings.TrimRight(strings.ReplaceAll(p, `\`, "/"), "/")
	i := strings.LastIndex(norm, "/")
	switch {
	case endpoint == "/api/list" || endpoint == "/api/upload" || endpoint == "/api/search":
		// The path is a folder.
		if i >= 0 && norm[i+1:] != "" {
			file, folder = norm[i+1:], p[:min(i+1, len(p))]
		} else {
			file = p
		}
		if endpoint == "/api/search" {
			action = "Searched for “" + q.Get("query") + "” in"
		}
	default:
		if i >= 0 {
			file, folder = norm[i+1:], p[:i+1]
		} else {
			file = p
		}
	}
	return
}

func (app *App) activity(w http.ResponseWriter, r *http.Request) {
	machines, _ := app.store.AccessibleMachines(userFrom(r).ID, "name")
	q := r.URL.Query()
	f := activityFilter{Show: q.Get("show") != "", Method: q.Get("method"), Status: q.Get("status"), Background: q.Get("background") != ""}
	f.Machine, _ = strconv.ParseInt(q.Get("machine"), 10, 64)
	if f.Method != "GET" && f.Method != "POST" {
		f.Method = ""
	}
	if f.Status != "2xx" && f.Status != "3xx" && f.Status != "4xx" && f.Status != "5xx" {
		f.Status = ""
	}
	data := map[string]any{"Machines": machines, "Filter": f}
	if !f.Show {
		app.render(w, r, http.StatusOK, "activity", "Activity", data)
		return
	}

	var targets []*Machine
	for _, m := range machines {
		if f.Machine == 0 || m.ID == f.Machine {
			targets = append(targets, m)
		}
	}
	who := app.requesterNames()
	var mu sync.Mutex
	var rows []activityRow
	var failed []string
	forEachMachine(targets, func(m *Machine) {
		ag, err := app.store.Agent(m)
		var entries []ActivityEntry
		if err == nil {
			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			defer cancel()
			entries, err = ag.Activity(ctx, activityPerMachine)
		}
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			failed = append(failed, m.Name)
			return
		}
		for _, e := range entries {
			if f.Method != "" && e.Method != f.Method {
				continue
			}
			if f.Status != "" && strconv.Itoa(e.Status/100)+"xx" != f.Status {
				continue
			}
			endpoint, action, file, folder := describeRequest(e.Path)
			if !f.Background && (backgroundPaths[endpoint] || strings.HasPrefix(endpoint, "/api/transfers/")) {
				continue
			}
			t, _ := time.Parse(time.RFC3339, e.Time)
			by := who[e.Client]
			if by == "" {
				by = e.Client
			}
			rows = append(rows, activityRow{ActivityEntry: e, Machine: m, When: t, Action: action, File: file, Folder: folder, By: by, Shown: readablePath(e.Path)})
		}
	})
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].When.After(rows[j].When) })
	data["Total"] = len(rows)
	if len(rows) > activityShown {
		rows = rows[:activityShown]
	}
	data["Rows"] = rows
	data["Failed"] = failed
	app.render(w, r, http.StatusOK, "activity", "Activity", data)
}

// requesterNames maps the IP addresses agents see to names: every machine's
// host, and this hub's own addresses (it runs on one of the machines).
func (app *App) requesterNames() map[string]string {
	names := map[string]string{}
	if all, err := app.store.AllMachines(); err == nil {
		for _, m := range all {
			names[m.Host] = m.Name
		}
	}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && !ipn.IP.IsLoopback() {
				names[ipn.IP.String()] = "LANHub hub"
			}
		}
	}
	return names
}

// ---- Agents ----

type agentStats struct {
	SentBytes, ReceivedBytes int64
	sentSecs, recvSecs       float64
	AvgUp, AvgDown           float64
	Active                   int
}

func (app *App) agents(w http.ResponseWriter, r *http.Request) {
	machines, _ := app.store.AccessibleMachines(userFrom(r).ID, "sort")
	// Ping every agent at once and record its status, like Agents\Index::refresh().
	forEachMachine(machines, func(m *Machine) {
		ag, err := app.store.Agent(m)
		online := false
		if err == nil {
			_, err = ag.Health(r.Context())
			online = err == nil
		}
		app.store.SetMachineStatus(m.ID, online)
		if online {
			m.Status = "online"
			now := time.Now().UTC()
			m.LastSeenAt = &now
		} else {
			m.Status = "offline"
		}
	})

	stats := map[int64]*agentStats{}
	ids := []string{}
	for _, m := range machines {
		stats[m.ID] = &agentStats{}
		ids = append(ids, strconv.FormatInt(m.ID, 10))
	}
	if len(ids) > 0 {
		in := strings.Join(ids, ",")
		rows, err := app.store.db.Query(`SELECT source_machine_id, destination_machine_id, coalesce(bytes_transferred, 0), status, started_at, completed_at
			FROM relay_transfers WHERE source_machine_id IN (` + in + `) OR destination_machine_id IN (` + in + `)`)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var src, dst, bytes int64
				var status string
				var started, completed nullStr
				if rows.Scan(&src, &dst, &bytes, &status, &started, &completed) != nil {
					continue
				}
				if status == "queued" || status == "downloading" || status == "uploading" {
					if s := stats[src]; s != nil {
						s.Active++
					}
					if s := stats[dst]; s != nil {
						s.Active++
					}
					continue
				}
				st, ct := parseDBTime(started.NullString), parseDBTime(completed.NullString)
				if status != "done" || bytes == 0 || st == nil || ct == nil {
					continue
				}
				secs := max(ct.Sub(*st).Seconds(), 0.001)
				if s := stats[src]; s != nil {
					s.SentBytes += bytes
					s.sentSecs += secs
				}
				if s := stats[dst]; s != nil {
					s.ReceivedBytes += bytes
					s.recvSecs += secs
				}
			}
		}
	}
	for _, s := range stats {
		if s.sentSecs > 0 {
			s.AvgUp = float64(s.SentBytes) / s.sentSecs
		}
		if s.recvSecs > 0 {
			s.AvgDown = float64(s.ReceivedBytes) / s.recvSecs
		}
	}
	type card struct {
		M *Machine
		S *agentStats
	}
	var cards []card
	for _, m := range machines {
		cards = append(cards, card{m, stats[m.ID]})
	}
	app.render(w, r, http.StatusOK, "agents", "Agents", map[string]any{"Cards": cards})
}

type logRow struct {
	ID       int64
	File     string
	Sent     bool
	Other    string
	OtherCol string
	Bytes    int64
	Created  *time.Time
	Status   string
}

func (app *App) agentLog(w http.ResponseWriter, r *http.Request) {
	m, _, ok := app.machineFor(w, r, "read_only")
	if !ok {
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	const perPage = 25
	var total int
	app.store.db.QueryRow(`SELECT count(*) FROM relay_transfers WHERE source_machine_id = ? OR destination_machine_id = ?`, m.ID, m.ID).Scan(&total)
	rows, err := app.store.db.Query(`SELECT r.id, r.source_machine_id, r.source_path, r.destination_path, coalesce(r.bytes_transferred, 0), r.created_at, r.status,
		coalesce(sm.name, 'Unknown'), coalesce(sm.color, '#8c7a63'), coalesce(dm.name, 'Unknown'), coalesce(dm.color, '#8c7a63')
		FROM relay_transfers r LEFT JOIN machines sm ON sm.id = r.source_machine_id LEFT JOIN machines dm ON dm.id = r.destination_machine_id
		WHERE r.source_machine_id = ? OR r.destination_machine_id = ? ORDER BY r.created_at DESC, r.id DESC LIMIT ? OFFSET ?`,
		m.ID, m.ID, perPage, (page-1)*perPage)
	var list []logRow
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var row logRow
			var srcID int64
			var srcPath, dstPath, sName, sCol, dName, dCol string
			var created nullStr
			if rows.Scan(&row.ID, &srcID, &srcPath, &dstPath, &row.Bytes, &created, &row.Status, &sName, &sCol, &dName, &dCol) != nil {
				continue
			}
			row.Sent = srcID == m.ID
			row.Created = parseDBTime(created.NullString)
			if row.Sent {
				row.File, row.Other, row.OtherCol = baseName(dstPath), dName, dCol
			} else {
				row.File, row.Other, row.OtherCol = baseName(srcPath), sName, sCol
			}
			list = append(list, row)
		}
	}
	pages := (total + perPage - 1) / perPage
	app.render(w, r, http.StatusOK, "agentlog", m.Name+" — Transfer Log", map[string]any{
		"Machine": m, "Rows": list, "Page": page, "Pages": pages, "Prev": page - 1, "Next": page + 1, "Total": total,
	})
}

// ---- Sync rules ----

type syncRule struct {
	ID                   int64
	Name                 string
	SrcID, DstID         int64
	SrcPath, DstPath     string
	SrcName, SrcColor    string
	DstName, DstColor    string
	Direction            string
	Interval             int
	Enabled, HasConflict bool
	LastError            string
	LastRun              *time.Time
}

func (app *App) loadRules(machineIDs []string) []syncRule {
	if len(machineIDs) == 0 {
		return nil
	}
	in := strings.Join(machineIDs, ",")
	rows, err := app.store.db.Query(`SELECT s.id, s.name, s.source_machine_id, s.destination_machine_id, s.source_path, s.destination_path,
		sm.name, sm.color, dm.name, dm.color, s.direction, s.interval_minutes, s.enabled, s.has_conflict, coalesce(s.last_error, ''), s.last_run_at
		FROM sync_rules s JOIN machines sm ON sm.id = s.source_machine_id JOIN machines dm ON dm.id = s.destination_machine_id
		WHERE s.destination_machine_id IN (` + in + `) OR s.source_machine_id IN (` + in + `) ORDER BY s.name`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []syncRule
	for rows.Next() {
		var s syncRule
		var last nullStr
		if rows.Scan(&s.ID, &s.Name, &s.SrcID, &s.DstID, &s.SrcPath, &s.DstPath, &s.SrcName, &s.SrcColor, &s.DstName, &s.DstColor,
			&s.Direction, &s.Interval, &s.Enabled, &s.HasConflict, &s.LastError, &last) == nil {
			s.LastRun = parseDBTime(last.NullString)
			out = append(out, s)
		}
	}
	return out
}

type ruleForm struct {
	ID                    int64
	Name, SrcPath, DstPath string
	SrcID, DstID          int64
	Direction             string
	Interval              int
	Enabled               bool
	Errors                map[string]string
}

func (app *App) syncRules(w http.ResponseWriter, r *http.Request) {
	app.renderRules(w, r, http.StatusOK, nil)
}

func (app *App) renderRules(w http.ResponseWriter, r *http.Request, status int, form *ruleForm) {
	machines, _ := app.store.AccessibleMachines(userFrom(r).ID, "name")
	var ids []string
	for _, m := range machines {
		ids = append(ids, strconv.FormatInt(m.ID, 10))
	}
	app.render(w, r, status, "syncrules", "Sync Rules", map[string]any{"Machines": machines, "Rules": app.loadRules(ids), "Form": form})
}

func (app *App) ruleDestinationAllowed(r *http.Request, ruleID int64) bool {
	var dst int64
	if app.store.db.QueryRow(`SELECT destination_machine_id FROM sync_rules WHERE id = ?`, ruleID).Scan(&dst) != nil {
		return false
	}
	return app.store.CanAccess(userFrom(r).ID, dst, "full")
}

func (app *App) saveSyncRule(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	f := &ruleForm{Errors: map[string]string{}}
	if idStr := r.PathValue("id"); idStr != "" {
		f.ID, _ = strconv.ParseInt(idStr, 10, 64)
		if !app.ruleDestinationAllowed(r, f.ID) {
			app.errorPage(w, r, 403, "You need full access to the destination machine to change this rule.")
			return
		}
	}
	f.Name = strings.TrimSpace(r.PostFormValue("name"))
	f.SrcPath = strings.TrimSpace(r.PostFormValue("source_path"))
	f.DstPath = strings.TrimSpace(r.PostFormValue("destination_path"))
	f.SrcID, _ = strconv.ParseInt(r.PostFormValue("source_machine_id"), 10, 64)
	f.DstID, _ = strconv.ParseInt(r.PostFormValue("destination_machine_id"), 10, 64)
	f.Direction = r.PostFormValue("direction")
	f.Interval, _ = strconv.Atoi(r.PostFormValue("interval_minutes"))
	f.Enabled = r.PostFormValue("enabled") != ""
	if f.Name == "" || len(f.Name) > 255 {
		f.Errors["name"] = "The name field is required."
	}
	if f.SrcPath == "" || len(f.SrcPath) > 1000 {
		f.Errors["source_path"] = "The source path is required."
	}
	if f.DstPath == "" || len(f.DstPath) > 1000 {
		f.Errors["destination_path"] = "The destination path is required."
	}
	if f.Direction != "one_way" && f.Direction != "mirror" {
		f.Errors["direction"] = "Pick a direction."
	}
	if f.Interval < 5 || f.Interval > 10080 {
		f.Errors["interval_minutes"] = "The interval must be between 5 and 10080 minutes."
	}
	if !app.store.CanAccess(u.ID, f.SrcID, "read_only") {
		f.Errors["source_machine_id"] = "Pick a source machine you can access."
	}
	if !app.store.CanAccess(u.ID, f.DstID, "full") {
		f.Errors["destination_machine_id"] = "Pick a destination machine you have full access to."
	}
	if len(f.Errors) > 0 {
		app.renderRules(w, r, http.StatusUnprocessableEntity, f)
		return
	}
	now := nowDB()
	if f.ID > 0 {
		app.store.db.Exec(`UPDATE sync_rules SET name=?, source_machine_id=?, source_path=?, destination_machine_id=?, destination_path=?,
			direction=?, interval_minutes=?, enabled=?, updated_at=? WHERE id=?`,
			f.Name, f.SrcID, f.SrcPath, f.DstID, f.DstPath, f.Direction, f.Interval, f.Enabled, now, f.ID)
	} else {
		app.store.db.Exec(`INSERT INTO sync_rules (name, source_machine_id, source_path, destination_machine_id, destination_path, direction,
			interval_minutes, enabled, has_conflict, created_by, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?)`,
			f.Name, f.SrcID, f.SrcPath, f.DstID, f.DstPath, f.Direction, f.Interval, f.Enabled, u.ID, now, now)
	}
	http.Redirect(w, r, "/sync-rules", http.StatusSeeOther)
}

func (app *App) toggleSyncRule(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if !app.ruleDestinationAllowed(r, id) {
		app.errorPage(w, r, 403, "You need full access to the destination machine to change this rule.")
		return
	}
	app.store.db.Exec(`UPDATE sync_rules SET enabled = NOT enabled, updated_at = ? WHERE id = ?`, nowDB(), id)
	http.Redirect(w, r, "/sync-rules", http.StatusSeeOther)
}

func (app *App) deleteSyncRule(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if !app.ruleDestinationAllowed(r, id) {
		app.errorPage(w, r, 403, "You need full access to the destination machine to delete this rule.")
		return
	}
	app.store.db.Exec(`DELETE FROM sync_rules WHERE id = ?`, id)
	http.Redirect(w, r, "/sync-rules", http.StatusSeeOther)
}

// ---- Users (admin) ----

type inviteForm struct {
	Name, Email string
	Roles       map[int64]string
	Errors      map[string]string
}

func (app *App) users(w http.ResponseWriter, r *http.Request) {
	app.renderUsers(w, r, http.StatusOK, nil)
}

func (app *App) renderUsers(w http.ResponseWriter, r *http.Request, status int, form *inviteForm) {
	users, _ := app.store.Users()
	all, _ := app.store.AllMachines()
	type machineRow struct{ ID int64; Name, Color string }
	machines := make([]machineRow, 0, len(all))
	for _, m := range all {
		machines = append(machines, machineRow{m.ID, m.Name, string(safeColor(m.Color))})
	}
	app.render(w, r, status, "users", "Users", map[string]any{"Users": users, "Machines": machines, "Form": form})
}

func (app *App) inviteUser(w http.ResponseWriter, r *http.Request) {
	f := &inviteForm{
		Name:   strings.TrimSpace(r.PostFormValue("name")),
		Email:  strings.ToLower(strings.TrimSpace(r.PostFormValue("email"))),
		Roles:  map[int64]string{},
		Errors: map[string]string{},
	}
	r.ParseForm()
	for k, v := range r.PostForm {
		if id, err := strconv.ParseInt(strings.TrimPrefix(k, "role_"), 10, 64); err == nil && strings.HasPrefix(k, "role_") && len(v) > 0 {
			if v[0] == "read_only" || v[0] == "full" {
				f.Roles[id] = v[0]
			}
		}
	}
	if f.Name == "" || len(f.Name) > 255 {
		f.Errors["name"] = "The name field is required."
	}
	if _, err := mail.ParseAddress(f.Email); err != nil || len(f.Email) > 255 {
		f.Errors["email"] = "Enter a valid email address."
	} else if _, err := app.store.UserByEmail(f.Email); err == nil {
		f.Errors["email"] = "The email has already been taken."
	}
	if len(f.Errors) > 0 {
		app.renderUsers(w, r, http.StatusUnprocessableEntity, f)
		return
	}
	now := nowDB()
	res, err := app.store.db.Exec(`INSERT INTO users (name, email, password, is_admin, created_at, updated_at) VALUES (?, ?, ?, 0, ?, ?)`,
		f.Name, f.Email, hashPassword(randomToken(30)), now, now)
	if err != nil {
		app.errorPage(w, r, 500, "Couldn't create the user.")
		return
	}
	id, _ := res.LastInsertId()
	for mid, role := range f.Roles {
		app.store.db.Exec(`INSERT INTO machine_user (machine_id, user_id, role, created_at, updated_at) SELECT id, ?, ?, ?, ? FROM machines WHERE id = ?`, id, role, now, now, mid)
	}
	app.sendInvite(r, id, f.Email)
	http.Redirect(w, r, "/users", http.StatusSeeOther)
}

func (app *App) sendInvite(r *http.Request, userID int64, email string) {
	tok := randomFrom("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789", 64)
	app.store.db.Exec(`UPDATE users SET invite_token = ?, invite_token_expires_at = ?, updated_at = ? WHERE id = ?`,
		tok, time.Now().UTC().Add(7*24*time.Hour).Format(dbTime), nowDB(), userID)
	base := app.cfg.AppURL
	if base == "" {
		base = "http://" + r.Host
	}
	link := base + "/set-password/" + tok
	if err := app.mailer.Send(email, "You've been invited to LANHub",
		"You've been invited to LANHub.\n\nSet your password here (the link works for 7 days):\n\n"+link+"\n"); err != nil {
		sessionFrom(r).Flash("The user was created, but the invite email failed: " + err.Error())
	}
}

func (app *App) resendInvite(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if u, err := app.store.UserByID(id); err == nil && u.IsPending() {
		app.sendInvite(r, u.ID, u.Email)
	}
	http.Redirect(w, r, "/users", http.StatusSeeOther)
}

func (app *App) toggleAdmin(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if id == userFrom(r).ID {
		app.errorPage(w, r, 403, "You can't change your own admin status.")
		return
	}
	app.store.db.Exec(`UPDATE users SET is_admin = NOT is_admin, updated_at = ? WHERE id = ?`, nowDB(), id)
	http.Redirect(w, r, "/users", http.StatusSeeOther)
}

func (app *App) deleteUser(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if id == userFrom(r).ID {
		app.errorPage(w, r, 403, "You can't delete your own account.")
		return
	}
	app.store.db.Exec(`DELETE FROM users WHERE id = ?`, id)
	app.store.db.Exec(`DELETE FROM go_sessions WHERE user_id = ?`, id)
	http.Redirect(w, r, "/users", http.StatusSeeOther)
}
