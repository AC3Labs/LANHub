package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
)

// `lanhub-agent configure [--token T] [--port N] [--cert F --key F]` writes
// config.json next to the program (or LANHUB_AGENT_CONFIG): the installers
// use it, so they don't each need their own JSON handling. Settings not
// given are kept; a new file starts from the defaults.
func runConfigure(args []string) error {
	fs := flag.NewFlagSet("configure", flag.ContinueOnError)
	token := fs.String("token", "", "agent token, as set for this machine in the hub")
	port := fs.Int("port", 0, "port to listen on")
	cert := fs.String("cert", "", "TLS certificate file")
	key := fs.String("key", "", "TLS key file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	path := configPath()
	cfg := map[string]any{"token": "", "host": "0.0.0.0", "port": 8765, "allowed_roots": []string{}, "log_file": "", "throttle_kbps": 0, "cert_file": "", "key_file": ""}
	if raw, err := os.ReadFile(path); err == nil {
		raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return fmt.Errorf("%s isn't valid JSON (%v); fix or delete it first", path, err)
		}
	}
	if t := strings.TrimSpace(*token); t != "" {
		cfg["token"] = t
	}
	if *port > 0 {
		cfg["port"] = *port
	}
	if *cert != "" && *key != "" {
		cfg["cert_file"], cfg["key_file"] = *cert, *key
	}
	out, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(path, append(out, '\n'), 0o600); err != nil {
		return err
	}
	if t, _ := cfg["token"].(string); t == "" || t == "REPLACE_WITH_A_LONG_RANDOM_TOKEN" {
		fmt.Printf("Saved %s. It has no token yet: run configure again with --token.\n", path)
		return nil
	}
	fmt.Printf("Saved %s.\n", path)
	return nil
}

// hasToken reports whether config.json has a real token (installers start
// the service only then).
func hasToken() bool {
	cfg, err := loadConfig(configPath())
	return err == nil && cfg.Token != "REPLACE_WITH_A_LONG_RANDOM_TOKEN"
}
