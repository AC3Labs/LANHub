//go:build !windows

// Linux has no equivalent concept here — it runs as a plain foreground
// process managed by systemd (see install/lanhub-agent.service).

package main

func isWindowsService() bool {
	return false
}

func handleServiceCommand(cmd string) bool {
	return false
}

func runWindowsService() {
	// Never called: isWindowsService() always returns false here.
}
