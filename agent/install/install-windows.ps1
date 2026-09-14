# Installs the LANHub agent as a native Windows Service — no NSSM or any
# other third-party service wrapper. The agent registers and manages
# itself (`lanhub-agent.exe install`), which is more robust than
# depending on an extra tool: NSSM itself got blocked outright by an
# Application Control policy on real hardware during rollout, and a
# wrapper program is one more thing that can be blocked or go missing.
# Run as Administrator. Pass -Tls to also generate a self-signed cert
# and enable HTTPS.

param(
    [switch]$Tls
)

$ErrorActionPreference = "Stop"

$InstallDir = "C:\LANHub-Agent"
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path

New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null

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
    Add-MpPreference -ExclusionProcess "lanhub-agent.exe" -ErrorAction SilentlyContinue
} catch {
    Write-Warning "Could not check/set Windows Defender exclusions (Get-MpComputerStatus/Add-MpPreference failed) — this is fine if Defender isn't the active AV on this machine, otherwise add an exclusion for $InstallDir manually if the agent hangs at startup."
}

New-NetFirewallRule -DisplayName "LANHub Agent" -Direction Inbound -Protocol TCP -LocalPort 8765 -Action Allow -Profile Any -ErrorAction SilentlyContinue | Out-Null

# --- Migrate a previous NSSM-based install, if one exists ----------------
$existing = Get-Service LANHubAgent -ErrorAction SilentlyContinue
if ($existing) {
    Write-Host "Removing previous LANHubAgent service registration..."
    Stop-Service LANHubAgent -ErrorAction SilentlyContinue
    Get-Process lanhub-agent -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
    Get-Process python -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
    Start-Sleep -Seconds 1
    sc.exe delete LANHubAgent | Out-Null
    Start-Sleep -Seconds 1
}

Copy-Item "$ScriptDir\..\dist\lanhub-agent-windows-amd64.exe" "$InstallDir\lanhub-agent.exe" -Force

Push-Location $InstallDir
& .\lanhub-agent.exe install
Pop-Location

Write-Host ""
Write-Host "Installed as a native Windows Service (LANHubAgent) — auto-starts at boot, restarts itself on any exit."
Write-Host "Edit $InstallDir\config.json with a real token, then: Restart-Service LANHubAgent"
Write-Host "Check status any time with: Get-Service LANHubAgent"
Write-Host "Uninstall with: $InstallDir\lanhub-agent.exe uninstall (after Stop-Service LANHubAgent)"
