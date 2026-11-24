# Stop TransactX Local Staging Stack
param(
    [int]$TimeoutSeconds = 10
)

$ErrorActionPreference = "SilentlyContinue"
$root = (Get-Item $PSScriptRoot\..).FullName
$binDir = Join-Path $root "bin"
$logsDir = Join-Path $root "logs"
$supervisorPidFile = Join-Path $logsDir "supervisor.pid"
$supervisorStopFile = Join-Path $logsDir "supervisor.stop"
$stagingPidsFile = Join-Path $logsDir "staging-pids.json"

Write-Host "Stopping TransactX Local Staging Stack..."

# 1. Signal supervisor to stop cleanly
"stop" | Set-Content $supervisorStopFile

# 2. Wait up to $TimeoutSeconds for supervisor to terminate
if (Test-Path $supervisorPidFile) {
    $supPidStr = (Get-Content $supervisorPidFile -ErrorAction SilentlyContinue).Trim()
    if ($supPidStr -match '^\d+$') {
        $supPid = [int]$supPidStr
        Write-Host "Waiting up to $TimeoutSeconds seconds for supervisor (PID $supPid) to exit cleanly..."
        $elapsed = 0
        while ($elapsed -lt $TimeoutSeconds) {
            $supProc = Get-Process -Id $supPid -ErrorAction SilentlyContinue
            if (-not $supProc -or $supProc.HasExited) {
                Write-Host "Supervisor process (PID $supPid) exited cleanly."
                break
            }
            Start-Sleep -Seconds 1
            $elapsed++
        }
        $supProc = Get-Process -Id $supPid -ErrorAction SilentlyContinue
        if ($supProc -and (-not $supProc.HasExited)) {
            Write-Host "Supervisor (PID $supPid) did not exit within $TimeoutSeconds seconds. Forcing tree termination..."
            & taskkill.exe /PID $supPid /T /F 2>$null
        }
    }
}

# 3. Terminate any remaining child processes explicitly tracked in staging-pids.json
if (Test-Path $stagingPidsFile) {
    $pidsJson = Get-Content $stagingPidsFile -Raw -ErrorAction SilentlyContinue
    if ($pidsJson) {
        $pids = $pidsJson | ConvertFrom-Json -ErrorAction SilentlyContinue
        if ($pids) {
            foreach ($prop in $pids.PSObject.Properties) {
                $pidNum = $prop.Value
                $name = $prop.Name
                $proc = Get-Process -Id $pidNum -ErrorAction SilentlyContinue
                if ($proc -and (-not $proc.HasExited)) {
                    Write-Host "Stopping TransactX $name (PID $pidNum and tree)..."
                    & taskkill.exe /PID $pidNum /T /F 2>$null
                }
            }
        }
    }
}

# 4. Terminate any lingering processes whose binary is located specifically in $root\bin
Get-Process | Where-Object {
    try {
        $_.Path -and ($_.Path.StartsWith($binDir, [System.StringComparison]::OrdinalIgnoreCase))
    } catch {
        $false
    }
} | ForEach-Object {
    Write-Host "Stopping lingering repository process $($_.ProcessName) (PID $($_.Id))..."
    & taskkill.exe /PID $_.Id /T /F 2>$null
}

# 5. Verify local staging ports (8080, 8081, 8082, 3000) are released
$ports = @(8080, 8081, 8082, 3000)
$occupiedPorts = @()
foreach ($port in $ports) {
    $conn = Get-NetTCPConnection -LocalPort $port -State Listen -ErrorAction SilentlyContinue
    if ($conn) {
        $occupiedPorts += $port
        [Console]::Error.WriteLine("ERROR: Port $port is still in use by PID $($conn.OwningProcess)!")
    }
}

# 6. Check if supervisor or any staging process is still running
$lingeringProcesses = Get-Process | Where-Object {
    try {
        $_.Path -and ($_.Path.StartsWith($binDir, [System.StringComparison]::OrdinalIgnoreCase))
    } catch {
        $false
    }
}

if ($occupiedPorts.Count -gt 0 -or $lingeringProcesses.Count -gt 0) {
    [Console]::Error.WriteLine("TransactX local staging shutdown INCOMPLETE. Lingering processes: $($lingeringProcesses.Count), occupied ports: $($occupiedPorts -join ', ').")
    exit 1
}

# 7. Clean up supervisor and staging PID/stop files only when shutdown is verified
Remove-Item $supervisorPidFile -Force -ErrorAction SilentlyContinue
Remove-Item $supervisorStopFile -Force -ErrorAction SilentlyContinue
Remove-Item $stagingPidsFile -Force -ErrorAction SilentlyContinue
Get-ChildItem -Path $logsDir -Filter "*.stop" | Remove-Item -Force -ErrorAction SilentlyContinue

Write-Host "All TransactX staging ports (8080, 8081, 8082, 3000) verified free."
Write-Host "TransactX local staging shutdown complete."
exit 0
