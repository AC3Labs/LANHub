//go:build !windows

package main

import (
	"path/filepath"
	"strings"
)

func isHidden(path string) bool {
	return strings.HasPrefix(filepath.Base(path), ".")
}
