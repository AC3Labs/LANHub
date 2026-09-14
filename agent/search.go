package main

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// walkSearch mirrors the Python agent's bounded os.walk search: a
// directory's own depth decides whether its children get recursed into,
// but that directory's immediate children are always checked for a match
// in the same pass — matching max_depth=N means entries up to depth N+1
// can still be reported, only recursion past that is cut off.
func walkSearch(start, query string, maxDepth, maxResults int, timeoutSec float64) ([]Entry, bool) {
	queryLower := strings.ToLower(query)
	startDepth := pathDepth(start)
	deadline := time.Now().Add(time.Duration(timeoutSec * float64(time.Second)))

	results := make([]Entry, 0)
	truncated := false

	var walk func(dir string) (stop bool)
	walk = func(dir string) bool {
		if time.Now().After(deadline) {
			truncated = true
			return true
		}

		children, err := os.ReadDir(dir)
		if err != nil {
			return false
		}

		depth := pathDepth(dir) - startDepth

		var subdirs []string
		for _, c := range children {
			name := c.Name()
			full := filepath.Join(dir, name)

			if strings.Contains(strings.ToLower(name), queryLower) {
				results = append(results, toEntry(full))
				if len(results) >= maxResults {
					truncated = true
					return true
				}
			}

			if c.IsDir() {
				subdirs = append(subdirs, full)
			}
		}

		if depth >= maxDepth {
			return false
		}

		for _, sd := range subdirs {
			if walk(sd) {
				return true
			}
		}

		return false
	}

	walk(start)

	return results, truncated
}

func pathDepth(path string) int {
	clean := filepath.Clean(path)
	return len(strings.Split(clean, string(filepath.Separator)))
}
