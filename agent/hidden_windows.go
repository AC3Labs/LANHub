//go:build windows

package main

import (
	"path/filepath"
	"strings"
	"syscall"
)

const fileAttributeHidden = 0x2

func isHidden(path string) bool {
	if strings.HasPrefix(filepath.Base(path), ".") {
		return true
	}

	ptr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return false
	}

	attrs, err := syscall.GetFileAttributes(ptr)
	if err != nil || attrs == syscall.INVALID_FILE_ATTRIBUTES {
		return false
	}

	return attrs&fileAttributeHidden != 0
}
