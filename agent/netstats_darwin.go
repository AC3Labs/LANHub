//go:build darwin

package main

import (
	"os/exec"
	"strconv"
	"strings"
)

// netStats shells out to the built-in `netstat -ib`, which lists one row
// per (interface, address family) — so en0 typically appears two or
// three times — and sums Ibytes/Obytes once per interface name (first
// occurrence only) across everything but the loopback interface(s).
// macOS has no equivalent of Linux's /proc/net/dev to read directly, and
// pulling this from the SystemConfiguration/IOKit frameworks needs cgo,
// which the rest of this agent deliberately avoids.
func netStats() (sent, recv uint64) {
	out, err := exec.Command("netstat", "-ib").Output()
	if err != nil {
		return 0, 0
	}

	lines := strings.Split(string(out), "\n")
	if len(lines) == 0 {
		return 0, 0
	}

	header := strings.Fields(lines[0])
	ibytesIdx, obytesIdx := -1, -1
	for i, col := range header {
		switch col {
		case "Ibytes":
			ibytesIdx = i
		case "Obytes":
			obytesIdx = i
		}
	}
	if ibytesIdx < 0 || obytesIdx < 0 {
		return 0, 0
	}

	seen := map[string]bool{}
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) <= ibytesIdx || len(fields) <= obytesIdx {
			continue
		}

		name := fields[0]
		if name == "lo0" || strings.HasPrefix(name, "lo") || seen[name] {
			continue
		}
		seen[name] = true

		if rx, err := strconv.ParseUint(fields[ibytesIdx], 10, 64); err == nil {
			recv += rx
		}
		if tx, err := strconv.ParseUint(fields[obytesIdx], 10, 64); err == nil {
			sent += tx
		}
	}

	return sent, recv
}
