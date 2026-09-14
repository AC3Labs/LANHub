package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

const (
	logMaxBytes    = 5 * 1024 * 1024
	logBackupCount = 3
)

type ActivityEntry struct {
	Time   string `json:"time"`
	Method string `json:"method"`
	Path   string `json:"path"`
	Status int    `json:"status"`
	Client string `json:"client"`
}

type RotatingLog struct {
	mu   sync.Mutex
	path string
	file *os.File
	size int64
}

func newRotatingLog(path string) (*RotatingLog, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}

	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}

	return &RotatingLog{path: path, file: f, size: info.Size()}, nil
}

func (r *RotatingLog) writeLine(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	data := []byte(line + "\n")

	if r.size+int64(len(data)) > logMaxBytes {
		r.rotate()
	}

	n, err := r.file.Write(data)
	if err == nil {
		r.size += int64(n)
	}
}

func (r *RotatingLog) rotate() {
	r.file.Close()

	for i := logBackupCount - 1; i >= 1; i-- {
		src := fmt.Sprintf("%s.%d", r.path, i)
		dst := fmt.Sprintf("%s.%d", r.path, i+1)
		os.Rename(src, dst)
	}
	os.Rename(r.path, r.path+".1")

	f, err := os.OpenFile(r.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		r.file = f
		r.size = 0
	}
}

var activityLog *RotatingLog

func logActivity(method, path string, status int, client string) {
	if activityLog == nil {
		return
	}

	if client == "" {
		client = "-"
	}

	entry := ActivityEntry{
		Time:   time.Now().UTC().Format(time.RFC3339),
		Method: method,
		Path:   path,
		Status: status,
		Client: client,
	}

	line, err := json.Marshal(entry)
	if err != nil {
		return
	}

	activityLog.writeLine(string(line))
}

// readActivity returns up to limit most-recent entries, newest first —
// only the active (non-rotated) log file is read, matching the Python
// agent's behavior.
func readActivity(logPath string, limit int) []ActivityEntry {
	f, err := os.Open(logPath)
	if err != nil {
		return []ActivityEntry{}
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}

	entries := make([]ActivityEntry, 0, len(lines))
	for i := len(lines) - 1; i >= 0; i-- {
		var e ActivityEntry
		if err := json.Unmarshal([]byte(lines[i]), &e); err == nil {
			entries = append(entries, e)
		}
	}

	return entries
}
