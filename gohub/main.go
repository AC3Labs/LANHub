// Command lanhub-hub is the Go version of the LANHub hub, built for a
// side-by-side evaluation against the PHP hub. It uses the PHP hub's
// database and APP_KEY as they are; see docs/GO_HUB_PLAN.md.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println(hubVersion)
		return
	}
	cfg := loadConfig()
	// A newer hub installed from Settings → Updates takes over from here.
	if bin := stagedHubToRun(hubUpdateDir(cfg.DBPath), hubVersion); bin != "" {
		if err := execStaged(bin); err != nil {
			log.Printf("update: couldn't start the installed update: %v", err)
		}
	}
	if err := cfg.fillSecrets(); err != nil {
		log.Fatal(err)
	}
	enc, err := NewEncrypter(cfg.AppKey)
	if err != nil {
		log.Fatalf("APP_KEY: %v", err)
	}
	store, err := openStore(cfg.DBPath, enc)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	if len(os.Args) > 1 {
		os.Exit(runCLI(os.Args[1:], store))
	}
	app, err := newApp(cfg, store)
	if err != nil {
		log.Fatalf("startup: %v", err)
	}
	app.relays.RecoverStale()
	app.pruneSessions()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go app.upd.markHealthy(ctx)
	go app.upd.watchMedia(ctx)
	if cfg.Scheduler {
		app.startScheduler(ctx)
		go app.upd.autoCheck(ctx)
	} else {
		log.Printf("scheduler off (LANHUB_SCHEDULER=false): no health checks, alerts or sync rules")
	}

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           app.routes(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		// No overall read/write timeouts: uploads and downloads stream for
		// as long as they keep moving (stall timeouts handle the rest).
	}
	var restartInto string
	go func() {
		select {
		case <-ctx.Done():
		case restartInto = <-app.upd.restartTo:
		}
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	log.Printf("LANHub %s listening on %s (db %s)", cfg.Version, cfg.Listen, cfg.DBPath)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	if restartInto != "" {
		store.db.Close()
		err := execStaged(restartInto)
		// Exec only returns on failure. Exiting lets Docker restart the
		// container, and start-up tries the staged hub again.
		log.Fatalf("update: couldn't restart into the new hub: %v", err)
	}
	os.Exit(0)
}
