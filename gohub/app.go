package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

//go:embed web/templates/*.html
var templateFS embed.FS

//go:embed web/static
var staticFS embed.FS

type App struct {
	cfg     Config
	store   *Store
	pages   map[string]*template.Template
	limiter *limiter
	relays  *RelayManager
	mailer  *Mailer

	netMu      sync.Mutex
	netSamples map[int64]netSample // last /api/netstats reading per machine

	media mediaConfig
	plans planCache
	upd   *updater

	agentBuilds *agentBundle // agent builds to install and push; nil if built without
}

type netSample struct {
	sent, recv int64
	at         time.Time
}

func newApp(cfg Config, store *Store) (*App, error) {
	app := &App{cfg: cfg, store: store, limiter: newLimiter(), netSamples: map[int64]netSample{},
		media: mediaConfig{url: cfg.MediaURL, secret: cfg.MediaSecret, sourceBase: cfg.MediaSourceBase},
		plans: planCache{items: map[string]cachedPlan{}}}
	app.mailer = &Mailer{store: store, appName: cfg.AppName}
	app.agentBuilds = loadAgentBundle(agentBuildsFS)
	app.upd = newUpdater(app)
	app.relays = newRelayManager(app)
	if err := app.loadTemplates(); err != nil {
		return nil, err
	}
	return app, nil
}

// ---- templates ----

var pageFiles = map[string][]string{
	"login":       {"guest.html", "login.html"},
	"verify2fa":   {"guest.html", "verify2fa.html"},
	"setpassword": {"guest.html", "setpassword.html"},
	"dashboard":   {"layout.html", "dashboard.html"},
	"explorer":    {"layout.html", "explorer.html"},
	"activity":    {"layout.html", "activity.html"},
	"agents":      {"layout.html", "agents.html"},
	"agentlog":    {"layout.html", "agentlog.html"},
	"syncrules":   {"layout.html", "syncrules.html"},
	"users":       {"layout.html", "users.html"},
	"profile":     {"layout.html", "profile.html"},
	"settings":    {"layout.html", "settings.html"},
	"setup":       {"guest.html", "setup.html"},
	"forgot":      {"guest.html", "forgot.html"},
	"reset":       {"guest.html", "reset.html"},
	"error":       {"guest.html", "error.html"},
}

func (app *App) loadTemplates() error {
	app.pages = map[string]*template.Template{}
	for name, files := range pageFiles {
		paths := make([]string, len(files))
		for i, f := range files {
			paths[i] = "web/templates/" + f
		}
		t, err := template.New(files[0]).Funcs(templateFuncs).ParseFS(templateFS, append(paths, "web/templates/partials.html")...)
		if err != nil {
			return fmt.Errorf("template %s: %w", name, err)
		}
		app.pages[name] = t
	}
	return nil
}

type pageData struct {
	Title   string
	Page    string
	User    *User
	CSRF    string
	Flash   string
	AppName string
	Version string
	Data    any
	// UpdateVersion is a newer LANHub, shown to admins as a banner.
	UpdateVersion string
}

func (app *App) render(w http.ResponseWriter, r *http.Request, status int, page, title string, data any) {
	s := sessionFrom(r)
	pd := pageData{Title: title, Page: page, User: userFrom(r), AppName: app.cfg.AppName, Version: app.cfg.Version, Data: data}
	if pd.User != nil && pd.User.IsAdmin && page != "settings" {
		if st := app.upd.status(); st.Phase != "downloading" && st.Phase != "restarting" {
			pd.UpdateVersion = app.upd.latest()
		}
	}
	if s != nil {
		pd.CSRF = s.CSRF
		pd.Flash = s.TakeFlash()
		app.saveSession(s)
	}
	var buf bytes.Buffer
	if err := app.pages[page].Execute(&buf, pd); err != nil {
		log.Printf("render %s: %v", page, err)
		http.Error(w, "Something went wrong showing this page.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	w.Write(buf.Bytes())
}

func (app *App) errorPage(w http.ResponseWriter, r *http.Request, status int, msg string) {
	if status == http.StatusForbidden || status == http.StatusNotFound {
		who := "guest"
		if u := userFrom(r); u != nil {
			who = u.Email
		}
		log.Printf("%d for %s on %s %s: %s", status, who, r.Method, r.URL.Path, msg)
	}
	if wantsJSON(r) {
		writeJSON(w, status, map[string]string{"message": msg})
		return
	}
	app.render(w, r, status, "error", fmt.Sprint(status), map[string]any{"Status": status, "Message": msg})
}

func wantsJSON(r *http.Request) bool {
	return strings.HasPrefix(r.URL.Path, "/api/") || strings.Contains(r.Header.Get("Accept"), "application/json")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func readJSON(r *http.Request, v any) error {
	return json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(v)
}

// ---- routing and middleware ----

type handler func(w http.ResponseWriter, r *http.Request)

func (app *App) routes() http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(staticFS, "web/static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", cacheForever(http.FileServerFS(static))))
	mux.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, static, "favicon.ico")
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	// Agent installs: public, like the repo they come from.
	mux.HandleFunc("GET /agent/install.sh", app.agentInstallScript)
	mux.HandleFunc("GET /agent/install.ps1", app.agentInstallScript)
	mux.HandleFunc("GET /agent/download/{name}", app.agentDownload)

	guest := func(h handler) http.Handler { return app.withSession(app.csrf(h)) }
	auth := func(h handler) http.Handler { return app.withSession(app.requireAuth(app.csrf(h))) }
	admin := func(h handler) http.Handler { return app.withSession(app.requireAuth(app.requireAdmin(app.csrf(h)))) }

	mux.Handle("GET /{$}", guest(app.home))
	mux.Handle("GET /login", guest(app.showLogin))
	mux.Handle("POST /login", guest(app.doLogin))
	mux.Handle("GET /verify-2fa", guest(app.showVerify))
	mux.Handle("POST /verify-2fa", guest(app.doVerify))
	mux.Handle("POST /verify-2fa/email", guest(app.doVerifyEmail))
	mux.Handle("GET /set-password/{token}", guest(app.showSetPassword))
	mux.Handle("POST /set-password/{token}", guest(app.doSetPassword))
	mux.Handle("POST /logout", auth(app.doLogout))

	mux.Handle("GET /dashboard", auth(app.dashboard))
	mux.Handle("GET /machines", auth(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/dashboard", http.StatusFound) }))
	mux.Handle("GET /api/dashboard/status", auth(app.apiDashboardStatus))
	mux.Handle("GET /api/dashboard/netstats", auth(app.apiNetstats))
	mux.Handle("POST /machines", auth(app.saveMachine))
	mux.Handle("POST /machines/{id}", auth(app.saveMachine))
	mux.Handle("POST /machines/{id}/delete", auth(app.deleteMachine))

	mux.Handle("GET /explorer", auth(app.explorer))
	mux.Handle("GET /api/machines", auth(app.apiMachines))
	mux.Handle("GET /api/machines/{id}/list", auth(app.apiList))
	mux.Handle("POST /api/machines/{id}/mkdir", auth(app.apiMkdir))
	mux.Handle("POST /api/machines/{id}/rename", auth(app.apiRename))
	mux.Handle("POST /api/machines/{id}/delete", auth(app.apiDelete))
	mux.Handle("POST /api/machines/{id}/upload", auth(app.apiUpload))
	mux.Handle("GET /api/machines/{id}/search", auth(app.apiSearch))
	mux.Handle("GET /api/search", auth(app.apiGlobalSearch))
	mux.Handle("POST /api/drop", auth(app.apiDrop))
	mux.Handle("GET /api/transfers", auth(app.apiTransfers))
	mux.Handle("GET /machines/{id}/download", auth(app.download))
	mux.Handle("GET /machines/{id}/preview", auth(app.preview))
	mux.Handle("GET /machines/{id}/media", auth(app.mediaFile))
	mux.Handle("GET /api/machines/{id}/media-info", auth(app.apiMediaInfo))
	mux.Handle("GET /machines/{id}/play", auth(app.play))
	// No session: a signed, expiring, single-file link for LANHub Media.
	mux.HandleFunc("GET /media-source", app.mediaSource)

	mux.Handle("GET /activity", auth(app.activity))
	mux.Handle("GET /agents", auth(app.agents))
	mux.Handle("GET /agents/{id}/log", auth(app.agentLog))
	mux.Handle("GET /api/agents/{id}/status", auth(app.apiAgentStatus))
	mux.Handle("POST /api/agents/{id}/update", admin(app.apiAgentUpdate))
	mux.Handle("GET /sync-rules", auth(app.syncRules))
	mux.Handle("POST /sync-rules", auth(app.saveSyncRule))
	mux.Handle("POST /sync-rules/{id}", auth(app.saveSyncRule))
	mux.Handle("POST /sync-rules/{id}/toggle", auth(app.toggleSyncRule))
	mux.Handle("POST /sync-rules/{id}/delete", auth(app.deleteSyncRule))

	mux.Handle("GET /users", admin(app.users))
	mux.Handle("POST /users", admin(app.inviteUser))
	mux.Handle("POST /users/{id}/resend", admin(app.resendInvite))
	mux.Handle("POST /users/{id}/admin", admin(app.toggleAdmin))
	mux.Handle("POST /users/{id}/delete", admin(app.deleteUser))

	mux.Handle("GET /profile", auth(app.profile))
	mux.Handle("POST /profile", auth(app.updateProfile))
	mux.Handle("POST /profile/password", auth(app.updatePassword))
	mux.Handle("POST /profile/authenticator/{action}", auth(app.authenticatorAction))
	mux.Handle("POST /profile/delete", auth(app.deleteAccount))
	mux.Handle("GET /settings", admin(app.settings))
	mux.Handle("POST /settings/updates/check", admin(app.updatesCheck))
	mux.Handle("POST /settings/updates/install", admin(app.updatesInstall))
	mux.Handle("POST /settings/updates/options", admin(app.updatesOptions))
	mux.Handle("GET /api/updates/status", admin(app.updatesStatus))
	mux.Handle("POST /settings", admin(app.saveSettings))
	mux.Handle("GET /setup", guest(app.showSetup))
	mux.Handle("POST /setup", guest(app.doSetup))
	mux.Handle("GET /forgot-password", guest(app.showForgot))
	mux.Handle("POST /forgot-password", guest(app.doForgot))
	mux.Handle("GET /reset-password/{token}", guest(app.showReset))
	mux.Handle("POST /reset-password/{token}", guest(app.doReset))

	return securityHeaders(mux)
}

func cacheForever(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		h.ServeHTTP(w, r)
	})
}

func securityHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hd := w.Header()
		hd.Set("X-Frame-Options", "SAMEORIGIN")
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("Referrer-Policy", "same-origin")
		h.ServeHTTP(w, r)
	})
}

func (app *App) withSession(next handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := app.loadSession(w, r)
		r = withValue(r, ctxSession, s)
		if s.UserID > 0 {
			if u, err := app.store.UserByID(s.UserID); err == nil {
				r = withValue(r, ctxUser, u)
			}
		}
		next(w, r)
		app.saveSession(s)
	})
}

func (app *App) requireAuth(next handler) handler {
	return func(w http.ResponseWriter, r *http.Request) {
		if userFrom(r) == nil {
			if wantsJSON(r) {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "Your session has ended. Log in again."})
				return
			}
			// Come back here after logging in (e.g. an email's Update button).
			if s := sessionFrom(r); s != nil && r.Method == http.MethodGet {
				s.Set("url.intended", r.URL.RequestURI())
			}
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		next(w, r)
	}
}

func (app *App) requireAdmin(next handler) handler {
	return func(w http.ResponseWriter, r *http.Request) {
		if !userFrom(r).IsAdmin {
			app.errorPage(w, r, http.StatusForbidden, "Only admins can do that.")
			return
		}
		next(w, r)
	}
}

// csrf checks every state-changing request against the session's token.
func (app *App) csrf(next handler) handler {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			// Multipart uploads carry the token in the header; don't parse
			// a large body just to look for a form field.
			if !validCSRF(sessionFrom(r), r) {
				app.errorPage(w, r, 419, "This page expired. Reload it and try again.")
				return
			}
		}
		next(w, r)
	}
}
