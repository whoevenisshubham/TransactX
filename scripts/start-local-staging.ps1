# Start TransactX Local Staging Stack
param(
    [string]$DbUrl = $(if ($env:DATABASE_URL) { $env:DATABASE_URL } else { "postgres://postgres:1234@127.0.0.1:5432/transactx_local_staging?sslmode=disable" }),
    [switch]$IncludeFrontend = $false
)

$ErrorActionPreference = "Stop"
$root = (Get-Item $PSScriptRoot\..).FullName
$logsDir = Join-Path $root "logs"
$scriptsDir = Join-Path $root "scripts"

New-Item -ItemType Directory -Force -Path $logsDir | Out-Null

Write-Host "Starting TransactX Local Staging Stack from $root..."

# Propagate Database URLs into environment
if ($DbUrl) {
    $env:DATABASE_URL = $DbUrl
    $env:BANK_A_DATABASE_URL = $DbUrl
    $env:BANK_B_DATABASE_URL = $DbUrl
}

if ($IncludeFrontend) {
    Write-Host "Verifying frontend build artifacts in frontend/dist..."
    if (-not (Test-Path "$root\frontend\dist\index.html")) {
        Write-Host "Building frontend assets..."
        Push-Location "$root\frontend"
        & npm run build
        Pop-Location
    }
}

function Start-Service {
    param([string]$Name, [string]$ScriptFile, [string]$LogName)
    $stdout = Join-Path $logsDir "$LogName.log"
    $stderr = Join-Path $logsDir "$LogName.err"
    "" | Set-Content $stdout
    "" | Set-Content $stderr
    $scriptPath = Join-Path $scriptsDir $ScriptFile
    $p = Start-Process powershell.exe -ArgumentList "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", "& '$scriptPath'" -WorkingDirectory $root -RedirectStandardOutput $stdout -RedirectStandardError $stderr -PassThru -WindowStyle Hidden
    Write-Host "Started $Name with PID $($p.Id)"
    return $p.Id
}

$pids = @{}
$pids["bank-a"] = Start-Service -Name "Bank A" -ScriptFile "run-bank-a.ps1" -LogName "bank-a"
$pids["bank-b"] = Start-Service -Name "Bank B" -ScriptFile "run-bank-b.ps1" -LogName "bank-b"
$pids["api"] = Start-Service -Name "API Server" -ScriptFile "run-api.ps1" -LogName "api"
$pids["worker"] = Start-Service -Name "Recovery Worker" -ScriptFile "run-worker.ps1" -LogName "worker"
$pids["caddy"] = Start-Service -Name "Caddy Ingress" -ScriptFile "run-caddy.ps1" -LogName "caddy"

$pids | ConvertTo-Json | Set-Content (Join-Path $logsDir "staging-pids.json")
Write-Host "All processes started. PIDs saved to logs/staging-pids.json."
