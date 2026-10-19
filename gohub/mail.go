package main

import (
	"crypto/tls"
	"fmt"
	"log"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// Mailer sends through the SMTP settings saved on the PHP hub's Settings
// page (settings table, encrypted). With none saved it logs the message,
// like Laravel's "log" mailer, so a login code is still recoverable from
// the container log.
type Mailer struct {
	store   *Store
	appName string
}

func (m *Mailer) Configured() bool { return m.store.Setting("smtp_host") != "" }

// Check connects and logs in to the mail server without sending anything.
func (m *Mailer) Check() error {
	return m.send("", "", "", "", true)
}

func (m *Mailer) Send(to, subject, body string) error {
	return m.send(to, subject, body, "", false)
}

// SendHTML sends a message with a plain-text and an HTML version; mail apps
// show whichever they can.
func (m *Mailer) SendHTML(to, subject, text, html string) error {
	return m.send(to, subject, text, html, false)
}

func (m *Mailer) send(to, subject, body, html string, checkOnly bool) error {
	host := m.store.Setting("smtp_host")
	if host == "" {
		if checkOnly {
			return fmt.Errorf("no mail server is set up (Settings page)")
		}
		log.Printf("[mail not configured] to=%s subject=%q\n%s", to, subject, body)
		return nil
	}
	port := m.store.Setting("smtp_port")
	if port == "" {
		port = "587"
	}
	user, pass := m.store.Setting("smtp_username"), m.store.Setting("smtp_password")
	from := m.store.Setting("smtp_from_address")
	if from == "" {
		from = user
	}
	fromName := m.store.Setting("smtp_from_name")
	if fromName == "" {
		fromName = m.appName
	}
	enc := m.store.Setting("smtp_encryption") // "ssl" = SMTPS, "tls" = STARTTLS, "" = none

	addr := net.JoinHostPort(host, port)
	var conn net.Conn
	var err error
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	if enc == "ssl" {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: host})
	} else {
		conn, err = dialer.Dial("tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("can't reach the mail server: %w", err)
	}
	conn.SetDeadline(time.Now().Add(30 * time.Second))
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer c.Close()
	if enc == "tls" {
		if err := c.StartTLS(&tls.Config{ServerName: host}); err != nil {
			return fmt.Errorf("STARTTLS failed: %w", err)
		}
	}
	if user != "" {
		if err := c.Auth(smtp.PlainAuth("", user, pass, host)); err != nil {
			return fmt.Errorf("the mail server refused the login: %w", err)
		}
	}
	if checkOnly {
		return c.Quit()
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	wc, err := c.Data()
	if err != nil {
		return err
	}
	msg := buildMessage(mime.QEncoding.Encode("utf-8", fromName)+" <"+from+">", to, subject, body, html)
	if _, err := wc.Write([]byte(msg)); err != nil {
		return err
	}
	if err := wc.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// buildMessage writes the email: plain text, or multipart/alternative with
// an HTML part when there is one. Bodies are quoted-printable, so long HTML
// lines stay inside SMTP's line limit.
func buildMessage(from, to, subject, text, html string) string {
	head := []string{
		"From: " + from,
		"To: " + to,
		"Subject: " + mime.QEncoding.Encode("utf-8", subject),
		"Date: " + time.Now().Format(time.RFC1123Z),
		"MIME-Version: 1.0",
	}
	part := func(ctype, body string) []string {
		return []string{"Content-Type: " + ctype + "; charset=utf-8", "Content-Transfer-Encoding: quoted-printable", "", quotedPrintable(body)}
	}
	if html == "" {
		return strings.Join(append(head, part("text/plain", text)...), "\r\n")
	}
	boundary := "lanhub-" + randomToken(24)
	lines := append(head, `Content-Type: multipart/alternative; boundary="`+boundary+`"`, "", "--"+boundary)
	lines = append(lines, part("text/plain", text)...)
	lines = append(lines, "--"+boundary)
	lines = append(lines, part("text/html", html)...)
	lines = append(lines, "--"+boundary+"--", "")
	return strings.Join(lines, "\r\n")
}

func quotedPrintable(s string) string {
	var b strings.Builder
	w := quotedprintable.NewWriter(&b)
	w.Write([]byte(strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\n", "\r\n")))
	w.Close()
	return b.String()
}
