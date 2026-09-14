package main

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const chunkSize = 256 * 1024

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"message": message})
}

func handleErr(w http.ResponseWriter, err error) {
	if ae, ok := err.(*AgentError); ok {
		writeError(w, ae.Status, ae.Message)
		return
	}
	writeError(w, 500, err.Error())
}

func queryParam(r *http.Request, name string) string {
	return r.URL.Query().Get(name)
}

// --- /api/health ---

func handleHealth(w http.ResponseWriter, r *http.Request) {
	hostname, _ := os.Hostname()
	writeJSON(w, 200, map[string]string{
		"hostname": hostname,
		"os":       runtime.GOOS,
		"version":  agentVersion,
	})
}

// --- /api/drives ---

func handleDrives(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"drives": listDrives()})
}

// --- /api/list ---

func handleList(w http.ResponseWriter, r *http.Request) {
	raw := queryParam(r, "path")
	if raw == "" {
		writeError(w, 400, "path is required.")
		return
	}

	dir, err := resolvePath(raw)
	if err != nil {
		handleErr(w, err)
		return
	}

	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		writeError(w, 404, dir+" is not a directory.")
		return
	}

	children, err := os.ReadDir(dir)
	if err != nil {
		writeError(w, 403, "Permission denied reading "+dir+".")
		return
	}

	entries := make([]Entry, 0, len(children))
	for _, c := range children {
		entries = append(entries, toEntry(filepath.Join(dir, c.Name())))
	}

	sortEntries(entries)

	writeJSON(w, 200, map[string]any{"path": dir, "entries": entries})
}

func sortEntries(entries []Entry) {
	sort.SliceStable(entries, func(i, j int) bool {
		iDir := entries[i].Type == "dir"
		jDir := entries[j].Type == "dir"
		if iDir != jDir {
			return iDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
}

// --- throttling ---

func throttleDelay(cfg *Config) time.Duration {
	if cfg.ThrottleKbps <= 0 {
		return 0
	}
	seconds := float64(chunkSize) / 1024 / float64(cfg.ThrottleKbps)
	return time.Duration(seconds * float64(time.Second))
}

func streamThrottled(w io.Writer, path string, delay time.Duration) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	buf := make([]byte, chunkSize)
	for {
		n, readErr := f.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				return writeErr
			}
			if delay > 0 {
				time.Sleep(delay)
			}
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

// --- /api/download ---

func handleDownload(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw := queryParam(r, "path")
		filePath, err := resolvePath(raw)
		if err != nil {
			handleErr(w, err)
			return
		}

		info, err := os.Stat(filePath)
		if err != nil || info.IsDir() {
			writeError(w, 404, filePath+" is not a file.")
			return
		}

		w.Header().Set("Content-Disposition", `attachment; filename="`+filepath.Base(filePath)+`"`)
		w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
		w.Header().Set("Content-Type", "application/octet-stream")

		streamThrottled(w, filePath, throttleDelay(cfg))
	}
}

// --- /api/preview ---

var previewImageTypes = map[string]bool{
	"image/jpeg": true, "image/png": true, "image/gif": true,
	"image/webp": true, "image/bmp": true, "image/svg+xml": true,
}

const previewTextMaxBytes = 512 * 1024

func handlePreview(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw := queryParam(r, "path")
		filePath, err := resolvePath(raw)
		if err != nil {
			handleErr(w, err)
			return
		}

		info, err := os.Stat(filePath)
		if err != nil || info.IsDir() {
			writeError(w, 404, filePath+" is not a file.")
			return
		}

		w.Header().Set("X-Content-Type-Options", "nosniff")

		guessed := mime.TypeByExtension(filepath.Ext(filePath))
		if idx := strings.Index(guessed, ";"); idx != -1 {
			guessed = guessed[:idx]
		}

		if previewImageTypes[guessed] {
			w.Header().Set("Content-Disposition", `inline; filename="`+filepath.Base(filePath)+`"`)
			w.Header().Set("Content-Type", guessed)
			streamThrottled(w, filePath, throttleDelay(cfg))
			return
		}

		if guessed == "application/pdf" {
			w.Header().Set("Content-Disposition", `inline; filename="`+filepath.Base(filePath)+`"`)
			w.Header().Set("Content-Type", "application/pdf")
			streamThrottled(w, filePath, throttleDelay(cfg))
			return
		}

		isTextish := guessed == "" || strings.HasPrefix(guessed, "text/") ||
			guessed == "application/json" || guessed == "application/xml" || guessed == "application/x-yaml"

		if isTextish {
			f, err := os.Open(filePath)
			if err != nil {
				writeError(w, 403, "Permission denied reading "+filePath+".")
				return
			}
			defer f.Close()

			buf := make([]byte, previewTextMaxBytes+1)
			n, _ := io.ReadFull(f, buf)
			truncated := n > previewTextMaxBytes
			text := string(buf[:min(n, previewTextMaxBytes)])
			if truncated {
				text += "\n\n[preview truncated]"
			}

			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(200)
			w.Write([]byte(text))
			return
		}

		writeError(w, 415, "No preview available for "+filepath.Base(filePath)+".")
	}
}

// --- /api/upload ---

func handleUpload(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw := r.URL.Query().Get("path")
		if raw == "" {
			raw = r.FormValue("path")
		}

		destDir, err := resolvePath(raw)
		if err != nil {
			handleErr(w, err)
			return
		}

		if err := os.MkdirAll(destDir, 0755); err != nil {
			writeError(w, 500, err.Error())
			return
		}

		file, header, err := r.FormFile("file")
		if err != nil {
			writeError(w, 400, "file is required.")
			return
		}
		defer file.Close()

		destPath := filepath.Join(destDir, header.Filename)
		out, err := os.Create(destPath)
		if err != nil {
			writeError(w, 500, err.Error())
			return
		}
		defer out.Close()

		delay := throttleDelay(cfg)
		buf := make([]byte, chunkSize)
		for {
			n, readErr := file.Read(buf)
			if n > 0 {
				if _, writeErr := out.Write(buf[:n]); writeErr != nil {
					writeError(w, 500, writeErr.Error())
					return
				}
				if delay > 0 {
					time.Sleep(delay)
				}
			}
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				writeError(w, 500, readErr.Error())
				return
			}
		}

		writeJSON(w, 200, toEntry(destPath))
	}
}

// --- mutating body-based endpoints ---

type pathBody struct {
	Path string `json:"path"`
}

type renameBody struct {
	Path    string `json:"path"`
	NewName string `json:"new_name"`
}

type transferBody struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
}

type deleteBody struct {
	Path      string `json:"path"`
	Recursive *bool  `json:"recursive"`
}

func decodeBody(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

func handleMkdir(w http.ResponseWriter, r *http.Request) {
	var body pathBody
	if err := decodeBody(r, &body); err != nil {
		writeError(w, 400, "invalid request body.")
		return
	}

	target, err := resolvePath(body.Path)
	if err != nil {
		handleErr(w, err)
		return
	}

	if err := os.MkdirAll(target, 0755); err != nil {
		writeError(w, 500, err.Error())
		return
	}

	writeJSON(w, 200, toEntry(target))
}

func handleRename(w http.ResponseWriter, r *http.Request) {
	var body renameBody
	if err := decodeBody(r, &body); err != nil {
		writeError(w, 400, "invalid request body.")
		return
	}

	source, err := resolvePath(body.Path)
	if err != nil {
		handleErr(w, err)
		return
	}

	target := filepath.Join(filepath.Dir(source), body.NewName)

	if _, err := os.Stat(target); err == nil {
		writeError(w, 409, target+" already exists.")
		return
	}

	if err := os.Rename(source, target); err != nil {
		writeError(w, 500, err.Error())
		return
	}

	writeJSON(w, 200, toEntry(target))
}

func handleMove(w http.ResponseWriter, r *http.Request) {
	var body transferBody
	if err := decodeBody(r, &body); err != nil {
		writeError(w, 400, "invalid request body.")
		return
	}

	source, err := resolvePath(body.Source)
	if err != nil {
		handleErr(w, err)
		return
	}
	destination, err := resolvePath(body.Destination)
	if err != nil {
		handleErr(w, err)
		return
	}

	if _, err := os.Stat(destination); err == nil {
		writeError(w, 409, destination+" already exists.")
		return
	}

	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		writeError(w, 500, err.Error())
		return
	}

	if err := os.Rename(source, destination); err != nil {
		writeError(w, 500, err.Error())
		return
	}

	writeJSON(w, 200, toEntry(destination))
}

func handleCopy(w http.ResponseWriter, r *http.Request) {
	var body transferBody
	if err := decodeBody(r, &body); err != nil {
		writeError(w, 400, "invalid request body.")
		return
	}

	source, err := resolvePath(body.Source)
	if err != nil {
		handleErr(w, err)
		return
	}
	destination, err := resolvePath(body.Destination)
	if err != nil {
		handleErr(w, err)
		return
	}

	if _, err := os.Stat(destination); err == nil {
		writeError(w, 409, destination+" already exists.")
		return
	}

	info, err := os.Stat(source)
	if err != nil {
		writeError(w, 404, source+" does not exist.")
		return
	}

	if info.IsDir() {
		err = copyTree(source, destination)
	} else {
		err = copyFileWithProgress(source, destination, &TransferJob{mu: &sync.Mutex{}})
	}

	if err != nil {
		writeError(w, 500, err.Error())
		return
	}

	writeJSON(w, 200, toEntry(destination))
}

func handleDelete(w http.ResponseWriter, r *http.Request) {
	var body deleteBody
	if err := decodeBody(r, &body); err != nil {
		writeError(w, 400, "invalid request body.")
		return
	}

	recursive := true
	if body.Recursive != nil {
		recursive = *body.Recursive
	}

	target, err := resolvePath(body.Path)
	if err != nil {
		handleErr(w, err)
		return
	}

	info, err := os.Stat(target)
	if err != nil {
		writeError(w, 404, target+" does not exist.")
		return
	}

	if info.IsDir() {
		if !recursive {
			children, _ := os.ReadDir(target)
			if len(children) > 0 {
				writeError(w, 400, target+" is not empty.")
				return
			}
		}
		if err := os.RemoveAll(target); err != nil {
			writeError(w, 500, err.Error())
			return
		}
	} else if err := os.Remove(target); err != nil {
		writeError(w, 500, err.Error())
		return
	}

	writeJSON(w, 200, map[string]string{"deleted": target})
}

// --- /api/search ---

func handleSearch(w http.ResponseWriter, r *http.Request) {
	raw := queryParam(r, "path")
	query := queryParam(r, "query")
	if query == "" {
		writeError(w, 400, "query is required.")
		return
	}

	maxDepth := intParam(r, "max_depth", 8)
	maxResults := intParam(r, "max_results", 200)
	timeoutSec := floatParam(r, "timeout", 10.0)

	start, err := resolvePath(raw)
	if err != nil {
		handleErr(w, err)
		return
	}

	info, err := os.Stat(start)
	if err != nil || !info.IsDir() {
		writeError(w, 404, start+" is not a directory.")
		return
	}

	entries, truncated := walkSearch(start, query, maxDepth, maxResults, timeoutSec)

	writeJSON(w, 200, map[string]any{
		"path": start, "query": query, "entries": entries, "truncated": truncated,
	})
}

// --- /api/transfers ---

func handleCreateTransfer(queue *TransferQueue) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Source      string `json:"source"`
			Destination string `json:"destination"`
			Kind        string `json:"kind"`
		}
		if err := decodeBody(r, &body); err != nil {
			writeError(w, 400, "invalid request body.")
			return
		}
		if body.Kind == "" {
			body.Kind = "copy"
		}
		if body.Kind != "copy" && body.Kind != "move" {
			writeError(w, 400, "kind must be 'copy' or 'move'.")
			return
		}

		source, err := resolvePath(body.Source)
		if err != nil {
			handleErr(w, err)
			return
		}
		destination, err := resolvePath(body.Destination)
		if err != nil {
			handleErr(w, err)
			return
		}

		if _, err := os.Stat(source); err != nil {
			writeError(w, 404, source+" does not exist.")
			return
		}
		if _, err := os.Stat(destination); err == nil {
			writeError(w, 409, destination+" already exists.")
			return
		}

		job := queue.Create(body.Kind, source, destination)
		writeJSON(w, 200, job.snapshot())
	}
}

func handleGetTransfer(queue *TransferQueue) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		job, ok := queue.Get(id)
		if !ok {
			writeError(w, 404, "Unknown transfer id.")
			return
		}
		writeJSON(w, 200, job.snapshot())
	}
}

func handleListTransfers(queue *TransferQueue) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"transfers": queue.Recent(50)})
	}
}

// --- /api/activity ---

func handleActivity(w http.ResponseWriter, r *http.Request) {
	limit := intParam(r, "limit", 200)
	entries := readActivity(logFilePath, limit)
	writeJSON(w, 200, map[string]any{"entries": entries})
}

// --- helpers ---

func intParam(r *http.Request, name string, def int) int {
	v := queryParam(r, name)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func floatParam(r *http.Request, name string, def float64) float64 {
	v := queryParam(r, name)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}
