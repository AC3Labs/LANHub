# Installs the LANHub agent as a proper Windows Service (via NSSM) —
# auto-starts at boot, restarts itself if the process ever exits for any
# reason (crash, kill, whatever), and rotates its own log file. Run as
# Administrator. Requires Python 3.11+ on PATH.
#
# Replaces the earlier Scheduled Task approach (kept working, but had no
# restart policy — if the agent process died after boot for any reason,
# it stayed dead until the next full reboot). A real Windows Service's
# Recovery/AppExit behavior is the correct fix, not a bigger hack on top
# of Scheduled Tasks.
#
# Pass -Tls to also generate a self-signed cert and enable HTTPS.

param(
    [switch]$Tls
)

$ErrorActionPreference = "Stop"

$InstallDir = "C:\LANHub-Agent"
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path

New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
Copy-Item "$ScriptDir\..\agent.py" "$InstallDir\agent.py" -Force
Copy-Item "$ScriptDir\..\requirements.txt" "$InstallDir\requirements.txt" -Force

$ConfigWasFresh = $false
if (-not (Test-Path "$InstallDir\config.json")) {
    Copy-Item "$ScriptDir\..\config.example.json" "$InstallDir\config.json"
    Write-Host "Wrote default config to $InstallDir\config.json — edit the token before starting the service."
    $ConfigWasFresh = $true
}

if ($Tls) {
    $CertsDir = "$InstallDir\certs"
    New-Item -ItemType Directory -Force -Path $CertsDir | Out-Null
    $CertFile = "$CertsDir\agent.crt"
    $KeyFile = "$CertsDir\agent.key"

    if (-not (Test-Path $CertFile)) {
        if (Get-Command openssl -ErrorAction SilentlyContinue) {
            & openssl req -x509 -newkey rsa:2048 -nodes -keyout $KeyFile -out $CertFile -days 3650 -subj "/CN=$env:COMPUTERNAME"
            Write-Host "Generated a self-signed TLS cert at $CertFile (valid 10 years). This is for a trusted LAN — it will not validate against a public CA."
        } else {
            Write-Warning "openssl.exe not found (e.g. 'choco install openssl') — install it and re-run with -Tls, or generate cert_file/key_file yourself and set them in config.json."
        }
    }

    if ((Test-Path $CertFile) -and $ConfigWasFresh) {
        $configJson = Get-Content "$InstallDir\config.json" -Raw | ConvertFrom-Json
        $configJson | Add-Member -NotePropertyName cert_file -NotePropertyValue $CertFile -Force
        $configJson | Add-Member -NotePropertyName key_file -NotePropertyValue $KeyFile -Force
        $configJson | ConvertTo-Json -Depth 10 | Set-Content "$InstallDir\config.json"
    } elseif (Test-Path $CertFile) {
        Write-Host "config.json already existed — set `"cert_file`": `"$CertFile`" and `"key_file`": `"$KeyFile`" in it yourself to enable HTTPS."
    }
}

# --- Windows Defender hardening -----------------------------------------
# A real, repeated failure mode on actual hardware: Defender's real-time
# protection getting into a bad state around this process, with zero error
# output — see README's "Windows troubleshooting" section. An exclusion
# up front avoids ever hitting it in the first place, which is much
# cheaper than debugging a silently-hanging agent later.
try {
    $tamperStatus = Get-MpComputerStatus -ErrorAction Stop
    if ($tamperStatus.IsTamperProtected) {
        Write-Warning "Windows Defender Tamper Protection is ON. The exclusion this script just added may not stick (silently reverts under Tamper Protection). If the agent later hangs at startup with zero output, this is almost certainly why — see README's Windows troubleshooting section. Add the exclusion manually via Windows Security > Virus & threat protection settings instead."
    }
    Add-MpPreference -ExclusionPath $InstallDir -ErrorAction SilentlyContinue
    Add-MpPreference -ExclusionProcess "python.exe" -ErrorAction SilentlyContinue
} catch {
    Write-Warning "Could not check/set Windows Defender exclusions (Get-MpComputerStatus/Add-MpPreference failed) — this is fine if Defender isn't the active AV on this machine, otherwise add an exclusion for $InstallDir manually if the agent hangs at startup."
}

python -m venv "$InstallDir\.venv"
& "$InstallDir\.venv\Scripts\pip.exe" install --quiet -r "$InstallDir\requirements.txt"

New-NetFirewallRule -DisplayName "LANHub Agent" -Direction Inbound -Protocol TCP -LocalPort 8765 -Action Allow -Profile Any -ErrorAction SilentlyContinue | Out-Null

# --- NSSM service install ------------------------------------------------
if (-not (Get-Command nssm -ErrorAction SilentlyContinue)) {
    if (Get-Command choco -ErrorAction SilentlyContinue) {
        Write-Host "Installing NSSM via Chocolatey..."
        choco install nssm -y | Out-Null
    } else {
        throw "nssm.exe not found and Chocolatey isn't installed. Install NSSM (https://nssm.cc/) or Chocolatey first, then re-run this script."
    }
}

# Clean up a previous Scheduled-Task-based install if one exists, so a
# re-run of this script on an older install migrates cleanly instead of
# running two copies of the agent at once.
schtasks /query /tn "LANHubAgent" 2>$null | Out-Null
if ($LASTEXITCODE -eq 0) {
    Write-Host "Removing old Scheduled Task install..."
    schtasks /end /tn "LANHubAgent" 2>$null | Out-Null
    schtasks /delete /tn "LANHubAgent" /f 2>$null | Out-Null
}
Get-Process python -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
Start-Sleep -Seconds 1

nssm remove LANHubAgent confirm 2>$null | Out-Null
nssm install LANHubAgent "$InstallDir\.venv\Scripts\python.exe" "agent.py"
nssm set LANHubAgent AppDirectory $InstallDir
nssm set LANHubAgent AppStdout "$InstallDir\task.log"
nssm set LANHubAgent AppStderr "$InstallDir\task.log"
nssm set LANHubAgent AppRotateFiles 1
nssm set LANHubAgent AppRotateBytes 10485760
nssm set LANHubAgent Start SERVICE_AUTO_START
# AppExit Default Restart = always restart the process, whatever its exit
# code — a clean exit shouldn't leave the agent down any more than a crash
# should. AppRestartDelay/AppThrottle keep a crash-loop from spinning hot.
nssm set LANHubAgent AppExit Default Restart
nssm set LANHubAgent AppRestartDelay 3000
nssm set LANHubAgent AppThrottle 3000

Start-Service LANHubAgent

Write-Host ""
Write-Host "Installed as a Windows Service (LANHubAgent) — auto-starts at boot, restarts itself on any exit."
Write-Host "Edit $InstallDir\config.json with a real token, then: Restart-Service LANHubAgent"
Write-Host "Check status any time with: Get-Service LANHubAgent"
