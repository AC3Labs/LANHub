package main

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// The Go hub reads and writes the PHP hub's own SQLite schema unchanged.
// The only addition is go_sessions, its own session table.

const dbTime = "2006-01-02 15:04:05" // how Laravel stores datetimes (UTC)

type Store struct {
	db  *sql.DB
	enc *Encrypter
}

func openStore(path string, enc *Encrypter) (*Store, error) {
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	if err := db.Ping(); err != nil {
		return nil, err
	}
	if err := createSchema(db); err != nil {
		return nil, err
	}
	var users int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('users','machines','machine_user','relay_transfers','sync_rules','settings')`).Scan(&users); err != nil || users != 6 {
		return nil, fmt.Errorf("%s doesn't look like a LANHub database (missing tables)", path)
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS go_sessions (
		id TEXT PRIMARY KEY,
		user_id INTEGER,
		data TEXT NOT NULL DEFAULT '{}',
		csrf TEXT NOT NULL,
		expires_at INTEGER NOT NULL,
		remember INTEGER NOT NULL DEFAULT 0
	)`)
	if err != nil {
		return nil, err
	}
	return &Store{db: db, enc: enc}, nil
}

func nowDB() string { return time.Now().UTC().Format(dbTime) }

// parseTimeText reads a datetime column scanned into a string. The driver
// returns DATETIME columns as RFC 3339; text written by PHP is "Y-m-d H:i:s".
func parseTimeText(s string) (time.Time, bool) {
	t := parseDBTime(sql.NullString{String: s, Valid: s != ""})
	if t == nil {
		return time.Time{}, false
	}
	return *t, true
}

func parseDBTime(s sql.NullString) *time.Time {
	if !s.Valid || s.String == "" {
		return nil
	}
	for _, layout := range []string{dbTime, time.RFC3339Nano, "2006-01-02T15:04:05.000000Z"} {
		if t, err := time.Parse(layout, s.String); err == nil {
			return &t
		}
	}
	return nil
}

// ---- users ----

type User struct {
	ID                    int64
	Name                  string
	Email                 string
	PasswordHash          string
	IsAdmin               bool
	InviteToken           sql.NullString
	InviteExpires         *time.Time
	TwoFactorCode         sql.NullString
	TwoFactorExpires      *time.Time
	TwoFactorSecretEnc    sql.NullString
	TwoFactorRecoveryEnc  sql.NullString
	TwoFactorConfirmedAt  *time.Time
	TwoFactorLastUsedStep int64
}

func (u *User) IsPending() bool       { return u.InviteToken.Valid }
func (u *User) HasAuthenticator() bool { return u.TwoFactorSecretEnc.Valid && u.TwoFactorConfirmedAt != nil }

const userCols = `id, name, email, password, is_admin, invite_token, invite_token_expires_at, two_factor_code,
	two_factor_expires_at, two_factor_secret, two_factor_recovery_codes, two_factor_confirmed_at, coalesce(two_factor_last_used_step, 0)`

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	var u User
	var inviteExp, tfExp, tfConfirmed sql.NullString
	err := row.Scan(&u.ID, &u.Name, &u.Email, &u.PasswordHash, &u.IsAdmin, &u.InviteToken, &inviteExp, &u.TwoFactorCode,
		&tfExp, &u.TwoFactorSecretEnc, &u.TwoFactorRecoveryEnc, &tfConfirmed, &u.TwoFactorLastUsedStep)
	if err != nil {
		return nil, err
	}
	u.InviteExpires, u.TwoFactorExpires, u.TwoFactorConfirmedAt = parseDBTime(inviteExp), parseDBTime(tfExp), parseDBTime(tfConfirmed)
	return &u, nil
}

func (s *Store) UserByID(id int64) (*User, error) {
	return scanUser(s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE id = ?`, id))
}

func (s *Store) UserByEmail(email string) (*User, error) {
	return scanUser(s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE email = ?`, email))
}

func (s *Store) Users() ([]*User, error) {
	rows, err := s.db.Query(`SELECT ` + userCols + ` FROM users ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) UserCount() int {
	var n int
	s.db.QueryRow(`SELECT count(*) FROM users`).Scan(&n)
	return n
}

// ---- machines ----

type Machine struct {
	ID         int64
	Name       string
	Host       string
	Port       int
	OS         string
	TokenEnc   string `json:"-"` // never sent to a browser
	Color      string
	SortOrder  int
	UseTLS     bool
	Status     string
	LastSeenAt *time.Time
	Notes      string
	LowSpaceAt *time.Time
	Role       string // the current user's role, when loaded for a user
}

func (m *Machine) Separator() string {
	if m.OS == "windows" {
		return `\`
	}
	return "/"
}

func (m *Machine) RootPath() string {
	if m.OS == "windows" {
		return `C:\`
	}
	return "/"
}

func (m *Machine) BaseURL() string {
	scheme := "http"
	if m.UseTLS {
		scheme = "https"
	}
	host := m.Host
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	return fmt.Sprintf("%s://%s:%d", scheme, host, m.Port)
}

func (m *Machine) Initial() string {
	for _, r := range m.Name {
		return strings.ToUpper(string(r))
	}
	return "?"
}

const machineCols = `m.id, m.name, m.host, m.port, m.os, m.agent_token, m.color, m.sort_order, m.use_tls, m.status,
	m.last_seen_at, coalesce(m.notes, ''), m.low_space_alerted_at`

func scanMachine(row interface{ Scan(...any) error }, withRole bool) (*Machine, error) {
	var m Machine
	var lastSeen, lowSpace sql.NullString
	dest := []any{&m.ID, &m.Name, &m.Host, &m.Port, &m.OS, &m.TokenEnc, &m.Color, &m.SortOrder, &m.UseTLS, &m.Status, &lastSeen, &m.Notes, &lowSpace}
	if withRole {
		dest = append(dest, &m.Role)
	}
	if err := row.Scan(dest...); err != nil {
		return nil, err
	}
	m.LastSeenAt, m.LowSpaceAt = parseDBTime(lastSeen), parseDBTime(lowSpace)
	return &m, nil
}

func (s *Store) Machine(id int64) (*Machine, error) {
	return scanMachine(s.db.QueryRow(`SELECT `+machineCols+` FROM machines m WHERE m.id = ?`, id), false)
}

func (s *Store) AllMachines() ([]*Machine, error) {
	return s.queryMachines(`SELECT `+machineCols+` FROM machines m ORDER BY m.name`, false)
}

// AccessibleMachines mirrors Machine::accessibleTo(): only machines the
// user has a machine_user row for. Admins get no implicit access.
func (s *Store) AccessibleMachines(userID int64, order string) ([]*Machine, error) {
	orderBy := "m.sort_order, m.name"
	if order == "name" {
		orderBy = "m.name"
	}
	return s.queryMachines(`SELECT `+machineCols+`, mu.role FROM machines m JOIN machine_user mu ON mu.machine_id = m.id
		WHERE mu.user_id = ? ORDER BY `+orderBy, true, userID)
}

func (s *Store) queryMachines(q string, withRole bool, args ...any) ([]*Machine, error) {
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Machine
	for rows.Next() {
		m, err := scanMachine(rows, withRole)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Role returns "read_only", "full", or "" (no access).
func (s *Store) Role(userID, machineID int64) string {
	var role string
	if err := s.db.QueryRow(`SELECT role FROM machine_user WHERE user_id = ? AND machine_id = ?`, userID, machineID).Scan(&role); err != nil {
		return ""
	}
	return role
}

// CanAccess mirrors User::canAccess(): any role allows read_only; "full"
// needs the full role.
func (s *Store) CanAccess(userID, machineID int64, atLeast string) bool {
	role := s.Role(userID, machineID)
	if role == "" {
		return false
	}
	return atLeast != "full" || role == "full"
}

func (s *Store) SetMachineStatus(id int64, online bool) {
	if online {
		s.db.Exec(`UPDATE machines SET status = 'online', last_seen_at = ?, updated_at = ? WHERE id = ?`, nowDB(), nowDB(), id)
	} else {
		s.db.Exec(`UPDATE machines SET status = 'offline', updated_at = ? WHERE id = ?`, nowDB(), id)
	}
}

func (s *Store) AgentToken(m *Machine) (string, error) {
	return s.enc.DecryptString(m.TokenEnc)
}

// ---- settings ----

func (s *Store) Setting(key string) string {
	var enc sql.NullString
	if err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&enc); err != nil || !enc.Valid {
		return ""
	}
	v, err := s.enc.DecryptString(enc.String)
	if err != nil {
		return ""
	}
	return v
}

var errNotFound = errors.New("not found")

func isNoRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }
