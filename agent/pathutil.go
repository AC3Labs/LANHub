package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type AgentError struct {
	Status  int
	Message string
}

func (e *AgentError) Error() string {
	return e.Message
}

func newError(status int, format string, args ...any) *AgentError {
	return &AgentError{Status: status, Message: fmt.Sprintf(format, args...)}
}

var allowedRoots []string

func setAllowedRoots(raw []string) {
	for _, r := range raw {
		if r == "" {
			continue
		}
		abs, err := filepath.Abs(r)
		if err != nil {
			continue
		}
		if resolved, err := filepath.EvalSymlinks(abs); err == nil {
			abs = resolved
		}
		allowedRoots = append(allowedRoots, abs)
	}
}

// resolvePath mirrors the Python agent's Path(raw).resolve(): absolute,
// symlinks resolved where the path already exists, then checked against
// any configured allowed_roots. A path outside every allowed root is
// rejected with 403 rather than silently clamped, matching the original.
func resolvePath(raw string) (string, error) {
	abs, err := filepath.Abs(raw)
	if err != nil {
		return "", newError(400, "invalid path %s", raw)
	}
	abs = filepath.Clean(abs)

	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}

	if len(allowedRoots) == 0 {
		return abs, nil
	}

	for _, root := range allowedRoots {
		if abs == root || strings.HasPrefix(abs, root+string(filepath.Separator)) {
			return abs, nil
		}
	}

	return "", newError(403, "Path %s is outside the allowed roots.", abs)
}

func directorySize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	if !info.IsDir() {
		return info.Size()
	}

	var total int64
	filepath.Walk(path, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi == nil {
			return nil
		}
		if !fi.IsDir() {
			total += fi.Size()
		}
		return nil
	})

	return total
}
