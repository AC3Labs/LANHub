# LANHub agent installer for Windows, served by your LANHub hub. Right-click
# Start, choose Terminal (Admin), and run:
#
#   & ([scriptblock]::Create((irm '{{%.Hub%}}/agent/install.ps1'))) -Token 'TOKEN'
#
# Options: -Token (the machine's token from the hub), -Port (default 8765),
# -Tls (a self-signed certificate, for HTTPS; needs openssl.exe).
# Running it again updates the agent and keeps its settings and token.
param([string]$Token = "", [int]$Port = 0, [switch]$Tls)
$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"

$Hub = "{{%.Hub%}}"
$Version = "{{%.Version%}}"
$Sum = "{{%index .Sums "windows-amd64"%}}"
$Dir = "C:\LANHub-Agent"
$Exe = Join-Path $Dir "lanhub-agent.exe"
$New = Join-Path $Dir "lanhub-agent.new.exe"

$principal = [Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw "Run this in Terminal (Admin): right-click Start, choose Terminal (Admin), and run it again there."
}
if (-not $Sum) { throw "This hub has no agent build for Windows." }
if ($env:PROCESSOR_ARCHITECTURE -ne "AMD64") { throw "This hub only has a 64-bit Intel/AMD agent build for Windows." }

New-Item -ItemType Directory -Force -Path $Dir | Out-Null
Write-Host "Downloading the LANHub agent $Version from $Hub"
Invoke-WebRequest -UseBasicParsing "$Hub/agent/download/lanhub-agent-windows-amd64.exe" -OutFile $New
if ((Get-FileHash $New -Algorithm SHA256).Hash.ToLower() -ne $Sum) {
    Remove-Item $New -Force
    throw "The download doesn't match its checksum, so nothing was changed."
}

# Run it once first: Windows Smart App Control blocks unsigned programs it
# hasn't seen, and then the agent that's here now should stay.
try {
    $got = (& $New version | Out-String).Trim()
} catch {
    Remove-Item $New -Force -ErrorAction SilentlyContinue
    if ("$_" -match "Application Control") {
        throw "Windows blocked the new agent (Smart App Control or another Application Control policy). The agent already installed here, if any, was left as it is."
    }
    throw
}
if ($got -ne $Version) {
    Remove-Item $New -Force
    throw "The downloaded agent won't run on this machine, so nothing was changed."
}

# Windows Defender's real-time scanning has hung the agent at startup on
# real machines; an exclusion avoids it. Not fatal if Defender isn't used.
try {
    Add-MpPreference -ExclusionPath $Dir -ErrorAction Stop
    Add-MpPreference -ExclusionProcess "lanhub-agent.exe" -ErrorAction Stop
} catch {
    Write-Warning "Couldn't add a Windows Defender exclusion for $Dir. Add one yourself if the agent hangs at startup."
}

$svc = Get-Service LANHubAgent -ErrorAction SilentlyContinue
if ($svc) {
    Stop-Service LANHubAgent -Force -ErrorAction SilentlyContinue
    Start-Sleep -Seconds 2
    Get-Process lanhub-agent -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
}
Move-Item -Force $New $Exe

$conf = @()
if ($Token) { $conf += @("--token", $Token) }
if ($Port) { $conf += @("--port", "$Port") }
if ($Tls) {
    $certs = Join-Path $Dir "certs"
    New-Item -ItemType Directory -Force -Path $certs | Out-Null
    if (-not (Test-Path "$certs\agent.crt")) {
        if (-not (Get-Command openssl -ErrorAction SilentlyContinue)) { throw "-Tls needs openssl.exe (for example: winget install ShiningLight.OpenSSL.Light)." }
        & openssl req -x509 -newkey rsa:2048 -nodes -keyout "$certs\agent.key" -out "$certs\agent.crt" -days 3650 -subj "/CN=$env:COMPUTERNAME" 2>$null
        Write-Host "Made a self-signed certificate (10 years). Tick `"Use HTTPS`" for this machine in the hub."
    }
    $conf += @("--cert", "$certs\agent.crt", "--key", "$certs\agent.key")
}
& $Exe configure @conf
if ($LASTEXITCODE -ne 0) { throw "Couldn't save the agent's settings." }

$cfg = Get-Content (Join-Path $Dir "config.json") -Raw | ConvertFrom-Json
$listen = if ($cfg.port) { [int]$cfg.port } else { 8765 }
if (-not (Get-NetFirewallRule -DisplayName "LANHub Agent" -ErrorAction SilentlyContinue)) {
    New-NetFirewallRule -DisplayName "LANHub Agent" -Direction Inbound -Protocol TCP -LocalPort $listen -Action Allow -Profile Any -RemoteAddress LocalSubnet | Out-Null
}

& $Exe has-token
if ($LASTEXITCODE -ne 0) {
    if (-not $svc) { & $Exe install | Out-Null; Stop-Service LANHubAgent -Force -ErrorAction SilentlyContinue }
    Write-Host "Installed, but it has no token yet. Run this again with -Token 'TOKEN' (from the hub's Add Machine)."
    return
}
if ($svc) { Start-Service LANHubAgent } else { & $Exe install | Out-Null }
Start-Sleep -Seconds 2
if ((Get-Service LANHubAgent).Status -ne "Running") {
    throw "The agent service didn't start. Check Event Viewer > Windows Logs > Application."
}
Write-Host "The LANHub agent $Version is running on port $listen."
