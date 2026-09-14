//go:build windows

// Native Windows Service support — no NSSM or any other third-party
// service wrapper needed. `lanhub-agent.exe install` registers the
// service (auto-start, restarts itself on any failure); `start`/`stop`/
// `uninstall` manage it from there. This exists because NSSM itself got
// blocked by an Application Control policy on real hardware during
// rollout — a wrapper program is one more thing that can be blocked or
// go missing; the binary managing itself has nothing else to depend on.
package main

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const serviceName = "LANHubAgent"

func isWindowsService() bool {
	is, err := svc.IsWindowsService()
	return err == nil && is
}

func handleServiceCommand(cmd string) bool {
	switch cmd {
	case "install":
		if err := installService(); err != nil {
			fmt.Fprintln(os.Stderr, "install failed:", err)
			os.Exit(1)
		}
		fmt.Println("LANHubAgent service installed and started.")
	case "uninstall", "remove":
		if err := uninstallService(); err != nil {
			fmt.Fprintln(os.Stderr, "uninstall failed:", err)
			os.Exit(1)
		}
		fmt.Println("LANHubAgent service removed.")
	case "start":
		if err := controlService("start"); err != nil {
			fmt.Fprintln(os.Stderr, "start failed:", err)
			os.Exit(1)
		}
		fmt.Println("LANHubAgent service started.")
	case "stop":
		if err := controlService("stop"); err != nil {
			fmt.Fprintln(os.Stderr, "stop failed:", err)
			os.Exit(1)
		}
		fmt.Println("LANHubAgent service stopped.")
	default:
		return false
	}

	return true
}

func installService() error {
	exePath, err := os.Executable()
	if err != nil {
		return err
	}

	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()

	if existing, err := m.OpenService(serviceName); err == nil {
		existing.Close()
		return fmt.Errorf("service %s already exists — run 'uninstall' first to reinstall", serviceName)
	}

	s, err := m.CreateService(serviceName, exePath, mgr.Config{
		DisplayName:  "LANHub Agent",
		Description:  "LANHub file-access agent — see docs/AGENT_API.md.",
		StartType:    mgr.StartAutomatic,
		ErrorControl: mgr.ErrorNormal,
	})
	if err != nil {
		return err
	}
	defer s.Close()

	// Restart on any failure, forever — a clean exit shouldn't leave the
	// agent down any more than a crash should, matching the old NSSM
	// AppExit "Default Restart" behavior.
	err = s.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 3 * time.Second},
	}, uint32((24 * time.Hour).Seconds()))
	if err != nil {
		return err
	}
	if err := s.SetRecoveryActionsOnNonCrashFailures(true); err != nil {
		return err
	}

	return s.Start()
}

func uninstallService() error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("service %s is not installed", serviceName)
	}
	defer s.Close()

	s.Control(svc.Stop)

	return s.Delete()
}

func controlService(action string) error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("service %s is not installed", serviceName)
	}
	defer s.Close()

	if action == "start" {
		return s.Start()
	}

	_, err = s.Control(svc.Stop)
	return err
}

type agentService struct{}

func (a *agentService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	changes <- svc.Status{State: svc.StartPending}

	cfg, srv, err := setup()
	if err != nil {
		return true, 1
	}

	go serve(cfg, srv)

	changes <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}

	for c := range r {
		switch c.Cmd {
		case svc.Interrogate:
			changes <- c.CurrentStatus
		case svc.Stop, svc.Shutdown:
			changes <- svc.Status{State: svc.StopPending}
			shutdown(srv)
			changes <- svc.Status{State: svc.Stopped}
			return false, 0
		}
	}

	return false, 0
}

func runWindowsService() {
	svc.Run(serviceName, &agentService{})
}
