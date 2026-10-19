package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	AppKey    string
	AppName   string
	DBPath    string
	Listen    string
	Scheduler bool   // health/disk checks and sync rules; off by default
	NtfyTopic string // empty = no push alerts
	AppURL    string // used in invitation links when set
	Version   string
	DirectOff bool // LANHUB_DIRECT_TRANSFERS=false: always relay through the hub

	MediaURL        string // LANHub Media service, e.g. http://192.168.0.70:8960
	MediaSecret     string // shared with it
	MediaSourceBase string // this hub's address as the media service reaches it
	MediaSecretFile string // where to keep a generated MEDIA_SECRET (shared volume)
}

// fillSecrets generates what a new install needs and didn't set: APP_KEY
// is kept next to the database, MEDIA_SECRET in MEDIA_SECRET_FILE (a
// volume LANHub Media reads too). Set values always win.
func (c *Config) fillSecrets() error {
	if c.AppKey == "" {
		k, err := secretFile(filepath.Join(filepath.Dir(c.DBPath), "app.key"), newAppKey)
		if err != nil {
			return fmt.Errorf("APP_KEY isn't set and one couldn't be saved: %w", err)
		}
		c.AppKey = k
	}
	if c.MediaSecret == "" && c.MediaSecretFile != "" && c.MediaURL != "" {
		s, err := secretFile(c.MediaSecretFile, newMediaSecret)
		if err != nil {
			return fmt.Errorf("MEDIA_SECRET isn't set and one couldn't be saved: %w", err)
		}
		c.MediaSecret = s
	}
	return nil
}

// loadConfig reads the same hub.env the PHP hub uses (APP_KEY, NTFY_TOPIC,
// APP_URL), then lets LANHUB_* environment variables override.
func loadConfig() Config {
	env := map[string]string{}
	if path := getenv("LANHUB_ENV_FILE", "/app/hub.env"); path != "" {
		env = readEnvFile(path)
	}
	for _, kv := range os.Environ() {
		k, v, _ := strings.Cut(kv, "=")
		env[k] = v
	}
	get := func(k, def string) string {
		if v, ok := env[k]; ok && v != "" {
			return v
		}
		return def
	}
	sched, _ := strconv.ParseBool(get("LANHUB_SCHEDULER", "false"))
	direct, _ := strconv.ParseBool(get("LANHUB_DIRECT_TRANSFERS", "true"))
	listen := get("LANHUB_LISTEN", "")
	if listen == "" {
		listen = ":" + get("PORT", "8000") // the name the PHP hub's compose file used
	}
	return Config{
		AppKey:    get("APP_KEY", ""),
		AppName:   get("APP_NAME", "LANHub"),
		DBPath:    get("LANHUB_DB", "/data/database.sqlite"),
		Listen:    listen,
		Scheduler: sched,
		NtfyTopic: get("NTFY_TOPIC", ""),
		AppURL:    strings.TrimRight(get("APP_URL", ""), "/"),
		Version:   hubVersion,
		DirectOff: !direct,

		MediaURL:        get("MEDIA_URL", ""),
		MediaSecret:     get("MEDIA_SECRET", ""),
		MediaSecretFile: get("MEDIA_SECRET_FILE", ""),
		MediaSourceBase: get("MEDIA_SOURCE_BASE", ""),
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// readEnvFile understands the subset of dotenv syntax Laravel's .env uses:
// KEY=value, KEY="quoted value", comments, blank lines.
func readEnvFile(path string) map[string]string {
	out := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
			v = v[1 : len(v)-1]
		} else if i := strings.Index(v, " #"); i >= 0 {
			v = strings.TrimSpace(v[:i])
		}
		out[strings.TrimSpace(k)] = v
	}
	return out
}
