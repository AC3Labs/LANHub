package main

import (
	"io"
	"os"
	"path/filepath"
)

// copyTree recursively copies a directory (like Python's shutil.copytree),
// preserving each file's modification time.
func copyTree(source, destination string) error {
	return filepath.Walk(source, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(source, p)
		if err != nil {
			return err
		}

		target := filepath.Join(destination, rel)

		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}

		return copyFile(p, target, info)
	})
}

func copyFile(source, destination string, info os.FileInfo) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return err
	}

	src, err := os.Open(source)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return err
	}

	return os.Chtimes(destination, info.ModTime(), info.ModTime())
}
