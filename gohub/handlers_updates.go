package main

import (
	"context"
	"net/http"
	"os"
	"time"
)

// Settings → Updates.

type updatesData struct {
	Current, ImageVersion       string
	Staged                      bool
	MediaVersion, MediaError    string
	MediaUpdatable              bool
	Latest, Published           string
	CheckedAt, CheckError       string
	Notes, LastResult           string
	HubFailed, MediaFailed      string
	AutoCheck, Email            bool
	MailConfigured, SchedulerOn bool
	ActiveTransfers             int
	Install                     string // ?install= from the update email
	Status                      updateStatus
}

func (app *App) updatesView(r *http.Request) *updatesData {
	s := app.store.Setting
	d := &updatesData{Current: hubVersion, ImageVersion: imageVersion(), Staged: os.Getenv("LANHUB_STAGED") == "1",
		Latest: app.upd.latest(), Published: s("update_latest"), CheckedAt: s("update_checked_at"), CheckError: s("update_check_error"),
		Notes: s("update_notes"), LastResult: s("update_last_result"), AutoCheck: s("update_auto_check") != "0", Email: s("update_email") != "0",
		MailConfigured: app.mailer.Configured(), SchedulerOn: app.cfg.Scheduler, MediaUpdatable: app.upd.mediaDir != "",
		Install: r.URL.Query().Get("install"), Status: app.upd.status()}
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()
	if app.mediaEnabled() {
		if v, err := app.mediaVersion(ctx); err != nil {
			d.MediaError = "not answering"
		} else {
			d.MediaVersion = v
		}
	}
	if m := readStaged(app.upd.hubDir); m != nil && m.Failed {
		d.HubFailed = m.Version + ": " + m.FailedReason
	}
	if app.upd.mediaDir != "" {
		if m := readStaged(app.upd.mediaDir); m != nil && m.Failed {
			d.MediaFailed = m.Version + ": " + m.FailedReason
		}
	}
	app.store.db.QueryRow(`SELECT count(*) FROM relay_transfers WHERE status IN ('queued', 'downloading', 'uploading')`).Scan(&d.ActiveTransfers)
	return d
}

func (app *App) updatesCheck(w http.ResponseWriter, r *http.Request) {
	s := sessionFrom(r)
	v, err := app.upd.check(r.Context(), false)
	switch {
	case err != nil:
		s.Flash("Couldn't check for updates: " + err.Error())
	case compareVersions(v, hubVersion) > 0:
		s.Flash("LANHub " + v + " is available.")
	default:
		s.Flash("LANHub is up to date (" + hubVersion + ").")
	}
	http.Redirect(w, r, "/settings?tab=updates", http.StatusSeeOther)
}

func (app *App) updatesInstall(w http.ResponseWriter, r *http.Request) {
	s := sessionFrom(r)
	v := r.PostFormValue("version")
	if v == "" || v != app.upd.latest() {
		s.Flash("That update isn't available any more. Check for updates and try again.")
	} else if err := app.upd.install(v); err != nil {
		s.Flash(err.Error())
	} else {
		app.setSetting("update_started_by", userFrom(r).Email)
	}
	http.Redirect(w, r, "/settings?tab=updates", http.StatusSeeOther)
}

func (app *App) updatesOptions(w http.ResponseWriter, r *http.Request) {
	onOff := func(k string) string {
		if r.PostFormValue(k) != "" {
			return "1"
		}
		return "0"
	}
	app.setSetting("update_auto_check", onOff("auto_check"))
	app.setSetting("update_email", onOff("email"))
	sessionFrom(r).Flash("Update settings saved.")
	http.Redirect(w, r, "/settings?tab=updates", http.StatusSeeOther)
}

func (app *App) updatesStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, app.upd.status())
}
