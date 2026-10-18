package main

import (
	"crypto/tls"
	"fmt"
	"log"
	"mime"
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
	return m.send("", "", "", true)
}

func (m *Mailer) Send(to, subject, body string) error {
	return m.send(to, subject, body, false)
}

func (m *Mailer) send(to, subject, body string, checkOnly bool) error {
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
	msg := strings.Join([]string{
		"From: " + mime.QEncoding.Encode("utf-8", fromName) + " <" + from + ">",
		"To: " + to,
		"Subject: " + mime.QEncoding.Encode("utf-8", subject),
		"Date: " + time.Now().Format(time.RFC1123Z),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=utf-8",
		"Content-Transfer-Encoding: 8bit",
		"",
		strings.ReplaceAll(body, "\n", "\r\n"),
	}, "\r\n")
	if _, err := wc.Write([]byte(msg)); err != nil {
		return err
	}
	if err := wc.Close(); err != nil {
		return err
	}
	return c.Quit()
}
