package main

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Cross-machine copy/move, run in the background like RelayTransferJob,
// but streamed: bytes go from the source agent's download straight into
// the destination agent's upload, never to the hub's disk.

type nullStr struct{ sql.NullString }

func logf(format string, args ...any) { log.Printf(format, args...) }

type RelayManager struct {
	app    *App
	mu     sync.Mutex
	totals map[string]int64 // destination path -> expected bytes, for progress
}

func newRelayManager(app *App) *RelayManager {
	return &RelayManager{app: app, totals: map[string]int64{}}
}

const maxFolderDepth = 64

// RecoverStale marks relays left running by a previous process as failed,
// like RelayTransferJob::failed(). Nothing is deleted.
func (rm *RelayManager) RecoverStale() {
	rm.app.store.db.Exec(`UPDATE relay_transfers SET status = 'error',
		error = 'The transfer stopped unexpectedly before it finished. Nothing was deleted - try it again.',
		completed_at = ?, updated_at = ? WHERE status IN ('queued', 'downloading', 'uploading')`, nowDB(), nowDB())
}

func (rm *RelayManager) Total(dest string) int64 {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	return rm.totals[dest]
}

func (rm *RelayManager) Start(src *Machine, srcPath string, dst *Machine, dstPath string, copy, isDir bool, userID int64) (int64, error) {
	kind := "move"
	if copy {
		kind = "copy"
	}
	now := nowDB()
	var uid any
	if userID > 0 {
		uid = userID
	}
	res, err := rm.app.store.db.Exec(`INSERT INTO relay_transfers (source_machine_id, destination_machine_id, source_path, destination_path,
		kind, status, user_id, is_directory, created_at, updated_at) VALUES (?, ?, ?, ?, ?, 'queued', ?, ?, ?, ?)`,
		src.ID, dst.ID, srcPath, dstPath, kind, uid, isDir, now, now)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	go rm.run(id, src, srcPath, dst, dstPath, kind, isDir)
	return id, nil
}

// RunSync runs a relay in the caller's goroutine (sync rules use this).
func (rm *RelayManager) RunSync(src *Machine, srcPath string, dst *Machine, dstPath string) error {
	now := nowDB()
	res, err := rm.app.store.db.Exec(`INSERT INTO relay_transfers (source_machine_id, destination_machine_id, source_path, destination_path,
		kind, status, is_directory, created_at, updated_at) VALUES (?, ?, ?, ?, 'copy', 'queued', 0, ?, ?)`,
		src.ID, dst.ID, srcPath, dstPath, now, now)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	return rm.run(id, src, srcPath, dst, dstPath, "copy", false)
}

type relayJob struct {
	rm       *RelayManager
	id       int64
	src, dst *Machine
	sa, da   *AgentClient
	bytes    atomic.Int64
	ctx      context.Context
}

func (rm *RelayManager) run(id int64, src *Machine, srcPath string, dst *Machine, dstPath, kind string, isDir bool) (err error) {
	db := rm.app.store.db
	j := &relayJob{rm: rm, id: id, src: src, dst: dst, ctx: context.Background()}
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("internal error: %v", p)
		}
		if err != nil {
			log.Printf("relay #%d failed: %v", id, err)
			db.Exec(`UPDATE relay_transfers SET status = 'error', error = ?, bytes_transferred = ?, completed_at = ?, updated_at = ? WHERE id = ?`,
				err.Error(), j.bytes.Load(), nowDB(), nowDB(), id)
		}
	}()
	if j.sa, err = rm.app.store.Agent(src); err != nil {
		return err
	}
	if j.da, err = rm.app.store.Agent(dst); err != nil {
		return err
	}
	db.Exec(`UPDATE relay_transfers SET status = 'downloading', started_at = ?, updated_at = ? WHERE id = ?`, nowDB(), nowDB(), id)

	// Progress for the transfers panel, once a second.
	stop := make(chan struct{})
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				db.Exec(`UPDATE relay_transfers SET bytes_transferred = ?, updated_at = ? WHERE id = ?`, j.bytes.Load(), nowDB(), id)
			}
		}
	}()
	defer close(stop)

	if isDir {
		err = j.relayFolder(srcPath, dstPath)
	} else {
		err = j.relayFile(srcPath, dstPath)
	}
	if err != nil {
		return err
	}
	// Only reached when every file arrived, so a move never deletes the
	// source of a partial copy.
	if kind == "move" {
		if err = j.sa.Delete(j.ctx, srcPath); err != nil {
			return fmt.Errorf("copied, but the original couldn't be deleted: %w", err)
		}
	}
	db.Exec(`UPDATE relay_transfers SET status = 'done', bytes_transferred = ?, completed_at = ?, updated_at = ? WHERE id = ?`,
		j.bytes.Load(), nowDB(), nowDB(), id)
	return nil
}

type countingReader struct {
	r io.Reader
	n *atomic.Int64
}

func (c countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n.Add(int64(n))
	return n, err
}

func splitDir(path, sep string) (dir, name string) {
	norm := strings.ReplaceAll(path, `\`, "/")
	i := strings.LastIndex(strings.TrimRight(norm, "/"), "/")
	if i < 0 {
		return "", path
	}
	dir, name = path[:i], path[i+1:]
	if dir == "" {
		dir = "/"
	}
	if sep == `\` {
		dir = strings.ReplaceAll(dir, "/", `\`)
		if strings.HasSuffix(dir, ":") {
			dir += `\` // "C:" alone means the current folder on C:
		}
	}
	return dir, name
}

// relayFile streams one file. The destination must end up the same size
// as the source said it was, or the relay fails.
func (j *relayJob) relayFile(srcPath, dstPath string) error {
	st, err := j.sa.Open(j.ctx, "/api/download", srcPath)
	if err != nil {
		return err
	}
	defer st.Close()
	want := int64(-1)
	if cl := st.Resp.Header.Get("Content-Length"); cl != "" {
		want, _ = strconv.ParseInt(cl, 10, 64)
	}
	if want > 0 {
		j.rm.mu.Lock()
		j.rm.totals[dstPath] = want
		j.rm.mu.Unlock()
		defer func() {
			j.rm.mu.Lock()
			delete(j.rm.totals, dstPath)
			j.rm.mu.Unlock()
		}()
	}
	j.rm.app.store.db.Exec(`UPDATE relay_transfers SET status = 'uploading', updated_at = ? WHERE id = ?`, nowDB(), j.id)
	dir, name := splitDir(dstPath, j.dst.Separator())
	before := j.bytes.Load()
	entry, err := j.da.Upload(j.ctx, dir, name, countingReader{r: st, n: &j.bytes})
	if err != nil {
		return err
	}
	sent := j.bytes.Load() - before
	if want >= 0 && sent != want {
		return fmt.Errorf("%s: only %d of %d bytes arrived", name, sent, want)
	}
	if entry.Size != nil && *entry.Size != sent {
		return fmt.Errorf("%s: the destination reports %d bytes, %d were sent", name, *entry.Size, sent)
	}
	return nil
}

// relayFolder recreates the source folder at the destination, refusing to
// merge into one that already exists, same as RelayTransferJob.
func (j *relayJob) relayFolder(srcPath, dstRoot string) error {
	sep := j.dst.Separator()
	if j.exists(dstRoot) {
		return fmt.Errorf("%s already exists on %s.", dstRoot, j.dst.Name)
	}
	if err := j.da.Mkdir(j.ctx, dstRoot); err != nil {
		return fmt.Errorf("Couldn't create %s on %s (%v). Pick a folder the agent there is allowed to write to.", dstRoot, j.dst.Name, err)
	}
	type item struct {
		src, dst string
		depth    int
	}
	queue := []item{{srcPath, dstRoot, 0}}
	for len(queue) > 0 {
		it := queue[0]
		queue = queue[1:]
		if it.depth > maxFolderDepth {
			return fmt.Errorf("%s is nested too deeply (possible folder link loop).", it.src)
		}
		// Not skipped on error: an unreadable subfolder fails the whole
		// transfer, or a move would delete files that never arrived.
		entries, err := j.sa.List(j.ctx, it.src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			target := strings.TrimRight(it.dst, sep) + sep + e.Name
			if e.Type == "dir" {
				if err := j.da.Mkdir(j.ctx, target); err != nil {
					return err
				}
				queue = append(queue, item{e.Path, target, it.depth + 1})
				continue
			}
			if err := j.relayFile(e.Path, target); err != nil {
				return err
			}
		}
	}
	return nil
}

func (j *relayJob) exists(path string) bool {
	parent, name := splitDir(strings.TrimRight(path, `\/`), j.dst.Separator())
	entries, err := j.da.List(j.ctx, parent)
	if err != nil {
		return false // parent doesn't exist yet; mkdir creates it
	}
	for _, e := range entries {
		if e.Name == name || (j.dst.OS == "windows" && strings.EqualFold(e.Name, name)) {
			return true
		}
	}
	return false
}
