//go:build !windows

package main

import (
	"os"
	"syscall"
)

type Drive struct {
	Path  string `json:"path"`
	Label string `json:"label"`
	Free  uint64 `json:"free"`
	Total uint64 `json:"total"`
}

func diskUsage(path string) (free, total uint64, err error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, 0, err
	}

	return stat.Bavail * uint64(stat.Bsize), stat.Blocks * uint64(stat.Bsize), nil
}

func listDrives() []Drive {
	var results []Drive

	if free, total, err := diskUsage("/"); err == nil {
		results = append(results, Drive{Path: "/", Label: "/", Free: free, Total: total})
	}

	if home, err := os.UserHomeDir(); err == nil {
		if _, statErr := os.Stat(home); statErr == nil {
			if free, total, err := diskUsage(home); err == nil {
				results = append(results, Drive{Path: home, Label: home, Free: free, Total: total})
			}
		}
	}

	return results
}
