package main

import (
	"encoding/base32"
	"encoding/json"
	"fmt"
	"html/template"
	"crypto/rand"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	"rsc.io/qr"
)

// Profile, Settings, first-run Setup and password reset: the parts of the
// PHP hub the first Go prototype left out.

// publicURL mirrors App\Support\PublicUrl: APP_URL when it's a real
// address, else the address saved in Settings, else this request's host.
func (app *App) publicURL(r *http.Request) string {
	if u := app.cfg.AppURL; u != "" {
		if p, err := url.Parse(u); err == nil && p.Hostname() != "" && p.Hostname() != "localhost" && p.Hostname() != "127.0.0.1" && p.Hostname() != "::1" {
			return u
		}
	}
	if u := strings.TrimRight(app.store.Setting("public_url"), "/"); u != "" {
		return u
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func (app *App) setSetting(key, value string) error {
	now := nowDB()
	if value == "" {
		_, err := app.store.db.Exec(`INSERT INTO settings (key, value, created_at, updated_at) VALUES (?, NULL, ?, ?)
			ON CONFLICT(key) DO UPDATE SET value = NULL, updated_at = excluded.updated_at`, key, now, now)
		return err
	}
	enc, err := app.store.enc.EncryptString(value)
	if err != nil {
		return err
	}
	_, err = app.store.db.Exec(`INSERT INTO settings (key, value, created_at, updated_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, key, enc, now, now)
	return err
}

// ---- Profile ----

type profileData struct {
	Errors         map[string]string
	Status         string
	SettingUp      bool
	Enabled        bool
	QR             template.HTML
	SetupKey       string
	RecoveryCodes  []string // shown once, right after they're made
	RemainingCodes int
	MailConfigured bool
}

func (app *App) profile(w http.ResponseWriter, r *http.Request) {
	app.renderProfile(w, r, http.StatusOK, nil, "", nil)
}

func (app *App) renderProfile(w http.ResponseWriter, r *http.Request, status int, errs map[string]string, msg string, codes []string) {
	u, _ := app.store.UserByID(userFrom(r).ID)
	d := profileData{Errors: errs, Status: msg, Enabled: u.HasAuthenticator(), RecoveryCodes: codes, MailConfigured: app.mailer.Configured()}
	if d.Errors == nil {
		d.Errors = map[string]string{}
	}
	if u.TwoFactorSecretEnc.Valid && !u.HasAuthenticator() {
		if secret, err := app.store.enc.DecryptString(u.TwoFactorSecretEnc.String); err == nil {
			d.SettingUp = true
			d.QR = qrSVG(fmt.Sprintf("otpauth://totp/%s:%s?secret=%s&issuer=%s&algorithm=SHA1&digits=6&period=30",
				url.PathEscape(app.cfg.AppName), url.PathEscape(u.Email), secret, url.QueryEscape(app.cfg.AppName)))
			var groups []string
			for i := 0; i < len(secret); i += 4 {
				groups = append(groups, secret[i:min(i+4, len(secret))])
			}
			d.SetupKey = strings.Join(groups, " ")
		}
	}
	if u.TwoFactorRecoveryEnc.Valid {
		if raw, err := app.store.enc.DecryptString(u.TwoFactorRecoveryEnc.String); err == nil {
			var c []string
			json.Unmarshal([]byte(raw), &c)
			d.RemainingCodes = len(c)
		}
	}
	userFrom(r).Name, userFrom(r).Email = u.Name, u.Email
	app.render(w, r, status, "profile", "Profile", d)
}

// qrSVG draws a QR code as inline SVG (no image file to serve).
func qrSVG(text string) template.HTML {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return ""
	}
	n := code.Size
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="-2 -2 %d %d" width="192" height="192" shape-rendering="crispEdges"><rect x="-2" y="-2" width="%d" height="%d" fill="#fff"/><path fill="#000" d="`, n+4, n+4, n+4, n+4)
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			if code.Black(x, y) {
				fmt.Fprintf(&b, "M%d %dh1v1h-1z", x, y)
			}
		}
	}
	b.WriteString(`"/></svg>`)
	return template.HTML(b.String())
}

func (app *App) updateProfile(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	name := strings.TrimSpace(r.PostFormValue("name"))
	email := strings.ToLower(strings.TrimSpace(r.PostFormValue("email")))
	errs := map[string]string{}
	if name == "" || len(name) > 255 {
		errs["name"] = "The name field is required."
	}
	if _, err := mail.ParseAddress(email); err != nil || len(email) > 255 {
		errs["email"] = "Enter a valid email address."
	} else if other, err := app.store.UserByEmail(email); err == nil && other.ID != u.ID {
		errs["email"] = "The email has already been taken."
	}
	if len(errs) > 0 {
		app.renderProfile(w, r, http.StatusUnprocessableEntity, errs, "", nil)
		return
	}
	app.store.db.Exec(`UPDATE users SET name = ?, email = ?, updated_at = ? WHERE id = ?`, name, email, nowDB(), u.ID)
	app.renderProfile(w, r, http.StatusOK, nil, "Profile saved.", nil)
}

func (app *App) updatePassword(w http.ResponseWriter, r *http.Request) {
	u, _ := app.store.UserByID(userFrom(r).ID)
	cur, pw, confirm := r.PostFormValue("current_password"), r.PostFormValue("password"), r.PostFormValue("password_confirmation")
	errs := map[string]string{}
	switch {
	case !checkPassword(u.PasswordHash, cur):
		errs["current_password"] = "The password is incorrect."
	case len(pw) < 8 || len(pw) > 72:
		errs["password"] = "The password must be between 8 and 72 characters."
	case pw != confirm:
		errs["password"] = "The password confirmation does not match."
	}
	if len(errs) > 0 {
		app.renderProfile(w, r, http.StatusUnprocessableEntity, errs, "", nil)
		return
	}
	app.store.db.Exec(`UPDATE users SET password = ?, updated_at = ? WHERE id = ?`, hashPassword(pw), nowDB(), u.ID)
	// Other sessions of this user end; this one continues.
	app.store.db.Exec(`DELETE FROM go_sessions WHERE user_id = ? AND id != ?`, u.ID, sessionFrom(r).ID)
	app.renderProfile(w, r, http.StatusOK, nil, "Password changed.", nil)
}

// newTOTPSecret matches Google2FA::generateSecretKey(32): 32 base32 chars.
func newTOTPSecret() string {
	b := make([]byte, 20)
	rand.Read(b)
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
}

func newRecoveryCodes() []string {
	out := make([]string, 8)
	for i := range out {
		c := randomFrom(recoveryAlphabet, 10)
		out[i] = c[:5] + "-" + c[5:]
	}
	return out
}

func (app *App) authenticatorAction(w http.ResponseWriter, r *http.Request) {
	u, _ := app.store.UserByID(userFrom(r).ID)
	action := r.PathValue("action")
	switch action {
	case "start":
		if !u.HasAuthenticator() {
			enc, _ := app.store.enc.EncryptString(newTOTPSecret())
			app.store.db.Exec(`UPDATE users SET two_factor_secret = ?, two_factor_confirmed_at = NULL, two_factor_recovery_codes = NULL,
				two_factor_last_used_step = NULL, updated_at = ? WHERE id = ?`, enc, nowDB(), u.ID)
		}
	case "confirm":
		if !u.TwoFactorSecretEnc.Valid || u.HasAuthenticator() {
			break
		}
		secret, _ := app.store.enc.DecryptString(u.TwoFactorSecretEnc.String)
		step := verifyTOTP(secret, r.PostFormValue("code"), 0, time.Now())
		if step == 0 {
			app.renderProfile(w, r, http.StatusUnprocessableEntity, map[string]string{"code": "That code didn't match. Check the time on your phone is set automatically, and try the newest code."}, "", nil)
			return
		}
		codes := newRecoveryCodes()
		raw, _ := json.Marshal(codes)
		enc, _ := app.store.enc.EncryptString(string(raw))
		app.store.db.Exec(`UPDATE users SET two_factor_confirmed_at = ?, two_factor_last_used_step = ?, two_factor_recovery_codes = ?, updated_at = ? WHERE id = ?`,
			nowDB(), step, enc, nowDB(), u.ID)
		app.renderProfile(w, r, http.StatusOK, nil, "Authenticator app turned on.", codes)
		return
	case "cancel":
		if !u.HasAuthenticator() {
			app.clearAuthenticator(u.ID)
		}
	case "regenerate":
		if u.HasAuthenticator() {
			codes := newRecoveryCodes()
			raw, _ := json.Marshal(codes)
			enc, _ := app.store.enc.EncryptString(string(raw))
			app.store.db.Exec(`UPDATE users SET two_factor_recovery_codes = ?, updated_at = ? WHERE id = ?`, enc, nowDB(), u.ID)
			app.renderProfile(w, r, http.StatusOK, nil, "New recovery codes made; the old ones no longer work.", codes)
			return
		}
	case "remove":
		if !checkPassword(u.PasswordHash, r.PostFormValue("current_password")) {
			app.renderProfile(w, r, http.StatusUnprocessableEntity, map[string]string{"remove_password": "The password is incorrect."}, "", nil)
			return
		}
		app.clearAuthenticator(u.ID)
	default:
		app.errorPage(w, r, 404, "Unknown action.")
		return
	}
	http.Redirect(w, r, "/profile#authenticator", http.StatusSeeOther)
}

func (app *App) clearAuthenticator(userID int64) {
	app.store.db.Exec(`UPDATE users SET two_factor_secret = NULL, two_factor_recovery_codes = NULL, two_factor_confirmed_at = NULL,
		two_factor_last_used_step = NULL, updated_at = ? WHERE id = ?`, nowDB(), userID)
}

func (app *App) deleteAccount(w http.ResponseWriter, r *http.Request) {
	u, _ := app.store.UserByID(userFrom(r).ID)
	if !checkPassword(u.PasswordHash, r.PostFormValue("password")) {
		app.renderProfile(w, r, http.StatusUnprocessableEntity, map[string]string{"delete_password": "The password is incorrect."}, "", nil)
		return
	}
	app.logout(w, sessionFrom(r))
	app.store.db.Exec(`DELETE FROM go_sessions WHERE user_id = ?`, u.ID)
	app.store.db.Exec(`DELETE FROM users WHERE id = ?`, u.ID)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// ---- Settings (admin) ----

type settingsData struct {
	Host, Username, Encryption, FromAddress, FromName, HubAddress, HubFromEnv, TestStatus, Status string
	Port                                                                                          int
	HasPassword                                                                                   bool
	Errors                                                                                        map[string]string
	Tab                                                                                           string // "email" or "updates"
	Upd                                                                                           *updatesData
}

func (app *App) loadSettings() settingsData {
	port, _ := strconv.Atoi(app.store.Setting("smtp_port"))
	if port == 0 {
		port = 587
	}
	enc := app.store.Setting("smtp_encryption")
	if enc == "" {
		enc = "none"
		if app.store.Setting("smtp_host") == "" {
			enc = "tls"
		}
	}
	d := settingsData{Host: app.store.Setting("smtp_host"), Port: port, Username: app.store.Setting("smtp_username"), Encryption: enc,
		FromAddress: app.store.Setting("smtp_from_address"), FromName: app.store.Setting("smtp_from_name"),
		HubAddress: app.store.Setting("public_url"), HasPassword: app.store.Setting("smtp_password") != "", Errors: map[string]string{}}
	if d.FromName == "" {
		d.FromName = app.cfg.AppName
	}
	if u := app.cfg.AppURL; u != "" && !strings.Contains(u, "localhost") {
		d.HubFromEnv = u
	}
	return d
}

func (app *App) settings(w http.ResponseWriter, r *http.Request) {
	d := app.loadSettings()
	d.Tab = "email"
	if r.URL.Query().Get("tab") == "updates" {
		d.Tab, d.Upd = "updates", app.updatesView(r)
	}
	app.render(w, r, http.StatusOK, "settings", "Settings", d)
}

func (app *App) saveSettings(w http.ResponseWriter, r *http.Request) {
	d := app.loadSettings()
	switch r.PostFormValue("section") {
	case "hub":
		addr := strings.TrimRight(strings.TrimSpace(r.PostFormValue("hub_address")), "/")
		if addr != "" {
			if p, err := url.Parse(addr); err != nil || (p.Scheme != "http" && p.Scheme != "https") || p.Host == "" {
				d.Errors["hub_address"] = "Enter an address starting with http:// or https://."
				d.HubAddress = addr
				d.Tab = "email"
				app.render(w, r, http.StatusUnprocessableEntity, "settings", "Settings", d)
				return
			}
		}
		app.setSetting("public_url", addr)
		sessionFrom(r).Flash("Hub address saved.")
	case "test":
		err := app.mailer.Send(userFrom(r).Email, "Your LANHub login code", "Your login code is:\n\n    TESTCODE1\n\n(This is a test email from the Settings page.)\n")
		if err != nil {
			sessionFrom(r).Flash("Failed to send: " + err.Error())
		} else if !app.mailer.Configured() {
			sessionFrom(r).Flash("Email isn't set up, so the test was written to the log instead.")
		} else {
			sessionFrom(r).Flash("Test email sent to " + userFrom(r).Email + " — check your inbox.")
		}
	default:
		port, err := strconv.Atoi(strings.TrimSpace(r.PostFormValue("smtp_port")))
		enc := r.PostFormValue("smtp_encryption")
		from := strings.TrimSpace(r.PostFormValue("smtp_from_address"))
		if r.PostFormValue("smtp_port") != "" && (err != nil || port < 1 || port > 65535) {
			d.Errors["smtp_port"] = "The port must be between 1 and 65535."
		}
		if enc != "tls" && enc != "ssl" && enc != "none" {
			d.Errors["smtp_encryption"] = "Pick an encryption option."
		}
		if from != "" {
			if _, err := mail.ParseAddress(from); err != nil {
				d.Errors["smtp_from_address"] = "Enter a valid email address."
			}
		}
		if len(d.Errors) > 0 {
			d.Tab = "email"
			app.render(w, r, http.StatusUnprocessableEntity, "settings", "Settings", d)
			return
		}
		if enc == "none" {
			enc = ""
		}
		app.setSetting("smtp_host", strings.TrimSpace(r.PostFormValue("smtp_host")))
		app.setSetting("smtp_port", strconv.Itoa(max(port, 0)))
		app.setSetting("smtp_username", strings.TrimSpace(r.PostFormValue("smtp_username")))
		app.setSetting("smtp_encryption", enc)
		app.setSetting("smtp_from_address", from)
		app.setSetting("smtp_from_name", strings.TrimSpace(r.PostFormValue("smtp_from_name")))
		// Blank means "keep the current password".
		if pw := r.PostFormValue("smtp_password"); pw != "" {
			app.setSetting("smtp_password", pw)
		}
		sessionFrom(r).Flash("Settings saved.")
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

// ---- first-run Setup ----

func (app *App) showSetup(w http.ResponseWriter, r *http.Request) {
	if app.store.UserCount() > 0 {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	app.render(w, r, http.StatusOK, "setup", "Set up LANHub", map[string]any{"Errors": map[string]string{}})
}

func (app *App) doSetup(w http.ResponseWriter, r *http.Request) {
	if app.store.UserCount() > 0 { // re-checked right before writing
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	name := strings.TrimSpace(r.PostFormValue("name"))
	email := strings.ToLower(strings.TrimSpace(r.PostFormValue("email")))
	pw, confirm := r.PostFormValue("password"), r.PostFormValue("password_confirmation")
	errs := map[string]string{}
	if name == "" {
		errs["name"] = "The name field is required."
	}
	if _, err := mail.ParseAddress(email); err != nil {
		errs["email"] = "Enter a valid email address."
	}
	if len(pw) < 8 || len(pw) > 72 {
		errs["password"] = "The password must be between 8 and 72 characters."
	} else if pw != confirm {
		errs["password"] = "The password confirmation does not match."
	}
	if len(errs) > 0 {
		app.render(w, r, http.StatusUnprocessableEntity, "setup", "Set up LANHub", map[string]any{"Errors": errs, "Name": name, "Email": email})
		return
	}
	res, err := app.store.db.Exec(`INSERT INTO users (name, email, password, is_admin, email_verified_at, created_at, updated_at) VALUES (?, ?, ?, 1, ?, ?, ?)`,
		name, email, hashPassword(pw), nowDB(), nowDB(), nowDB())
	if err != nil {
		app.errorPage(w, r, 500, "Couldn't create the account.")
		return
	}
	id, _ := res.LastInsertId()
	// Same trust boundary as the PHP hub: the very first account logs in
	// straight away, since no code could reach it yet.
	if app.store.Setting("public_url") == "" && app.cfg.AppURL == "" {
		app.setSetting("public_url", app.publicURL(r))
	}
	s := app.login(w, r, sessionFrom(r), id, false)
	s.Flash("Welcome. Every later login needs a code, so set up an authenticator app first.")
	app.saveSession(s)
	http.Redirect(w, r, "/profile#authenticator", http.StatusSeeOther)
}

// ---- forgot / reset password (Laravel's password_reset_tokens table) ----

const resetExpiry = 60 * time.Minute

func (app *App) showForgot(w http.ResponseWriter, r *http.Request) {
	app.render(w, r, http.StatusOK, "forgot", "Forgot password", map[string]any{})
}

func (app *App) doForgot(w http.ResponseWriter, r *http.Request) {
	email := strings.ToLower(strings.TrimSpace(r.PostFormValue("email")))
	// Same answer whether or not the address exists.
	done := func() {
		sessionFrom(r).Flash("If that address has an account, a reset link is on its way.")
		http.Redirect(w, r, "/forgot-password", http.StatusSeeOther)
	}
	key := "forgot:" + email + "|" + clientIP(r)
	if locked, _ := app.limiter.TooMany(key); locked {
		done()
		return
	}
	app.limiter.Hit(key)
	u, err := app.store.UserByEmail(email)
	if err != nil || u.IsPending() {
		done()
		return
	}
	var created string
	if app.store.db.QueryRow(`SELECT created_at FROM password_reset_tokens WHERE email = ?`, email).Scan(&created) == nil {
		if t, ok := parseTimeText(created); ok && time.Since(t) < 60*time.Second {
			done() // throttle: one link a minute
			return
		}
	}
	tok := randomFrom("abcdef0123456789", 64)
	app.store.db.Exec(`INSERT INTO password_reset_tokens (email, token, created_at) VALUES (?, ?, ?)
		ON CONFLICT(email) DO UPDATE SET token = excluded.token, created_at = excluded.created_at`, email, hashPassword(tok), nowDB())
	link := app.publicURL(r) + "/reset-password/" + tok + "?email=" + url.QueryEscape(email)
	if err := app.mailer.Send(email, "Reset your LANHub password", "Someone asked to reset the password for this LANHub account.\n\nReset it here (the link works for 60 minutes):\n\n"+link+"\n\nIf this wasn't you, ignore this email.\n"); err != nil {
		logf("password reset mail to %s failed: %v", email, err)
	}
	done()
}

func (app *App) showReset(w http.ResponseWriter, r *http.Request) {
	app.render(w, r, http.StatusOK, "reset", "Reset password", map[string]any{"Token": r.PathValue("token"), "Email": r.URL.Query().Get("email")})
}

func (app *App) doReset(w http.ResponseWriter, r *http.Request) {
	tok := r.PathValue("token")
	email := strings.ToLower(strings.TrimSpace(r.PostFormValue("email")))
	pw, confirm := r.PostFormValue("password"), r.PostFormValue("password_confirmation")
	fail := func(msg string) {
		app.render(w, r, http.StatusUnprocessableEntity, "reset", "Reset password", map[string]any{"Token": tok, "Email": email, "Error": msg})
	}
	if len(pw) < 8 || len(pw) > 72 {
		fail("The password must be between 8 and 72 characters.")
		return
	}
	if pw != confirm {
		fail("The password confirmation does not match.")
		return
	}
	var hash, created string
	err := app.store.db.QueryRow(`SELECT token, created_at FROM password_reset_tokens WHERE email = ?`, email).Scan(&hash, &created)
	t, ok := parseTimeText(created)
	if err != nil || !ok || time.Since(t) > resetExpiry || !checkPassword(hash, tok) {
		fail("This password reset token is invalid.")
		return
	}
	app.store.db.Exec(`UPDATE users SET password = ?, updated_at = ? WHERE email = ?`, hashPassword(pw), nowDB(), email)
	app.store.db.Exec(`DELETE FROM password_reset_tokens WHERE email = ?`, email)
	app.store.db.Exec(`DELETE FROM go_sessions WHERE user_id = (SELECT id FROM users WHERE email = ?)`, email)
	sessionFrom(r).Flash("Your password has been reset. Log in to continue.")
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
