[CmdletBinding()]
param(
    [int]$Port = 8080,
    [switch]$SkipBuild,
    [switch]$KeepRunning
)

# One-shot dev workflow: build -> start -> wait healthy -> run ALL smoke suites
# -> always stop the server (even on failure). Avoids leaving a background
# process (port 8080) behind, which is what previously looked like a "stall".
#
# Requires DB env vars in the current session, e.g.:
#   $env:PGSQL_HOST='...'; $env:PGSQL_PORT='5432'; $env:PGSQL_USERNAME='...';
#   $env:PGSQL_PASSWORD='...'; $env:PGSQL_DATABASE='coc'
#
# Usage:  powershell -ExecutionPolicy Bypass -File tools/dev-smoke-all.ps1
#         powershell -ExecutionPolicy Bypass -File tools/dev-smoke-all.ps1 -SkipBuild
#         powershell -ExecutionPolicy Bypass -File tools/dev-smoke-all.ps1 -KeepRunning

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

$root = Split-Path -Parent $PSScriptRoot
$tmp = Join-Path $env:TEMP 'opencode'
if (-not (Test-Path $tmp)) { New-Item -ItemType Directory -Path $tmp -Force | Out-Null }
$pidFile = Join-Path $tmp 'z-coc.pid'
$outLog = Join-Path $tmp 'z-coc.out.log'
$errLog = Join-Path $tmp 'z-coc.err.log'
$url = "http://127.0.0.1:$Port"

# Loopback calls must bypass any local HTTP proxy.
$env:HTTP_PROXY = $null; $env:HTTPS_PROXY = $null; $env:ALL_PROXY = $null
[System.Net.WebRequest]::DefaultWebProxy = $null

# Reap any instance left from a previous run so the port is free.
if (Test-Path $pidFile) {
    $old = Get-Content $pidFile -ErrorAction SilentlyContinue
    if ($old) { Stop-Process -Id $old -Force -ErrorAction SilentlyContinue }
    Remove-Item $pidFile -Force -ErrorAction SilentlyContinue
}

if (-not $env:PGSQL_HOST) { throw "PGSQL_HOST is not set in this session" }

Set-Location $root

if (-not $SkipBuild) {
    Write-Host '[build] go build ./...' -ForegroundColor Cyan
    go build -o z-coc.exe ./cmd/server
    if ($LASTEXITCODE -ne 0) { throw "build failed (exit $LASTEXITCODE)" }
}

Write-Host "[start] $url" -ForegroundColor Cyan
# Ensure the smoke admin account is bootstrapped, preserving any existing value.
$adminEmails = 'smoke-admin@example.com'
if ($env:ADMIN_EMAILS) { $adminEmails = "$($env:ADMIN_EMAILS),$adminEmails" }
$env:ADMIN_EMAILS = $adminEmails
$proc = Start-Process -FilePath (Join-Path $root 'z-coc.exe') -PassThru -RedirectStandardOutput $outLog -RedirectStandardError $errLog
$proc.Id | Set-Content $pidFile

try {
    $ok = $false
    for ($i = 0; $i -lt 30; $i++) {
        try {
            if ((Invoke-RestMethod -Uri "$url/health.php" -TimeoutSec 3).status -eq 'ok') { $ok = $true; break }
        } catch { }
        if ($proc.HasExited) { break }
        Start-Sleep -Seconds 1
    }
    if (-not $ok) {
        if (Test-Path $errLog) { Get-Content $errLog -Tail 20 }
        throw "server did not become healthy at $url"
    }
    Write-Host '[health] ok' -ForegroundColor Green

    $suites = @(
        @{ name = 'base';    file = 'api-smoke.ps1' },
        @{ name = 'friends'; file = 'api-smoke-friends.ps1' },
        @{ name = 'lobby';   file = 'api-smoke-lobby.ps1' },
        @{ name = 'admin';   file = 'api-smoke-admin.ps1' }
    )
    $failed = 0
    foreach ($s in $suites) {
        Write-Host "===== $($s.name) =====" -ForegroundColor Yellow
        & powershell -NoProfile -ExecutionPolicy Bypass -File (Join-Path $root "tools\$($s.file)") -BaseUrl $url
        if ($LASTEXITCODE -ne 0) { $failed++ }
    }
    if ($failed -gt 0) { Write-Host "SMOKE FAILURES: $failed suite(s)" -ForegroundColor Red; exit 1 }
    Write-Host 'ALL SMOKE SUITES PASSED' -ForegroundColor Green
}
finally {
    if (-not $KeepRunning) {
        Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue
        Start-Sleep -Milliseconds 500
        if (Get-Process -Id $proc.Id -ErrorAction SilentlyContinue) {
            Write-Host "[stop] WARNING still running pid=$($proc.Id)" -ForegroundColor Red
        } else {
            Write-Host "[stop] pid=$($proc.Id) stopped" -ForegroundColor Cyan
        }
        Remove-Item $pidFile -Force -ErrorAction SilentlyContinue
    } else {
        Write-Host "[keep] pid=$($proc.Id) still running" -ForegroundColor Cyan
    }
}
