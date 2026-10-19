package main

import (
	"crypto/rand"
	"database/sql"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"strings"
)

// A brand-new install starts from an empty volume: these create the
// database, the encryption key and the LANHub Media secret the first time,
// and reuse them on every start after that.

// schemaSQL is the PHP hub's schema (as its migrations left it), so a
// database made here can still be opened by either hub.
//
//go:embed schema.sql
var schemaSQL string

// createSchema fills an empty database. It refuses a database that already
// has tables of its own.
func createSchema(db *sql.DB) error {
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table'`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, stmt := range strings.Split(schemaSQL, ";\n") {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("creating the database: %w", err)
		}
	}
	log.Printf("new install: created an empty LANHub database")
	return tx.Commit()
}

// secretFile returns the secret saved at path, creating it with make() if
// there isn't one yet. The file is only readable by the hub's own user.
func secretFile(path string, make func() string) (string, error) {
	b, err := os.ReadFile(path)
	if err == nil && strings.TrimSpace(string(b)) != "" {
		return strings.TrimSpace(string(b)), nil
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	v := make()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) { // another start won the race; use theirs
		return secretFile(path, make)
	}
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(v + "\n"); err != nil {
		f.Close()
		return "", err
	}
	return v, f.Close()
}

func newAppKey() string {
	b := make([]byte, 32)
	rand.Read(b)
	return "base64:" + base64.StdEncoding.EncodeToString(b)
}

func newMediaSecret() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}
