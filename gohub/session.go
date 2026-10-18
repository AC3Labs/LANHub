package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Server-side sessions in go_sessions. The cookie holds a random token;
// the table stores only its SHA-256, so a copy of the database can't be
// used to hijack a session.

const (
	sessionCookie   = "lanhub_go_session"
	sessionIdle     = 120 * time.Minute    // SESSION_LIFETIME in the PHP hub
	sessionRemember = 30 * 24 * time.Hour  // "Remember me"
)

type Session struct {
	ID       string // sha256 of the cookie token
	UserID   int64
	CSRF     string
	Remember bool
	Data     map[string]any
	dirty    bool
}

func (s *Session) Get(key string) any { return s.Data[key] }

func (s *Session) Set(key string, v any) {
	if s.Data == nil {
		s.Data = map[string]any{}
	}
	s.Data[key] = v
	s.dirty = true
}

func (s *Session) Forget(keys ...string) {
	for _, k := range keys {
		delete(s.Data, k)
	}
	s.dirty = true
}

// Flash is shown once on the next page.
func (s *Session) Flash(msg string) { s.Set("_flash", msg) }

func (s *Session) TakeFlash() string {
	v, _ := s.Data["_flash"].(string)
	if v != "" {
		s.Forget("_flash")
	}
	return v
}

func randomToken(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func hashToken(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}

type ctxKey int

const (
	ctxSession ctxKey = iota
	ctxUser
)

func sessionFrom(r *http.Request) *Session {
	s, _ := r.Context().Value(ctxSession).(*Session)
	return s
}

func userFrom(r *http.Request) *User {
	u, _ := r.Context().Value(ctxUser).(*User)
	return u
}

// loadSession finds the request's session or starts a new one (written on
// the way out only if something was stored in it).
func (app *App) loadSession(w http.ResponseWriter, r *http.Request) *Session {
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		id := hashToken(c.Value)
		var userID sql.NullInt64
		var data, csrf string
		var expires int64
		var remember bool
		err := app.store.db.QueryRow(`SELECT user_id, data, csrf, expires_at, remember FROM go_sessions WHERE id = ?`, id).
			Scan(&userID, &data, &csrf, &expires, &remember)
		if err == nil && expires > time.Now().Unix() {
			s := &Session{ID: id, UserID: userID.Int64, CSRF: csrf, Remember: remember, Data: map[string]any{}}
			json.Unmarshal([]byte(data), &s.Data)
			app.touchSession(w, s, c.Value)
			return s
		}
		if err == nil {
			app.store.db.Exec(`DELETE FROM go_sessions WHERE id = ?`, id)
		}
	}
	return app.newSession(w, false)
}

func (app *App) newSession(w http.ResponseWriter, remember bool) *Session {
	tok := randomToken(32)
	s := &Session{ID: hashToken(tok), CSRF: randomToken(24), Remember: remember, Data: map[string]any{}, dirty: true}
	app.setSessionCookie(w, tok, remember)
	return s
}

func (app *App) setSessionCookie(w http.ResponseWriter, tok string, remember bool) {
	c := &http.Cookie{Name: sessionCookie, Value: tok, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode}
	if remember {
		c.MaxAge = int(sessionRemember.Seconds())
	}
	http.SetCookie(w, c)
}

// touchSession slides the idle expiry forward (at most once a minute).
func (app *App) touchSession(w http.ResponseWriter, s *Session, tok string) {
	life := sessionIdle
	if s.Remember {
		life = sessionRemember
	}
	app.store.db.Exec(`UPDATE go_sessions SET expires_at = ? WHERE id = ? AND expires_at < ?`,
		time.Now().Add(life).Unix(), s.ID, time.Now().Add(life-time.Minute).Unix())
}

func (app *App) saveSession(s *Session) {
	if s == nil || !s.dirty {
		return
	}
	life := sessionIdle
	if s.Remember {
		life = sessionRemember
	}
	data, _ := json.Marshal(s.Data)
	var uid any
	if s.UserID > 0 {
		uid = s.UserID
	}
	app.store.db.Exec(`INSERT INTO go_sessions (id, user_id, data, csrf, expires_at, remember) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET user_id = excluded.user_id, data = excluded.data, csrf = excluded.csrf,
		expires_at = excluded.expires_at, remember = excluded.remember`,
		s.ID, uid, string(data), s.CSRF, time.Now().Add(life).Unix(), s.Remember)
	s.dirty = false
}

// login replaces the session (new token, new CSRF) so a token fixed before
// login can't be reused after it.
func (app *App) login(w http.ResponseWriter, r *http.Request, old *Session, userID int64, remember bool) *Session {
	app.store.db.Exec(`DELETE FROM go_sessions WHERE id = ?`, old.ID)
	old.dirty = false // don't let the request's deferred save bring it back
	s := app.newSession(w, remember)
	s.UserID = userID
	s.Remember = remember
	app.saveSession(s)
	return s
}

func (app *App) logout(w http.ResponseWriter, s *Session) {
	app.store.db.Exec(`DELETE FROM go_sessions WHERE id = ?`, s.ID)
	s.dirty = false
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

func (app *App) pruneSessions() {
	app.store.db.Exec(`DELETE FROM go_sessions WHERE expires_at < ?`, time.Now().Unix())
}

func validCSRF(s *Session, r *http.Request) bool {
	tok := r.Header.Get("X-CSRF-Token")
	// Only plain form posts carry it as a field; never parse a multipart
	// upload body just to look for one.
	if tok == "" && strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
		tok = r.PostFormValue("_token")
	}
	return s != nil && tok != "" && subtle.ConstantTimeCompare([]byte(tok), []byte(s.CSRF)) == 1
}

func withValue(r *http.Request, k ctxKey, v any) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), k, v))
}

// ---- rate limiting (Laravel RateLimiter: 5 tries, then locked for 60 s) ----

type limiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

func newLimiter() *limiter { return &limiter{hits: map[string][]time.Time{}} }

const (
	maxAttempts  = 5
	attemptDecay = 60 * time.Second
)

func (l *limiter) recent(key string) []time.Time {
	cut := time.Now().Add(-attemptDecay)
	var keep []time.Time
	for _, t := range l.hits[key] {
		if t.After(cut) {
			keep = append(keep, t)
		}
	}
	l.hits[key] = keep
	return keep
}

// TooMany reports whether key is locked out and for how many seconds.
func (l *limiter) TooMany(key string) (bool, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	h := l.recent(key)
	if len(h) < maxAttempts {
		return false, 0
	}
	wait := int(time.Until(h[0].Add(attemptDecay)).Seconds()) + 1
	return true, wait
}

func (l *limiter) Hit(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.hits[key] = append(l.recent(key), time.Now())
}

func (l *limiter) Clear(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.hits, key)
}
