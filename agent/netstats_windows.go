//go:build windows

package main

import (
	"bufio"
	"os/exec"
	"strconv"
	"strings"
)

// netStats shells out to the built-in `netstat -e` (present on every
// Windows install, no admin rights needed) rather than binding the IP
// Helper API's interface-table structs — those are large, version-prone
// win32 structs to marshal correctly for one summary number. `netstat -e`
// already gives a single pre-summed total across every adapter in a
// fixed, well-known format:
//
//	Interface Statistics
//
//	                           Received            Sent
//
//	Bytes                      123456789           987654321
func netStats() (sent, recv uint64) {
	out, err := exec.Command("netstat", "-e").Output()
	if err != nil {
		return 0, 0
	}

	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 3 || fields[0] != "Bytes" {
			continue
		}

		if rx, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
			recv = rx
		}
		if tx, err := strconv.ParseUint(fields[2], 10, 64); err == nil {
			sent = tx
		}

		break
	}

	return sent, recv
}
