//go:build linux

package main

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// netStats reads /proc/net/dev directly — no external process, no extra
// dependency — and sums received/transmitted bytes across every real
// interface (everything but loopback). These are cumulative counters
// since the interface came up; the hub is the one that turns two samples
// into a rate (see NetStats in AgentClient).
func netStats() (sent, recv uint64) {
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return 0, 0
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		colon := strings.Index(line, ":")
		if colon < 0 {
			continue // header lines have no ':'
		}

		name := strings.TrimSpace(line[:colon])
		if name == "lo" {
			continue
		}

		fields := strings.Fields(line[colon+1:])
		// Layout: rx_bytes rx_packets ... (8 fields) tx_bytes tx_packets ...
		if len(fields) < 9 {
			continue
		}

		if rx, err := strconv.ParseUint(fields[0], 10, 64); err == nil {
			recv += rx
		}
		if tx, err := strconv.ParseUint(fields[8], 10, 64); err == nil {
			sent += tx
		}
	}

	return sent, recv
}
