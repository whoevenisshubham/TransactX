# Start TransactX Local Staging Stack via Unified Supervisor
param(
    [string]$DbUrl,
    [string]$EnvFile,
    [switch]$IncludeFrontend = $false,
    [switch]$Foreground = $false
)

$ErrorActionPreference = "Stop"
$root = (Get-Item $PSScriptRoot\..).FullName
$scriptsDir = Join-Path $root "scripts"
$logsDir = Join-Path $root "logs"
$supervisorPidFile = Join-Path $logsDir "supervisor.pid"

New-Item -ItemType Directory -Force -Path $logsDir | Out-Null

if ($IncludeFrontend) {
    Write-Host "Verifying frontend build artifacts in frontend/dist..."
    if (-not (Test-Path "$root\frontend\dist\index.html")) {
        Write-Host "Building frontend assets..."
        Push-Location "$root\frontend"
        & npm run build
        Pop-Location
    }
}

if ($Foreground) {
    Write-Host "Starting TransactX Local Staging Supervisor in foreground..."
    $params = @{}
    if ($DbUrl) { $params["DbUrl"] = $DbUrl }
    if ($EnvFile) { $params["EnvFile"] = $EnvFile }
    & "$scriptsDir\supervise-local-staging.ps1" @params
} else {
    Write-Host "Starting TransactX Local Staging Supervisor in background..."
    $argsStr = "-NoProfile -ExecutionPolicy Bypass -File `"$scriptsDir\supervise-local-staging.ps1`""
    if ($DbUrl) { $argsStr += " -DbUrl `"$DbUrl`"" }
    if ($EnvFile) { $argsStr += " -EnvFile `"$EnvFile`"" }

    $p = Start-Process powershell.exe -ArgumentList $argsStr -WorkingDirectory $root -PassThru -WindowStyle Hidden

    # Wait up to 10 seconds for supervisor to record PID
    $elapsed = 0
    while ($elapsed -lt 10) {
        if (Test-Path $supervisorPidFile) {
            $supPid = (Get-Content $supervisorPidFile).Trim()
            Write-Host "TransactX Local Staging Supervisor started with PID $supPid (Process ID $($p.Id))."
            return
        }
        Start-Sleep -Milliseconds 500
        $elapsed += 0.5
    }
    Write-Host "Supervisor launched with PID $($p.Id). Check logs/supervisor.pid."
}
