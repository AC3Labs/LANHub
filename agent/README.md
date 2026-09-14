# LANHub Agent

Runs on every machine you want to browse from the LANHub web app. A single
static binary — no runtime, no dependencies to install — same code on
Windows and Linux. See `../docs/AGENT_API.md` for the API it exposes.

## Setup

1. Copy `config.example.json` to `config.json` and set a long random `token`
   (this must match the token you enter for this machine in the LANHub hub).
2. Install as a background service:
   - **Linux:** `sudo install/install-linux.sh` (add `--tls` to also
     generate a self-signed cert and enable HTTPS).
   - **Windows (as Administrator):** `install/install-windows.ps1` —
     the binary registers itself as a native Windows Service (no NSSM or
     any other wrapper — auto-starts at boot, restarts itself on any
     exit). Add `-Tls` to also generate a self-signed cert (requires
     `openssl` on `PATH`, e.g. via `choco install openssl`) and enable
     HTTPS.
3. Start the service, then register the machine in the hub with this
   machine's LAN IP and port `8765` (check "Use HTTPS" too if you passed
   `--tls`/`-Tls`).

Prebuilt binaries for both platforms live in `dist/` — the install scripts
copy the right one automatically. Nothing else needs to be installed on
the target machine.

## Config reference

All fields live in `config.json` (see `config.example.json`):

| Field | Default | Purpose |
|---|---|---|
| `token` | — | Shared bearer secret; must match the hub's Machine record. |
| `host` / `port` | `0.0.0.0` / `8765` | Bind address. |
| `allowed_roots` | `[]` (unrestricted) | Restrict all operations to these paths (and descendants). |
| `log_file` | `agent.log` next to `config.json` | Rotating request log, also read back by `GET /api/activity`. |
| `throttle_kbps` | `0` (unlimited) | Soft bandwidth cap for downloads/uploads/transfer-queue jobs. |
| `cert_file` / `key_file` | unset | PEM cert/key pair to serve HTTPS instead of plain HTTP. |

## Building from source

Requires Go 1.22+. From this directory:

```
go build -trimpath -o dist/lanhub-agent-linux-amd64 .
GOOS=windows GOARCH=amd64 go build -trimpath -o dist/lanhub-agent-windows-amd64.exe .
```

`-trimpath` strips the local build machine's file paths out of the
compiled binary (Go otherwise embeds them for stack traces) — keep it
on any rebuild you intend to distribute.

The only external package is `golang.org/x/sys`, used for native Windows
Service support (`service_windows.go`) — everything else is standard
library.

## Windows service management

On Windows the binary manages its own service registration — no NSSM or
any other wrapper:

```
lanhub-agent.exe install     # registers + starts the service (auto-restart on any failure)
lanhub-agent.exe uninstall   # stops + removes it
lanhub-agent.exe start
lanhub-agent.exe stop
```

`install-windows.ps1` calls `install` for you and migrates a previous
NSSM-based install automatically if one exists.

## Windows troubleshooting

Two real issues came up installing this on actual Windows machines, worth
knowing before you assume something's broken:

- **`nssm.exe` (or any third-party service wrapper) gets blocked outright
  by an Application Control policy** (`Program 'nssm.exe' failed to run:
  An Application Control policy has blocked this file`), even run
  directly with no arguments. Hit on real hardware — the policy targeted
  `nssm.exe` specifically, not arbitrary unsigned executables (the agent
  binary itself ran fine). This is exactly why the agent now manages its
  own Windows service registration instead of depending on NSSM — if this
  still comes up, it means something is now blocking the agent binary
  itself, not a wrapper tool, and needs an actual Application Control /
  WDAC / Smart App Control exception on that machine.
- **The agent process starts but never binds its port, with zero error
  output, seemingly forever.** This was traced (on real hardware) to
  Windows Defender's real-time protection getting into a bad state — its
  own event log showed "Real-Time Protection feature has encountered an
  error and failed... filter driver was unloaded unexpectedly" for Network
  Inspection/Behavior Monitoring/On Access. Antivirus **exclusions added
  via `Add-MpPreference` did not reliably stick** when Tamper Protection is
  enabled (`Get-MpComputerStatus` → `IsTamperProtected`), which is common
  on managed/"Endpoint Protection"-branded Defender setups — the exclusion
  list can silently revert. **A full reboot of the affected machine
  resolved it** every time this came up; there was no clean way to fix it
  purely via remote PowerShell when Tamper Protection is active. If the
  agent hangs like this, check
  `Get-WinEvent -LogName "Microsoft-Windows-Windows Defender/Operational"`
  for filter driver errors first, and try a reboot.
- If you're setting this up **by SSHing in from another machine** (rather
  than at the physical keyboard): Windows' native OpenSSH server ties every
  child process it spawns to that SSH session's Job Object, and kills the
  whole job when the SSH command returns — even processes started via
  `Start-Process` or `WScript.Shell.Run` that look "detached." This makes
  manually testing a background launch over SSH unreliable/misleading
  (looks like it crashed immediately when it didn't). To actually test
  whether something works, either keep the SSH command running in the
  foreground while you check it from a second connection, or trigger it
  through a mechanism Windows itself manages independently of your SSH
  session (the Windows service, a real reboot) — don't trust a one-off
  process launched *through* an SSH one-liner as proof it will behave the
  same way at a real boot/logon.

## Running manually (for testing)

```
LANHUB_AGENT_CONFIG=./config.json ./dist/lanhub-agent-linux-amd64
```

On Windows: `dist\lanhub-agent-windows-amd64.exe` (reads `config.json`
alongside the binary by default, or set `LANHUB_AGENT_CONFIG`).

## Security notes

- The agent has no auth beyond the bearer token — only run it on a trusted
  LAN, never expose port 8765 to the internet.
- `allowed_roots` in `config.json` can restrict the agent to specific
  directories. Leave it empty (default) for full-drive access, matching a
  native file explorer.
