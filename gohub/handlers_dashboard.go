package main

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type machineForm struct {
	ID       int64
	Name     string
	Host     string
	Port     int
	OS       string
	Color    string
	UseTLS   bool
	Notes    string
	Shares   map[int64]string
	Errors   map[string]string
	HasToken bool
}

type dashboardData struct {
	Machines  []*Machine
	OtherUser []*User
	Form      *machineForm
	Edit      []machineEdit // what the edit form is pre-filled with, per machine
	// The hub can hand out agents: show install commands with the token.
	CanInstallAgents bool
}

type machineEdit struct {
	ID      int64            `json:"id"`
	Name    string           `json:"name"`
	Host    string           `json:"host"`
	Port    int              `json:"port"`
	OS      string           `json:"os"`
	Color   string           `json:"color"`
	UseTLS  bool             `json:"use_tls"`
	Notes   string           `json:"notes"`
	Shares  map[string]string `json:"shares"`
	CanEdit bool             `json:"can_edit"`
}

func (app *App) dashboard(w http.ResponseWriter, r *http.Request) {
	app.renderDashboard(w, r, http.StatusOK, nil)
}

func (app *App) renderDashboard(w http.ResponseWriter, r *http.Request, status int, form *machineForm) {
	u := userFrom(r)
	machines, err := app.store.AccessibleMachines(u.ID, "sort")
	if err != nil {
		app.errorPage(w, r, 500, "Couldn't load machines.")
		return
	}
	users, _ := app.store.Users()
	var others []*User
	for _, x := range users {
		if x.ID != u.ID {
			others = append(others, x)
		}
	}
	edits := make([]machineEdit, 0, len(machines))
	for _, m := range machines {
		e := machineEdit{ID: m.ID, Name: m.Name, Host: m.Host, Port: m.Port, OS: m.OS, Color: string(safeColor(m.Color)), UseTLS: m.UseTLS,
			Notes: m.Notes, Shares: map[string]string{}, CanEdit: m.Role == "full"}
		if rows, err := app.store.db.Query(`SELECT user_id, role FROM machine_user WHERE machine_id = ?`, m.ID); err == nil {
			for rows.Next() {
				var uid int64
				var role string
				if rows.Scan(&uid, &role) == nil {
					e.Shares[strconv.FormatInt(uid, 10)] = role
				}
			}
			rows.Close()
		}
		edits = append(edits, e)
	}
	app.render(w, r, status, "dashboard", "Dashboard", dashboardData{Machines: machines, OtherUser: others, Form: form, Edit: edits, CanInstallAgents: app.agentBuilds != nil})
}

type machineStatus struct {
	Hostname string  `json:"hostname"`
	Status   string  `json:"status"`
	Drives   []Drive `json:"drives"`
	Error    string  `json:"error"`
}

// forEachMachine runs fn for every machine at once and waits for all.
func forEachMachine(machines []*Machine, fn func(m *Machine)) {
	var wg sync.WaitGroup
	for _, m := range machines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fn(m)
		}()
	}
	wg.Wait()
}

func (app *App) fetchStatus(ctx context.Context, m *Machine) machineStatus {
	ag, err := app.store.Agent(m)
	if err == nil {
		var health AgentHealth
		if health, err = ag.Health(ctx); err == nil {
			var drives []Drive
			if drives, err = ag.Drives(ctx); err == nil {
				app.store.SetMachineStatus(m.ID, true)
				return machineStatus{Hostname: health.Hostname, Status: "online", Drives: drives}
			}
		}
	}
	app.store.SetMachineStatus(m.ID, false)
	return machineStatus{Status: "offline", Error: err.Error(), Drives: []Drive{}}
}

// apiDashboardStatus checks every accessible machine in parallel (the PHP
// hub does them one after another), or just ?machine=ID for "Ping".
func (app *App) apiDashboardStatus(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	machines, _ := app.store.AccessibleMachines(u.ID, "sort")
	if id, err := strconv.ParseInt(r.URL.Query().Get("machine"), 10, 64); err == nil {
		var only []*Machine
		for _, m := range machines {
			if m.ID == id {
				only = append(only, m)
			}
		}
		machines = only
	}
	var mu sync.Mutex
	out := map[int64]machineStatus{}
	forEachMachine(machines, func(m *Machine) {
		st := app.fetchStatus(r.Context(), m)
		mu.Lock()
		out[m.ID] = st
		mu.Unlock()
	})
	writeJSON(w, http.StatusOK, out)
}

type speed struct {
	Up   float64 `json:"up"`
	Down float64 `json:"down"`
	Avg  float64 `json:"avg"`
}

// apiNetstats turns two samples of each agent's cumulative byte counters
// into a current speed, as Dashboard\Index::sampleCurrentSpeed does.
func (app *App) apiNetstats(w http.ResponseWriter, r *http.Request) {
	machines, _ := app.store.AccessibleMachines(userFrom(r).ID, "sort")
	var mu sync.Mutex
	out := map[int64]speed{}
	forEachMachine(machines, func(m *Machine) {
		sp := app.sampleSpeed(r.Context(), m)
		mu.Lock()
		out[m.ID] = sp
		mu.Unlock()
	})
	writeJSON(w, http.StatusOK, out)
}

func (app *App) sampleSpeed(ctx context.Context, m *Machine) speed {
	ag, err := app.store.Agent(m)
	if err != nil {
		return speed{}
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	sent, recv, err := ag.Netstats(ctx)
	if err != nil {
		return speed{}
	}
	now := time.Now()
	app.netMu.Lock()
	prev, ok := app.netSamples[m.ID]
	app.netSamples[m.ID] = netSample{sent: sent, recv: recv, at: now}
	app.netMu.Unlock()
	if !ok || now.Sub(prev.at) > 2*time.Minute {
		return speed{}
	}
	elapsed := now.Sub(prev.at).Seconds()
	ds, dr := sent-prev.sent, recv-prev.recv
	// Counters reset when an agent or its machine restarts; don't show
	// that as a spike.
	if elapsed < 0.5 || ds < 0 || dr < 0 {
		return speed{}
	}
	up, down := float64(ds)/elapsed, float64(dr)/elapsed
	return speed{Up: up, Down: down, Avg: (up + down) / 2}
}

// ---- add / edit / delete machine ----

func (app *App) saveMachine(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	var editing *Machine
	if idStr := r.PathValue("id"); idStr != "" {
		id, _ := strconv.ParseInt(idStr, 10, 64)
		m, err := app.store.Machine(id)
		if err != nil {
			app.errorPage(w, r, 404, "That machine doesn't exist.")
			return
		}
		if !app.store.CanAccess(u.ID, m.ID, "full") {
			app.errorPage(w, r, 403, "You need full access to edit this machine.")
			return
		}
		editing = m
	}

	port, portErr := strconv.Atoi(strings.TrimSpace(r.PostFormValue("port")))
	f := &machineForm{
		Name:   strings.TrimSpace(r.PostFormValue("name")),
		Host:   strings.TrimSpace(r.PostFormValue("host")),
		Port:   port,
		OS:     r.PostFormValue("os"),
		Color:  r.PostFormValue("color"),
		UseTLS: r.PostFormValue("use_tls") != "",
		Notes:  strings.TrimSpace(r.PostFormValue("notes")),
		Shares: map[int64]string{},
		Errors: map[string]string{},
	}
	if editing != nil {
		f.ID = editing.ID
	}
	token := strings.TrimSpace(r.PostFormValue("agent_token"))
	r.ParseForm()
	for k, v := range r.PostForm {
		if strings.HasPrefix(k, "share_") {
			if id, err := strconv.ParseInt(strings.TrimPrefix(k, "share_"), 10, 64); err == nil && len(v) > 0 {
				if v[0] == "read_only" || v[0] == "full" || v[0] == "none" {
					f.Shares[id] = v[0]
				}
			}
		}
	}

	switch {
	case f.Name == "" || len(f.Name) > 255:
		f.Errors["name"] = "The name field is required (255 characters at most)."
	}
	if f.Host == "" || len(f.Host) > 255 {
		f.Errors["host"] = "The host field is required."
	}
	if portErr != nil || port < 1 || port > 65535 {
		f.Errors["port"] = "The port must be between 1 and 65535."
	}
	if f.OS != "windows" && f.OS != "linux" && f.OS != "macos" && f.OS != "other" {
		f.OS = "linux"
	}
	if string(safeColor(f.Color)) != f.Color {
		f.Color = "#ac8544"
	}
	if len(f.Notes) > 1000 {
		f.Errors["notes"] = "Notes can be 1000 characters at most."
	}
	if editing == nil && token == "" {
		f.Errors["agent_token"] = "A token is required when registering a new machine."
	}
	if len(token) > 255 {
		f.Errors["agent_token"] = "The token can be 255 characters at most."
	}
	if net.ParseIP(f.Host) == nil && strings.ContainsAny(f.Host, " /\\@") {
		f.Errors["host"] = "Enter a host name or IP address."
	}
	if len(f.Errors) > 0 {
		app.renderDashboard(w, r, http.StatusUnprocessableEntity, f)
		return
	}

	tx, err := app.store.db.Begin()
	if err != nil {
		app.errorPage(w, r, 500, "Couldn't save the machine.")
		return
	}
	defer tx.Rollback()
	now := nowDB()
	machineID := f.ID
	if editing != nil {
		_, err = tx.Exec(`UPDATE machines SET name=?, host=?, port=?, os=?, color=?, use_tls=?, notes=?, updated_at=? WHERE id=?`,
			f.Name, f.Host, f.Port, f.OS, f.Color, f.UseTLS, nullable(f.Notes), now, f.ID)
		if err == nil && token != "" {
			enc, encErr := app.store.enc.EncryptString(token)
			if encErr != nil {
				err = encErr
			} else {
				_, err = tx.Exec(`UPDATE machines SET agent_token=? WHERE id=?`, enc, f.ID)
			}
		}
	} else {
		enc, encErr := app.store.enc.EncryptString(token)
		if encErr != nil {
			err = encErr
		} else {
			res, insErr := tx.Exec(`INSERT INTO machines (name, host, port, os, agent_token, color, use_tls, notes, status, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'unknown', ?, ?)`, f.Name, f.Host, f.Port, f.OS, enc, f.Color, f.UseTLS, nullable(f.Notes), now, now)
			err = insErr
			if err == nil {
				machineID, _ = res.LastInsertId()
			}
		}
	}
	// Sharing: the person saving always keeps full access; others get what
	// was picked (same as $machine->users()->sync()).
	if err == nil {
		_, err = tx.Exec(`DELETE FROM machine_user WHERE machine_id = ?`, machineID)
	}
	if err == nil {
		_, err = tx.Exec(`INSERT INTO machine_user (machine_id, user_id, role, created_at, updated_at) VALUES (?, ?, 'full', ?, ?)`, machineID, u.ID, now, now)
	}
	for uid, role := range f.Shares {
		if err != nil || uid == u.ID || role == "none" {
			continue
		}
		_, err = tx.Exec(`INSERT INTO machine_user (machine_id, user_id, role, created_at, updated_at) SELECT ?, id, ?, ?, ? FROM users WHERE id = ?`, machineID, role, now, now, uid)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		app.errorPage(w, r, 500, "Couldn't save the machine: "+err.Error())
		return
	}
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}


func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (app *App) deleteMachine(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if !app.store.CanAccess(userFrom(r).ID, id, "full") {
		app.errorPage(w, r, 403, "You need full access to delete this machine.")
		return
	}
	app.store.db.Exec(`DELETE FROM machines WHERE id = ?`, id)
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}
