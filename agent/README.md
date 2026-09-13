# LANHub Agent

Runs on every machine you want to browse from the LANHub web app. Same code
on Windows and Linux — see `../docs/AGENT_API.md` for the API it exposes.

## Setup

1. Install Python 3.11+.
2. Copy `config.example.json` to `config.json` and set a long random `token`
   (this must match the token you enter for this machine in the LANHub hub).
3. Install as a background service:
   - **Linux:** `sudo install/install-linux.sh` (add `--tls` to also
     generate a self-signed cert and enable HTTPS).
   - **Windows (as Administrator):** `install/install-windows.ps1` —
     installs as a real Windows Service via NSSM (auto-starts at boot,
     restarts itself on any exit). Add `-Tls` to also generate a
     self-signed cert (requires `openssl` on `PATH`, e.g. via
     `choco install openssl`) and enable HTTPS.
4. Start the service, then register the machine in the hub with this
   machine's LAN IP and port `8765` (check "Use HTTPS" too if you passed
   `--tls`/`-Tls`).

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

## Windows troubleshooting

Two real issues came up installing this on actual Windows machines — both
worth knowing before you assume something's broken:

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
  purely via remote PowerShell when Tamper Protection is active. If an
  agent hangs like this, don't spend long debugging the Python code —
  check `Get-WinEvent -LogName "Microsoft-Windows-Windows Defender/Operational"`
  for filter driver errors first, and try a reboot.
- **A Scheduled Task with an "At log on" trigger silently fails
  (`Last Result: 1`) if nobody is actually logged into an interactive
  session** (checkable with `query user` — "No User exists for *" means
  nobody's logged in). This is normal for a machine that reboots and sits
  at a lock screen. **Use an "At startup" trigger running as `SYSTEM`
  instead** — that doesn't need any interactive session and is what
  `install-windows.ps1` does. A `.bat` wrapper redirecting
  `python.exe agent.py >> task.log 2>&1` (rather than pointing the task
  straight at `pythonw.exe`) makes failures visible instead of silent.
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
  session (Scheduled Tasks, a real reboot) — don't trust a `schtasks /run`
  or similar fired *through* an SSH one-liner as proof it will behave the
  same way at a real boot/logon.

## Running manually (for testing)

```
python -m venv .venv
.venv/bin/pip install -r requirements.txt   # .venv\Scripts\pip.exe on Windows
.venv/bin/python agent.py
```

## Security notes

- The agent has no auth beyond the bearer token — only run it on a trusted
  LAN, never expose port 8765 to the internet.
- `allowed_roots` in `config.json` can restrict the agent to specific
  directories. Leave it empty (default) for full-drive access, matching a
  native file explorer.
