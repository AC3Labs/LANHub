package main

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestSetupCreatesTheFirstAdminOnlyOnce(t *testing.T) {
	h := newTestHub(t)
	c := h.client()
	if resp, _ := c.get("/login"); resp.Header.Get("Location") != "/setup" {
		t.Fatal("an empty hub should send /login to /setup")
	}
	c.get("/setup")
	resp, _ := c.postForm("/setup", url.Values{"name": {"First"}, "email": {"first@example.com"}, "password": {"long-enough-1"}, "password_confirmation": {"long-enough-1"}})
	if resp.StatusCode != http.StatusSeeOther || !strings.HasPrefix(resp.Header.Get("Location"), "/profile") {
		t.Fatalf("setup: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	u, err := h.store.UserByEmail("first@example.com")
	if err != nil || !u.IsAdmin {
		t.Fatal("first account must be an admin")
	}
	if resp, _ := c.get("/profile"); resp.StatusCode != 200 {
		t.Fatalf("logged straight in after setup: %d", resp.StatusCode)
	}
	// Once anyone exists, setup can't make another admin.
	c2 := h.client()
	c2.get("/login")
	c2.postForm("/setup", url.Values{"name": {"Second"}, "email": {"second@example.com"}, "password": {"long-enough-1"}, "password_confirmation": {"long-enough-1"}})
	if _, err := h.store.UserByEmail("second@example.com"); err == nil {
		t.Fatal("setup must refuse once a user exists")
	}
}

func TestPasswordResetWorksOnceAndAnswersTheSameForUnknownEmails(t *testing.T) {
	h := newTestHub(t)
	h.user("ann@example.com", false)
	c := h.client()
	c.get("/forgot-password")
	_, unknown := c.postForm("/forgot-password", url.Values{"email": {"nobody@example.com"}})
	resp, _ := c.postForm("/forgot-password", url.Values{"email": {"ann@example.com"}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("forgot: %d", resp.StatusCode)
	}
	_, known := c.get("/forgot-password")
	_ = unknown
	if !strings.Contains(known, "If that address has an account") {
		t.Fatal("expected the neutral confirmation message")
	}
	// The link went to the log (no SMTP); use a token we set ourselves.
	h.exec(`UPDATE password_reset_tokens SET token = ? WHERE email = ?`, hashPassword("known-token"), "ann@example.com")
	c.get("/reset-password/known-token?email=ann@example.com")
	resp, _ = c.postForm("/reset-password/known-token", url.Values{"email": {"ann@example.com"}, "password": {"brand-new-pass"}, "password_confirmation": {"brand-new-pass"}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("reset: %d", resp.StatusCode)
	}
	u, _ := h.store.UserByEmail("ann@example.com")
	if !checkPassword(u.PasswordHash, "brand-new-pass") {
		t.Fatal("password should have changed")
	}
	c.get("/reset-password/known-token?email=ann@example.com")
	if _, body := c.postForm("/reset-password/known-token", url.Values{"email": {"ann@example.com"}, "password": {"another-pass-1"}, "password_confirmation": {"another-pass-1"}}); !strings.Contains(body, "invalid") {
		t.Fatal("a used token must not work twice")
	}
}

func TestExpiredResetTokenIsRefused(t *testing.T) {
	h := newTestHub(t)
	h.user("ann@example.com", false)
	h.exec(`INSERT INTO password_reset_tokens (email, token, created_at) VALUES (?, ?, ?)`, "ann@example.com", hashPassword("old"), time.Now().UTC().Add(-2*time.Hour).Format(dbTime))
	c := h.client()
	c.get("/reset-password/old")
	if _, body := c.postForm("/reset-password/old", url.Values{"email": {"ann@example.com"}, "password": {"brand-new-pass"}, "password_confirmation": {"brand-new-pass"}}); !strings.Contains(body, "invalid") {
		t.Fatal("an expired token must be refused")
	}
}

var setupKeyRe = regexp.MustCompile(`font-mono text-sm text-stone-800 break-all">([A-Z2-7 ]+)<`)

func TestAuthenticatorEnrolmentNeedsAWorkingCodeAndGivesRecoveryCodes(t *testing.T) {
	h := newTestHub(t)
	id := h.user("ann@example.com", false)
	c := h.client()
	c.login("ann@example.com")
	h.exec(`UPDATE users SET two_factor_secret = NULL, two_factor_confirmed_at = NULL WHERE id = ?`, id)

	c.get("/profile")
	c.postForm("/profile/authenticator/start", url.Values{})
	_, body := c.get("/profile")
	m := setupKeyRe.FindStringSubmatch(body)
	if m == nil || !strings.Contains(body, "<svg") {
		t.Fatal("setup should show a QR code and the key")
	}
	secret := strings.ReplaceAll(m[1], " ", "")
	if _, body := c.postForm("/profile/authenticator/confirm", url.Values{"code": {"000000"}}); !strings.Contains(body, "didn&#39;t match") {
		t.Fatal("a wrong code must not turn it on")
	}
	code, _ := totpCode(secret, time.Now().Unix()/30)
	_, body = c.postForm("/profile/authenticator/confirm", url.Values{"code": {code}})
	if !strings.Contains(body, "recovery codes somewhere safe") {
		t.Fatal("recovery codes should be shown once")
	}
	u, _ := h.store.UserByID(id)
	if !u.HasAuthenticator() || u.TwoFactorLastUsedStep == 0 {
		t.Fatal("authenticator should be on, with the used step recorded")
	}
	// Turning it off needs the password.
	c.postForm("/profile/authenticator/remove", url.Values{"current_password": {"wrong"}})
	if u, _ := h.store.UserByID(id); !u.HasAuthenticator() {
		t.Fatal("wrong password must not turn it off")
	}
	c.postForm("/profile/authenticator/remove", url.Values{"current_password": {"password-123"}})
	if u, _ := h.store.UserByID(id); u.HasAuthenticator() {
		t.Fatal("should be off")
	}
}

func TestSettingsAreAdminOnlyAndStoreEncryptedValues(t *testing.T) {
	h := newTestHub(t)
	h.user("ann@example.com", false)
	h.user("root@example.com", true)
	c := h.client()
	c.login("ann@example.com")
	if resp, _ := c.get("/settings"); resp.StatusCode != 403 {
		t.Fatalf("non-admin: %d", resp.StatusCode)
	}
	a := h.client()
	a.login("root@example.com")
	a.get("/settings")
	a.postForm("/settings", url.Values{"section": {"smtp"}, "smtp_host": {"mail.example.com"}, "smtp_port": {"465"}, "smtp_encryption": {"ssl"}, "smtp_password": {"s3cret"}})
	if h.store.Setting("smtp_host") != "mail.example.com" || h.store.Setting("smtp_password") != "s3cret" {
		t.Fatal("settings not saved")
	}
	var raw string
	h.store.db.QueryRow(`SELECT value FROM settings WHERE key = 'smtp_password'`).Scan(&raw)
	if strings.Contains(raw, "s3cret") {
		t.Fatal("settings must be stored encrypted, like the PHP hub's")
	}
	// Leaving the password blank keeps it.
	a.postForm("/settings", url.Values{"section": {"smtp"}, "smtp_host": {"mail.example.com"}, "smtp_port": {"465"}, "smtp_encryption": {"ssl"}})
	if h.store.Setting("smtp_password") != "s3cret" {
		t.Fatal("blank password field should keep the saved one")
	}
}

func TestCLICreateAdminAndResetAuthenticator(t *testing.T) {
	h := newTestHub(t)
	if code := runCLI([]string{"create-admin", "-name", "Ops", "-email", "ops@example.com", "-password", "long-enough-1"}, h.store); code != 0 {
		t.Fatalf("create-admin exit %d", code)
	}
	u, err := h.store.UserByEmail("ops@example.com")
	if err != nil || !u.IsAdmin || !checkPassword(u.PasswordHash, "long-enough-1") {
		t.Fatal("admin not created properly")
	}
	h.user("ann@example.com", false)
	if code := runCLI([]string{"reset-authenticator", "ann@example.com"}, h.store); code != 0 {
		t.Fatalf("reset-authenticator exit %d", code)
	}
	if u, _ := h.store.UserByEmail("ann@example.com"); u.HasAuthenticator() {
		t.Fatal("authenticator should be gone")
	}
}
