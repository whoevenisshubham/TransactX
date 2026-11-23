# Test TransactX Local Staging Lifecycle
$ErrorActionPreference = "Continue"
$root = (Get-Item $PSScriptRoot\..).FullName
Set-Location $root

Write-Host "=== TEST: Starting supervisor in background ==="
$sup = Start-Process powershell.exe -ArgumentList "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", "scripts\supervise-local-staging.ps1" -PassThru -WindowStyle Hidden
Start-Sleep -Seconds 6

Write-Host "Checking supervisor.pid..."
$supPid = $null
if (Test-Path "logs\supervisor.pid") {
    $supPid = (Get-Content "logs\supervisor.pid").Trim()
    Write-Host "Supervisor recorded PID: $supPid (Process Id: $($sup.Id))"
} else {
    Write-Host "ERROR: supervisor.pid not found!"
}

Write-Host "Checking API /readyz on port 8080..."
try {
    $resp = Invoke-RestMethod -Uri "http://127.0.0.1:8080/readyz" -TimeoutSec 3
    Write-Host "API /readyz status: $($resp.status)"
} catch {
    Write-Host "API check error: $_"
}

Write-Host "Checking Caddy ingress /readyz on port 3000..."
try {
    $caddyResp = Invoke-RestMethod -Uri "http://127.0.0.1:3000/readyz" -TimeoutSec 3
    Write-Host "Caddy /readyz status: $($caddyResp.status)"
} catch {
    Write-Host "Caddy check error: $_"
}

Write-Host "`n=== TEST: Stopping staging stack via stop-local-staging.ps1 ==="
& "scripts\stop-local-staging.ps1"

Start-Sleep -Seconds 3
Write-Host "Checking if supervisor is still running..."
$supStillRunning = Get-Process -Id $sup.Id -ErrorAction SilentlyContinue
if ($supStillRunning -and (-not $supStillRunning.HasExited)) {
    Write-Host "FAIL: Supervisor still running!"
    Stop-Process -Id $sup.Id -Force -ErrorAction SilentlyContinue
} else {
    Write-Host "PASS: Supervisor exited cleanly!"
}

$pidExists = Test-Path "logs\supervisor.pid"
$stopExists = Test-Path "logs\supervisor.stop"
$stagingPidsExists = Test-Path "logs\staging-pids.json"

Write-Host "supervisor.pid exists: $pidExists"
Write-Host "supervisor.stop exists: $stopExists"
Write-Host "staging-pids.json exists: $stagingPidsExists"

if (-not $pidExists -and -not $stopExists -and -not $stagingPidsExists) {
    Write-Host "`nALL LIFECYCLE CHECKS PASSED!"
} else {
    Write-Host "`nSOME LIFECYCLE CHECKS FAILED!"
}
