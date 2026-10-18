package main

import (
	"flag"
	"fmt"
	"net/mail"
	"os"
	"strings"
)

// Command-line equivalents of the PHP hub's artisan commands, for use with
// `docker exec <container> /lanhub-hub <command>`.
//
//	create-admin -name NAME -email EMAIL -password PASSWORD
//	reset-authenticator EMAIL
func runCLI(args []string, store *Store) int {
	switch args[0] {
	case "create-admin":
		fs := flag.NewFlagSet("create-admin", flag.ExitOnError)
		name := fs.String("name", "", "display name")
		email := fs.String("email", "", "email address")
		password := fs.String("password", "", "password (8-72 characters)")
		fs.Parse(args[1:])
		e := strings.ToLower(strings.TrimSpace(*email))
		if _, err := mail.ParseAddress(e); err != nil || strings.TrimSpace(*name) == "" || len(*password) < 8 || len(*password) > 72 {
			fmt.Fprintln(os.Stderr, "usage: create-admin -name NAME -email EMAIL -password PASSWORD (8-72 characters)")
			return 2
		}
		if _, err := store.UserByEmail(e); err == nil {
			fmt.Fprintln(os.Stderr, e+" already has an account.")
			return 1
		}
		_, err := store.db.Exec(`INSERT INTO users (name, email, password, is_admin, email_verified_at, created_at, updated_at) VALUES (?, ?, ?, 1, ?, ?, ?)`,
			strings.TrimSpace(*name), e, hashPassword(*password), nowDB(), nowDB(), nowDB())
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println("Admin account created for " + e + ".")
		return 0
	case "check-mail":
		if err := (&Mailer{store: store}).Check(); err != nil {
			fmt.Fprintln(os.Stderr, "Mail check failed: "+err.Error())
			return 1
		}
		fmt.Println("Mail server reached and the login was accepted (nothing was sent).")
		return 0
	case "reset-authenticator":
		if len(args) != 2 {
			fmt.Fprintln(os.Stderr, "usage: reset-authenticator EMAIL")
			return 2
		}
		res, err := store.db.Exec(`UPDATE users SET two_factor_secret = NULL, two_factor_recovery_codes = NULL, two_factor_confirmed_at = NULL,
			two_factor_last_used_step = NULL, updated_at = ? WHERE email = ?`, nowDB(), strings.ToLower(args[1]))
		if n, _ := res.RowsAffected(); err != nil || n == 0 {
			fmt.Fprintln(os.Stderr, "No account with that email.")
			return 1
		}
		fmt.Println("Authenticator removed; the next login will use an emailed code.")
		return 0
	}
	fmt.Fprintln(os.Stderr, "unknown command "+args[0]+" (create-admin, reset-authenticator, check-mail)")
	return 2
}
