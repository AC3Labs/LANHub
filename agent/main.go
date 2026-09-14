// Command lanhub-agent runs the LANHub file-access agent on a single
// machine. It implements the contract in docs/AGENT_API.md — see that
// file for the full wire format. This is a from-scratch reimplementation
// of the original Python/FastAPI agent as a single static binary, so
// installing it on a new machine needs nothing but the binary itself: no
// Python, no venv, no pip install. On Windows it can also register
// itself as a native service (see service_windows.go) — no NSSM needed.
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const agentVersion = "1.1.0"

var logFilePath string

func main() {
	if len(os.Args) > 1 {
		if handled := handleServiceCommand(os.Args[1]); handled {
			return
		}
	}

	if isWindowsService() {
		runWindowsService()
		return
	}

	cfg, srv, err := setup()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	log.Printf("LANHub agent %s listening on %s", agentVersion, srv.Addr)
	log.Fatal(serve(cfg, srv))
}

// setup loads config, wires the activity log, and builds (but does not
// start) the HTTP server — shared by the foreground run path and the
// Windows service Execute handler.
func setup() (*Config, *http.Server, error) {
	cfg, err := loadConfig(configPath())
	if err != nil {
		return nil, nil, err
	}

	setAllowedRoots(cfg.AllowedRoots)

	logFilePath = cfg.LogFile
	if logFilePath == "" {
		logFilePath = filepath.Join(filepath.Dir(configPath()), "agent.log")
	}

	rlog, err := newRotatingLog(logFilePath)
	if err != nil {
		return nil, nil, fmt.Errorf("could not open log file %s: %w", logFilePath, err)
	}
	activityLog = rlog

	queue := newTransferQueue(2)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", handleHealth)
	mux.HandleFunc("GET /api/drives", handleDrives)
	mux.HandleFunc("GET /api/netstats", handleNetstats)
	mux.HandleFunc("GET /api/list", handleList)
	mux.HandleFunc("GET /api/download", handleDownload(cfg))
	mux.HandleFunc("GET /api/preview", handlePreview(cfg))
	mux.HandleFunc("POST /api/upload", handleUpload(cfg))
	mux.HandleFunc("POST /api/mkdir", handleMkdir)
	mux.HandleFunc("POST /api/rename", handleRename)
	mux.HandleFunc("POST /api/move", handleMove)
	mux.HandleFunc("POST /api/copy", handleCopy)
	mux.HandleFunc("POST /api/delete", handleDelete)
	mux.HandleFunc("GET /api/search", handleSearch)
	mux.HandleFunc("POST /api/transfers", handleCreateTransfer(queue))
	mux.HandleFunc("GET /api/transfers/{id}", handleGetTransfer(queue))
	mux.HandleFunc("GET /api/transfers", handleListTransfers(queue))
	mux.HandleFunc("GET /api/activity", handleActivity)

	handler := loggingMiddleware(authMiddleware(cfg.Token, mux))
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))

	return cfg, &http.Server{Addr: addr, Handler: handler}, nil
}

// serve blocks until the server stops (error, or a clean Shutdown()).
func serve(cfg *Config, srv *http.Server) error {
	var err error
	if cfg.CertFile != "" && cfg.KeyFile != "" {
		err = srv.ListenAndServeTLS(cfg.CertFile, cfg.KeyFile)
	} else {
		err = srv.ListenAndServe()
	}

	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func shutdown(srv *http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}

func authMiddleware(token string, next http.Handler) http.Handler {
	expected := "Bearer " + token

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != expected {
			writeError(w, 401, "Invalid or missing agent token.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// loggingMiddleware writes one JSON line per request to the activity log
// — the same file /api/activity reads back — except the once-a-minute
// health poll, which would otherwise drown out everything else.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(rec, r)

		if r.URL.Path == "/api/health" {
			return
		}

		target := r.URL.Path
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}

		client := clientIP(r)
		logActivity(r.Method, target, rec.status, client)
	})
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
