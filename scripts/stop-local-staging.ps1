# Stop TransactX Local Staging Stack
$ErrorActionPreference = "SilentlyContinue"
$root = (Get-Item $PSScriptRoot\..).FullName
$pidFile = Join-Path $root "logs\staging-pids.json"

if (Test-Path $pidFile) {
    $pids = Get-Content $pidFile | ConvertFrom-Json
    foreach ($prop in $pids.PSObject.Properties) {
        $pidNum = $prop.Value
        $name = $prop.Name
        Write-Host "Stopping $name (PID $pidNum)..."
        Stop-Process -Id $pidNum -Force -ErrorAction SilentlyContinue
    }
    Remove-Item $pidFile -Force -ErrorAction SilentlyContinue
}

# Also ensure any lingering bin instances are stopped
Get-Process -Name "api", "bank-a", "bank-b", "worker", "caddy" -ErrorAction SilentlyContinue | ForEach-Object {
    Write-Host "Stopping lingering process $($_.ProcessName) (PID $($_.Id))..."
    Stop-Process -Id $_.Id -Force -ErrorAction SilentlyContinue
}

Write-Host "All TransactX local staging processes stopped."
