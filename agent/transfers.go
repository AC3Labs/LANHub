package main

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type TransferJob struct {
	mu *sync.Mutex

	ID          string  `json:"id"`
	Kind        string  `json:"kind"`
	Source      string  `json:"source"`
	Destination string  `json:"destination"`
	Status      string  `json:"status"`
	BytesDone   int64   `json:"bytes_done"`
	BytesTotal  int64   `json:"bytes_total"`
	Error       *string `json:"error"`
	CreatedAt   string  `json:"created_at"`
}

func (j *TransferJob) snapshot() TransferJob {
	j.mu.Lock()
	defer j.mu.Unlock()

	return TransferJob{
		ID: j.ID, Kind: j.Kind, Source: j.Source, Destination: j.Destination,
		Status: j.Status, BytesDone: j.BytesDone, BytesTotal: j.BytesTotal,
		Error: j.Error, CreatedAt: j.CreatedAt,
	}
}

type TransferQueue struct {
	mu    sync.RWMutex
	jobs  map[string]*TransferJob
	queue chan string
}

func newTransferQueue(workers int) *TransferQueue {
	q := &TransferQueue{
		jobs:  make(map[string]*TransferJob),
		queue: make(chan string, 1000),
	}

	for i := 0; i < workers; i++ {
		go q.worker()
	}

	return q
}

func (q *TransferQueue) worker() {
	for id := range q.queue {
		q.run(id)
	}
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (q *TransferQueue) Create(kind, source, destination string) *TransferJob {
	job := &TransferJob{
		mu:          &sync.Mutex{},
		ID:          randomHex(16),
		Kind:        kind,
		Source:      source,
		Destination: destination,
		Status:      "pending",
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
	}

	q.mu.Lock()
	q.jobs[job.ID] = job
	q.mu.Unlock()

	q.queue <- job.ID

	return job
}

func (q *TransferQueue) Get(id string) (*TransferJob, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	j, ok := q.jobs[id]
	return j, ok
}

func (q *TransferQueue) Recent(limit int) []TransferJob {
	q.mu.RLock()
	all := make([]*TransferJob, 0, len(q.jobs))
	for _, j := range q.jobs {
		all = append(all, j)
	}
	q.mu.RUnlock()

	sort.Slice(all, func(i, k int) bool { return all[i].CreatedAt > all[k].CreatedAt })

	if len(all) > limit {
		all = all[:limit]
	}

	out := make([]TransferJob, len(all))
	for i, j := range all {
		out[i] = j.snapshot()
	}

	return out
}

func (q *TransferQueue) run(id string) {
	job, ok := q.Get(id)
	if !ok {
		return
	}

	job.mu.Lock()
	job.Status = "running"
	job.mu.Unlock()

	total := directorySize(job.Source)
	job.mu.Lock()
	job.BytesTotal = total
	job.mu.Unlock()

	err := runCopyOrMove(job)

	job.mu.Lock()
	if err != nil {
		msg := err.Error()
		job.Status = "error"
		job.Error = &msg
		logActivity("TRANSFER", job.Kind+" "+job.Source+" -> "+job.Destination, 500, "-")
	} else {
		job.Status = "done"
		logActivity("TRANSFER", job.Kind+" "+job.Source+" -> "+job.Destination, 200, "-")
	}
	job.mu.Unlock()
}

func runCopyOrMove(job *TransferJob) error {
	info, err := os.Stat(job.Source)
	if err != nil {
		return err
	}

	if info.IsDir() {
		err := filepath.Walk(job.Source, func(p string, fi os.FileInfo, err error) error {
			if err != nil || fi.IsDir() {
				return err
			}

			rel, err := filepath.Rel(job.Source, p)
			if err != nil {
				return err
			}

			dest := filepath.Join(job.Destination, rel)

			return copyFileWithProgress(p, dest, job)
		})
		if err != nil {
			return err
		}

		if job.Kind == "move" {
			return os.RemoveAll(job.Source)
		}

		return nil
	}

	if err := copyFileWithProgress(job.Source, job.Destination, job); err != nil {
		return err
	}

	if job.Kind == "move" {
		return os.Remove(job.Source)
	}

	return nil
}

func copyFileWithProgress(source, destination string, job *TransferJob) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return err
	}

	src, err := os.Open(source)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.Create(destination)
	if err != nil {
		return err
	}
	defer dst.Close()

	buf := make([]byte, chunkSize)
	for {
		n, readErr := src.Read(buf)
		if n > 0 {
			if _, writeErr := dst.Write(buf[:n]); writeErr != nil {
				return writeErr
			}

			job.mu.Lock()
			job.BytesDone += int64(n)
			job.mu.Unlock()
		}

		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}

	if info, err := os.Stat(source); err == nil {
		os.Chtimes(destination, info.ModTime(), info.ModTime())
	}

	return nil
}
