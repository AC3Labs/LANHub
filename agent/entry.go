package main

import (
	"os"
	"path/filepath"
	"time"
)

type Entry struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Type     string `json:"type"`
	Size     *int64 `json:"size"`
	Modified string `json:"modified,omitempty"`
	Hidden   bool   `json:"hidden"`
}

func toEntry(path string) Entry {
	info, err := os.Stat(path)

	e := Entry{
		Name:   filepath.Base(path),
		Path:   path,
		Type:   "file",
		Hidden: isHidden(path),
	}

	if err != nil {
		e.Modified = ""

		return e
	}

	if info.IsDir() {
		e.Type = "dir"
	} else {
		size := info.Size()
		e.Size = &size
	}

	mod := info.ModTime().UTC().Format(time.RFC3339)
	e.Modified = mod

	return e
}
