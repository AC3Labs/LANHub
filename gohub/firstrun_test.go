package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestNewInstallCreatesDatabaseAndKeys(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{DBPath: filepath.Join(dir, "database.sqlite"), MediaURL: "http://media:8960", MediaSecretFile: filepath.Join(dir, "media", "secret")}
	os.Mkdir(filepath.Join(dir, "media"), 0o700)
	if err := cfg.fillSecrets(); err != nil {
		t.Fatal(err)
	}
	enc, err := NewEncrypter(cfg.AppKey)
	if err != nil {
		t.Fatalf("generated key unusable: %v", err)
	}
	if len(cfg.MediaSecret) != 64 {
		t.Fatalf("media secret %q", cfg.MediaSecret)
	}
	st, err := openStore(cfg.DBPath, enc)
	if err != nil {
		t.Fatalf("open new database: %v", err)
	}
	if st.UserCount() != 0 {
		t.Fatal("new database isn't empty")
	}
	st.db.Close()

	// A restart reuses the same key, secret and database.
	again := Config{DBPath: cfg.DBPath, MediaURL: cfg.MediaURL, MediaSecretFile: cfg.MediaSecretFile}
	if err := again.fillSecrets(); err != nil {
		t.Fatal(err)
	}
	if again.AppKey != cfg.AppKey || again.MediaSecret != cfg.MediaSecret {
		t.Fatal("secrets changed on restart")
	}
	if fi, _ := os.Stat(filepath.Join(dir, "app.key")); fi.Mode().Perm() != 0o600 {
		t.Fatalf("app.key mode %v", fi.Mode().Perm())
	}
	if _, err := openStore(cfg.DBPath, enc); err != nil {
		t.Fatalf("reopen: %v", err)
	}

	// An existing database that isn't LANHub's is still refused, not filled.
	other := filepath.Join(dir, "other.sqlite")
	raw, _ := sql.Open("sqlite", "file:"+other)
	raw.Exec(`CREATE TABLE notes (id integer)`)
	raw.Close()
	if _, err := openStore(other, enc); err == nil {
		t.Fatal("opened a database that isn't LANHub's")
	}

	// A key that was set wins over generating one.
	set := Config{DBPath: filepath.Join(t.TempDir(), "d.sqlite"), AppKey: cfg.AppKey}
	set.fillSecrets()
	if _, err := os.Stat(filepath.Join(filepath.Dir(set.DBPath), "app.key")); err == nil {
		t.Fatal("wrote app.key although APP_KEY was set")
	}
}
