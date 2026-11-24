# TransactX Local Staging Resilient Supervisor
param(
    [string]$DbUrl,
    [string]$EnvFile
)

$ErrorActionPreference = "Stop"
$root = (Get-Item $PSScriptRoot\..).FullName
$logsDir = Join-Path $root "logs"
$scriptsDir = Join-Path $root "scripts"
$supervisorPidFile = Join-Path $logsDir "supervisor.pid"
$supervisorStopFile = Join-Path $logsDir "supervisor.stop"
$stagingPidsFile = Join-Path $logsDir "staging-pids.json"

New-Item -ItemType Directory -Force -Path $logsDir | Out-Null

# 1. Load environment configuration via shared loader
$loaderArgs = @{}
if ($EnvFile) { $loaderArgs["EnvFile"] = $EnvFile }
try {
    & "$scriptsDir\load-local-staging-env.ps1" @loaderArgs
} catch {
    [Console]::Error.WriteLine("Supervisor configuration error: $_")
    exit 1
}

# 2. CLI parameter precedence: $DbUrl overrides loaded database URLs if explicitly supplied
if ($DbUrl) {
    $env:DATABASE_URL = $DbUrl
    $env:BANK_A_DATABASE_URL = $DbUrl
    $env:BANK_B_DATABASE_URL = $DbUrl
}

# 3. Record supervisor PID
$PID | Set-Content $supervisorPidFile
Write-Host "[SUPERVISOR] TransactX Resilient Local Staging Supervisor started with PID $PID."

# 4. Clean up any leftover .stop flags
Get-ChildItem -Path $logsDir -Filter "*.stop" | Remove-Item -Force -ErrorAction SilentlyContinue

$serviceConfigs = [ordered]@{
    "bank-a" = @{ Script = "run-bank-a.ps1"; Name = "Bank A" }
    "bank-b" = @{ Script = "run-bank-b.ps1"; Name = "Bank B" }
    "api"    = @{ Script = "run-api.ps1";    Name = "Core API" }
    "worker" = @{ Script = "run-worker.ps1"; Name = "Recovery Worker" }
    "caddy"  = @{ Script = "run-caddy.ps1";  Name = "Caddy Ingress" }
}

$runningProcs = @{}

function Start-ManagedService {
    param([string]$Key)
    $cfg = $serviceConfigs[$Key]
    $stdout = Join-Path $logsDir "$Key.log"
    $stderr = Join-Path $logsDir "$Key.err"
    $scriptPath = Join-Path $scriptsDir $cfg.Script
    $p = Start-Process powershell.exe -ArgumentList "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", "& '$scriptPath'" -WorkingDirectory $root -RedirectStandardOutput $stdout -RedirectStandardError $stderr -PassThru -WindowStyle Hidden
    Write-Host "[SUPERVISOR] Started $($cfg.Name) ($Key) with PID $($p.Id)"
    return $p
}

try {
    # Initial start
    foreach ($key in $serviceConfigs.Keys) {
        $runningProcs[$key] = Start-ManagedService -Key $key
    }

    # Supervision loop
    while ($true) {
        # Check for global supervisor stop signal
        if (Test-Path $supervisorStopFile) {
            Write-Host "[SUPERVISOR] Stop flag detected ($supervisorStopFile). Initiating clean shutdown..."
            break
        }

        $pidMap = @{}
        foreach ($key in $serviceConfigs.Keys) {
            if (Test-Path $supervisorStopFile) {
                break
            }

            $stopFile = Join-Path $logsDir "$key.stop"
            $proc = $runningProcs[$key]
            $isAlive = ($null -ne $proc) -and (-not $proc.HasExited)

            if (Test-Path $stopFile) {
                if ($isAlive) {
                    Write-Host "[SUPERVISOR] Stop flag detected for $key, stopping PID $($proc.Id)..."
                    & taskkill.exe /PID $proc.Id /T /F 2>$null
                    $runningProcs[$key] = $null
                }
            } else {
                if (-not $isAlive) {
                    Write-Host "[SUPERVISOR] Service $key not running, launching..."
                    $runningProcs[$key] = Start-ManagedService -Key $key
                }
            }

            if ($runningProcs[$key] -and (-not $runningProcs[$key].HasExited)) {
                $pidMap[$key] = $runningProcs[$key].Id
            }
        }

        if (Test-Path $supervisorStopFile) {
            break
        }

        $pidMap | ConvertTo-Json | Set-Content $stagingPidsFile
        Start-Sleep -Seconds 2
    }
} finally {
    Write-Host "[SUPERVISOR] Shutting down all managed services..."
    foreach ($p in $runningProcs.Values) {
        if ($p -and (-not $p.HasExited)) {
            Write-Host "[SUPERVISOR] Terminating process PID $($p.Id) and tree..."
            & taskkill.exe /PID $p.Id /T /F 2>$null
        }
    }
    Remove-Item $supervisorPidFile -Force -ErrorAction SilentlyContinue
    Remove-Item $supervisorStopFile -Force -ErrorAction SilentlyContinue
    Remove-Item $stagingPidsFile -Force -ErrorAction SilentlyContinue
    Write-Host "[SUPERVISOR] Supervisor stopped and clean-up complete."
}
