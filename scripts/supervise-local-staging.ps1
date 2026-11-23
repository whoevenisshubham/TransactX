# TransactX Local Staging Resilient Supervisor
param(
    [string]$DbUrl = "postgres://postgres:1234@127.0.0.1:5432/transactx_local_staging?sslmode=disable"
)

$ErrorActionPreference = "Stop"
$root = (Get-Item $PSScriptRoot\..).FullName
$logsDir = Join-Path $root "logs"
$scriptsDir = Join-Path $root "scripts"

New-Item -ItemType Directory -Force -Path $logsDir | Out-Null

# Clean up any leftover .stop flags
Get-ChildItem -Path $logsDir -Filter "*.stop" | Remove-Item -Force -ErrorAction SilentlyContinue

Write-Host "TransactX Resilient Local Staging Supervisor starting..."

$serviceConfigs = @{
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
    foreach ($key in @("bank-a", "bank-b", "api", "worker", "caddy")) {
        $runningProcs[$key] = Start-ManagedService -Key $key
    }

    # Supervision loop
    while ($true) {
        $pidMap = @{}
        foreach ($key in $serviceConfigs.Keys) {
            $stopFile = Join-Path $logsDir "$key.stop"
            $proc = $runningProcs[$key]
            $isAlive = ($null -ne $proc) -and (-not $proc.HasExited)

            if (Test-Path $stopFile) {
                if ($isAlive) {
                    Write-Host "[SUPERVISOR] Stop flag detected for $key, stopping PID $($proc.Id)..."
                    Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue
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

        $pidMap | ConvertTo-Json | Set-Content (Join-Path $logsDir "staging-pids.json")
        Start-Sleep -Seconds 2
    }
} finally {
    Write-Host "[SUPERVISOR] Shutting down all managed services..."
    foreach ($p in $runningProcs.Values) {
        if ($p -and (-not $p.HasExited)) {
            Stop-Process -Id $p.Id -Force -ErrorAction SilentlyContinue
        }
    }
}
