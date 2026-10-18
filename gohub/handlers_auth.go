package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/mail"
	"strings"
	"time"
)

func (app *App) home(w http.ResponseWriter, r *http.Request) {
	if userFrom(r) != nil {
		http.Redirect(w, r, "/dashboard", http.StatusFound)
		return
	}
	http.Redirect(w, r, "/login", http.StatusFound)
}

type loginForm struct {
	Email    string
	Remember bool
	Error    string
}

func (app *App) showLogin(w http.ResponseWriter, r *http.Request) {
	if userFrom(r) != nil {
		http.Redirect(w, r, "/dashboard", http.StatusFound)
		return
	}
	// A fresh install has nobody to log in as: send them to setup.
	if app.store.UserCount() == 0 {
		http.Redirect(w, r, "/setup", http.StatusFound)
		return
	}
	app.render(w, r, http.StatusOK, "login", "Log in", loginForm{})
}

// doLogin checks the password without logging in: every login then needs
// a second step (authenticator code, or an emailed code), as in the PHP hub.
func (app *App) doLogin(w http.ResponseWriter, r *http.Request) {
	s := sessionFrom(r)
	email := strings.ToLower(strings.TrimSpace(r.PostFormValue("email")))
	password := r.PostFormValue("password")
	form := loginForm{Email: email, Remember: r.PostFormValue("remember") != ""}
	fail := func(msg string) {
		form.Error = msg
		app.render(w, r, http.StatusUnprocessableEntity, "login", "Log in", form)
	}

	if _, err := mail.ParseAddress(email); err != nil || password == "" {
		fail("Enter your email address and password.")
		return
	}
	key := "login:" + email + "|" + clientIP(r)
	if locked, secs := app.limiter.TooMany(key); locked {
		fail(fmt.Sprintf("Too many login attempts. Please try again in %d seconds.", secs))
		return
	}
	u, err := app.store.UserByEmail(email)
	if err != nil || !checkPassword(u.PasswordHash, password) {
		app.limiter.Hit(key)
		fail("These credentials do not match our records.")
		return
	}
	app.limiter.Clear(key)
	if u.IsPending() {
		fail("This account has not been activated yet — check your email for a setup link.")
		return
	}

	method := "email"
	if u.HasAuthenticator() {
		method = "app"
	} else if err := app.sendLoginCode(u); err != nil {
		fail("Couldn't send your login code: " + err.Error())
		return
	}
	s.Set("2fa.user_id", u.ID)
	s.Set("2fa.method", method)
	s.Set("2fa.remember", form.Remember)
	http.Redirect(w, r, "/verify-2fa", http.StatusFound)
}

func (app *App) sendLoginCode(u *User) error {
	code := randomFrom("ABCDEFGHJKMNPQRSTUVWXYZ23456789!@#$%&*-_+?", 10)
	_, err := app.store.db.Exec(`UPDATE users SET two_factor_code = ?, two_factor_expires_at = ?, updated_at = ? WHERE id = ?`,
		hashPassword(code), time.Now().UTC().Add(10*time.Minute).Format(dbTime), nowDB(), u.ID)
	if err != nil {
		return err
	}
	return app.mailer.Send(u.Email, "Your LANHub login code",
		"Your login code is:\n\n    "+code+"\n\nThis code expires in 10 minutes. If this wasn't you, you can safely ignore this email — your password has not been changed.\n")
}

type verifyData struct {
	Method         string
	MailConfigured bool
	Error          string
}

func pendingUserID(s *Session) int64 {
	switch v := s.Get("2fa.user_id").(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case json.Number:
		n, _ := v.Int64()
		return n
	}
	return 0
}

func (app *App) showVerify(w http.ResponseWriter, r *http.Request) {
	s := sessionFrom(r)
	if pendingUserID(s) == 0 {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	method, _ := s.Get("2fa.method").(string)
	app.render(w, r, http.StatusOK, "verify2fa", "Verify", verifyData{Method: method, MailConfigured: app.mailer.Configured()})
}

func (app *App) doVerify(w http.ResponseWriter, r *http.Request) {
	s := sessionFrom(r)
	uid := pendingUserID(s)
	if uid == 0 {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	method, _ := s.Get("2fa.method").(string)
	data := verifyData{Method: method, MailConfigured: app.mailer.Configured()}
	fail := func(msg string) {
		data.Error = msg
		app.render(w, r, http.StatusUnprocessableEntity, "verify2fa", "Verify", data)
	}
	code := strings.TrimSpace(r.PostFormValue("code"))
	if code == "" {
		fail("Enter the code.")
		return
	}
	key := fmt.Sprintf("two-factor:%d", uid)
	if locked, secs := app.limiter.TooMany(key); locked {
		fail(fmt.Sprintf("Too many attempts. Try again in %d seconds.", secs))
		return
	}
	u, err := app.store.UserByID(uid)
	valid := err == nil && ((method == "app" && app.verifyAuthenticator(u, code)) || (method != "app" && verifyEmailCode(u, code)))
	if !valid {
		app.limiter.Hit(key)
		fail("That code is incorrect or has expired.")
		return
	}
	app.limiter.Clear(key)
	app.store.db.Exec(`UPDATE users SET two_factor_code = NULL, two_factor_expires_at = NULL WHERE id = ?`, u.ID)
	remember, _ := s.Get("2fa.remember").(bool)
	app.login(w, r, s, u.ID, remember)
	http.Redirect(w, r, "/dashboard", http.StatusFound)
}

// verifyAuthenticator mirrors User::verifyAuthenticatorCode(): a TOTP code
// (stores the step so it can't be replayed), or a single-use recovery code.
func (app *App) verifyAuthenticator(u *User, code string) bool {
	if !u.HasAuthenticator() {
		return false
	}
	secret, err := app.store.enc.DecryptString(u.TwoFactorSecretEnc.String)
	if err != nil {
		return false
	}
	if step := verifyTOTP(secret, code, u.TwoFactorLastUsedStep, time.Now()); step > 0 {
		app.store.db.Exec(`UPDATE users SET two_factor_last_used_step = ?, updated_at = ? WHERE id = ?`, step, nowDB(), u.ID)
		return true
	}
	if !u.TwoFactorRecoveryEnc.Valid {
		return false
	}
	raw, err := app.store.enc.DecryptString(u.TwoFactorRecoveryEnc.String)
	if err != nil {
		return false
	}
	var codes []string
	if json.Unmarshal([]byte(raw), &codes) != nil {
		return false
	}
	want := normalizeRecoveryCode(code)
	for i, c := range codes {
		if c == want {
			remaining, _ := json.Marshal(append(codes[:i:i], codes[i+1:]...))
			enc, err := app.store.enc.EncryptString(string(remaining))
			if err != nil {
				return false
			}
			app.store.db.Exec(`UPDATE users SET two_factor_recovery_codes = ?, updated_at = ? WHERE id = ?`, enc, nowDB(), u.ID)
			return true
		}
	}
	return false
}

func verifyEmailCode(u *User, code string) bool {
	if !u.TwoFactorCode.Valid || u.TwoFactorExpires == nil || u.TwoFactorExpires.Before(time.Now()) {
		return false
	}
	return checkPassword(u.TwoFactorCode.String, strings.ToUpper(code))
}

// doVerifyEmail sends (or resends) an emailed code and switches this login
// to it, like the PHP hub's "Email me a code instead".
func (app *App) doVerifyEmail(w http.ResponseWriter, r *http.Request) {
	s := sessionFrom(r)
	u, err := app.store.UserByID(pendingUserID(s))
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	if err := app.sendLoginCode(u); err != nil {
		s.Flash("Couldn't send the code: " + err.Error())
	} else {
		s.Set("2fa.method", "email")
		s.Flash("A code has been sent to your email.")
	}
	http.Redirect(w, r, "/verify-2fa", http.StatusFound)
}

func (app *App) doLogout(w http.ResponseWriter, r *http.Request) {
	app.logout(w, sessionFrom(r))
	http.Redirect(w, r, "/login", http.StatusFound)
}

// ---- invitations: set your own password ----

func (app *App) invitedUser(token string) *User {
	if len(token) < 32 {
		return nil
	}
	var id int64
	err := app.store.db.QueryRow(`SELECT id FROM users WHERE invite_token = ? AND invite_token_expires_at > ?`, token, nowDB()).Scan(&id)
	if err != nil {
		return nil
	}
	u, _ := app.store.UserByID(id)
	return u
}

func (app *App) showSetPassword(w http.ResponseWriter, r *http.Request) {
	u := app.invitedUser(r.PathValue("token"))
	if u == nil {
		app.errorPage(w, r, http.StatusNotFound, "This invitation link is invalid or has expired. Ask an admin to resend it.")
		return
	}
	app.render(w, r, http.StatusOK, "setpassword", "Set your password", map[string]any{"Email": u.Email, "Token": r.PathValue("token")})
}

func (app *App) doSetPassword(w http.ResponseWriter, r *http.Request) {
	tok := r.PathValue("token")
	u := app.invitedUser(tok)
	if u == nil {
		app.errorPage(w, r, http.StatusNotFound, "This invitation link is invalid or has expired. Ask an admin to resend it.")
		return
	}
	pw, confirm := r.PostFormValue("password"), r.PostFormValue("password_confirmation")
	msg := ""
	switch {
	case len(pw) < 8:
		msg = "The password must be at least 8 characters."
	case len(pw) > 72:
		msg = "The password must be at most 72 characters."
	case pw != confirm:
		msg = "The password confirmation does not match."
	}
	if msg != "" {
		app.render(w, r, http.StatusUnprocessableEntity, "setpassword", "Set your password", map[string]any{"Email": u.Email, "Token": tok, "Error": msg})
		return
	}
	app.store.db.Exec(`UPDATE users SET password = ?, invite_token = NULL, invite_token_expires_at = NULL, email_verified_at = ?, updated_at = ? WHERE id = ?`,
		hashPassword(pw), nowDB(), nowDB(), u.ID)
	sessionFrom(r).Flash("Your password is set. Log in to continue.")
	http.Redirect(w, r, "/login", http.StatusFound)
}

func clientIP(r *http.Request) string {
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i >= 0 {
		host = host[:i]
	}
	return strings.Trim(host, "[]")
}
