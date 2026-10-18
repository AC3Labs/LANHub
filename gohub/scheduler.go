package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Background jobs, ported from the PHP hub's scheduler. Only started when
// LANHUB_SCHEDULER=true: test containers run with them off so a copy of the
// data never sends alerts or runs sync rules against real machines.

func (app *App) startScheduler(ctx context.Context) {
	go every(ctx, time.Minute, app.checkHealth)
	go every(ctx, 15*time.Minute, app.checkDiskSpace)
	go every(ctx, time.Minute, app.runSyncRules)
	log.Printf("scheduler on: health every 1m, disk every 15m, sync rules every 1m")
}

func every(ctx context.Context, d time.Duration, fn func(context.Context)) {
	var running sync.Mutex // withoutOverlapping()
	t := time.NewTicker(d)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if running.TryLock() {
				go func() {
					defer running.Unlock()
					fn(ctx)
				}()
			}
		}
	}
}

func (app *App) notify(msg string) {
	if app.cfg.NtfyTopic == "" {
		return
	}
	req, _ := http.NewRequest(http.MethodPost, "https://ntfy.sh/"+app.cfg.NtfyTopic, strings.NewReader(msg))
	req.Header.Set("Title", "LANHub")
	req.Header.Set("Content-Type", "text/plain")
	c := &http.Client{Timeout: 5 * time.Second}
	if resp, err := c.Do(req); err != nil {
		log.Printf("ntfy.sh notification failed: %v", err)
	} else {
		resp.Body.Close()
	}
}

// checkHealth: online/offline status for every machine, alerting only on a
// change (CheckMachineHealth).
func (app *App) checkHealth(ctx context.Context) {
	machines, _ := app.store.AllMachines()
	forEachMachine(machines, func(m *Machine) {
		wasOnline := m.Status == "online"
		ag, err := app.store.Agent(m)
		if err == nil {
			_, err = ag.Health(ctx)
		}
		app.store.SetMachineStatus(m.ID, err == nil)
		switch {
		case err == nil && !wasOnline:
			app.notify(m.Name + " is back online.")
		case err != nil && wasOnline:
			log.Printf("machine %s went offline: %v", m.Name, err)
			app.notify(m.Name + " just went offline: " + err.Error())
		}
	})
}

const lowSpacePercent = 10.0

// checkDiskSpace records a reading per machine and alerts once when free
// space drops under 10% (CheckDiskSpace). Keeps 30 days of readings.
func (app *App) checkDiskSpace(ctx context.Context) {
	machines, _ := app.store.AllMachines()
	forEachMachine(machines, func(m *Machine) {
		ag, err := app.store.Agent(m)
		if err != nil {
			return
		}
		drives, err := ag.Drives(ctx)
		if err != nil {
			return
		}
		var free, total int64
		for _, d := range drives {
			free += d.Free
			total += d.Total
		}
		if total <= 0 {
			return
		}
		app.store.db.Exec(`INSERT INTO disk_readings (machine_id, free_bytes, total_bytes, recorded_at) VALUES (?, ?, ?, ?)`, m.ID, free, total, nowDB())
		pct := float64(free) / float64(total) * 100
		if pct < lowSpacePercent {
			if m.LowSpaceAt == nil {
				app.store.db.Exec(`UPDATE machines SET low_space_alerted_at = ? WHERE id = ?`, nowDB(), m.ID)
				app.notify(fmt.Sprintf("%s is low on disk space: %.1f%% free.", m.Name, pct))
			}
		} else if m.LowSpaceAt != nil {
			app.store.db.Exec(`UPDATE machines SET low_space_alerted_at = NULL WHERE id = ?`, m.ID)
		}
	})
	app.store.db.Exec(`DELETE FROM disk_readings WHERE recorded_at < ?`, time.Now().UTC().Add(-30*24*time.Hour).Format(dbTime))
	app.pruneSessions()
}

// ---- sync rules (RunSyncRules) ----

type fileInfo struct {
	Relative, Path, Modified string
	Size                     *int64
}

// listFilesRecursive walks a tree with /api/list, breadth first, capped at
// maxEntries, with relative paths normalised to "/" (AgentClient).
func listFilesRecursive(ctx context.Context, ag *AgentClient, root, sep string, maxEntries int) map[string]fileInfo {
	out := map[string]fileInfo{}
	queue := []string{root}
	prefix := strings.TrimRight(root, sep)
	for len(queue) > 0 && len(out) < maxEntries {
		dir := queue[0]
		queue = queue[1:]
		entries, err := ag.List(ctx, dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.Type == "dir" {
				queue = append(queue, e.Path)
				continue
			}
			rel := strings.TrimLeft(strings.TrimPrefix(e.Path, prefix), sep)
			rel = strings.ReplaceAll(rel, sep, "/")
			out[rel] = fileInfo{Relative: rel, Path: e.Path, Size: e.Size, Modified: e.Modified}
			if len(out) >= maxEntries {
				break
			}
		}
	}
	return out
}

func sameSize(a, b *int64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func (app *App) runSyncRules(ctx context.Context) {
	rows, err := app.store.db.Query(`SELECT id, source_machine_id, destination_machine_id, source_path, destination_path, direction, interval_minutes, last_run_at
		FROM sync_rules WHERE enabled = 1`)
	if err != nil {
		return
	}
	type rule struct {
		id, src, dst       int64
		srcPath, dstPath   string
		direction          string
		interval           int
		lastRun            *time.Time
	}
	var rules []rule
	for rows.Next() {
		var r rule
		var last nullStr
		if rows.Scan(&r.id, &r.src, &r.dst, &r.srcPath, &r.dstPath, &r.direction, &r.interval, &last) == nil {
			r.lastRun = parseDBTime(last.NullString)
			rules = append(rules, r)
		}
	}
	rows.Close()
	for _, r := range rules {
		if r.lastRun != nil && time.Now().Before(r.lastRun.Add(time.Duration(r.interval)*time.Minute)) {
			continue
		}
		conflict, err := app.runRule(ctx, r.src, r.dst, r.srcPath, r.dstPath, r.direction)
		if err != nil {
			log.Printf("sync rule #%d failed: %v", r.id, err)
			app.store.db.Exec(`UPDATE sync_rules SET last_run_at = ?, last_error = ?, updated_at = ? WHERE id = ?`, nowDB(), err.Error(), nowDB(), r.id)
			continue
		}
		app.store.db.Exec(`UPDATE sync_rules SET last_run_at = ?, has_conflict = ?, last_error = NULL, updated_at = ? WHERE id = ?`, nowDB(), conflict, nowDB(), r.id)
	}
}

// runRule copies new or changed files; mirror rules also delete extras. A
// destination file that changed since this run listed it is never
// overwritten or deleted: that's a conflict instead.
func (app *App) runRule(ctx context.Context, srcID, dstID int64, srcPath, dstPath, direction string) (bool, error) {
	src, err := app.store.Machine(srcID)
	if err != nil {
		return false, err
	}
	dst, err := app.store.Machine(dstID)
	if err != nil {
		return false, err
	}
	sa, err := app.store.Agent(src)
	if err != nil {
		return false, err
	}
	da, err := app.store.Agent(dst)
	if err != nil {
		return false, err
	}
	sep := dst.Separator()
	srcFiles := listFilesRecursive(ctx, sa, srcPath, src.Separator(), 5000)
	dstFiles := listFilesRecursive(ctx, da, dstPath, sep, 5000)
	fresh := map[string]map[string]Entry{}
	freshEntry := func(path string) *Entry {
		dir := path
		if i := strings.LastIndex(path, sep); i > 0 {
			dir = strings.TrimRight(path[:i], sep)
		}
		if dir == "" {
			dir = sep
		}
		if _, ok := fresh[dir]; !ok {
			fresh[dir] = map[string]Entry{}
			if list, err := da.List(ctx, dir); err == nil {
				for _, e := range list {
					fresh[dir][e.Path] = e
				}
			}
		}
		if e, ok := fresh[dir][path]; ok {
			return &e
		}
		return nil
	}
	conflict := false
	for rel, sf := range srcFiles {
		target := strings.TrimRight(dstPath, sep) + sep + strings.ReplaceAll(rel, "/", sep)
		existing, has := dstFiles[rel]
		if has && sameSize(existing.Size, sf.Size) && existing.Modified == sf.Modified {
			continue
		}
		if has {
			if f := freshEntry(target); f != nil && (!sameSize(f.Size, existing.Size) || f.Modified != existing.Modified) {
				conflict = true
				continue
			}
		}
		if src.ID == dst.ID {
			dir, _ := splitDir(target, sep)
			da.Mkdir(ctx, dir)
			if has {
				da.Delete(ctx, target)
			}
			if err := sa.Copy(ctx, sf.Path, target); err != nil {
				return conflict, err
			}
			continue
		}
		if err := app.relays.RunSync(src, sf.Path, dst, target); err != nil {
			return conflict, err
		}
	}
	if direction == "mirror" {
		for rel, df := range dstFiles {
			if _, ok := srcFiles[rel]; ok {
				continue
			}
			if f := freshEntry(df.Path); f != nil && (!sameSize(f.Size, df.Size) || f.Modified != df.Modified) {
				conflict = true
				continue
			}
			if err := da.Delete(ctx, df.Path); err != nil {
				return conflict, err
			}
		}
	}
	return conflict, nil
}
