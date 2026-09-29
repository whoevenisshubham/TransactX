param(
    [Parameter(Mandatory = $true)]
    [string]$DatabaseUrl,
    [Parameter(Mandatory = $true)]
    [string]$PostgresDataDirectory,
    [string]$PgCtl = "C:\Program Files\PostgreSQL\18\bin\pg_ctl.exe",
    [string]$PostgresOptions = "-p 55432 -h 127.0.0.1"
)

$ErrorActionPreference = "Stop"
$backendDirectory = Split-Path -Parent $PSScriptRoot
$statePath = Join-Path $backendDirectory ".restart-evidence.json"

try {
    $env:DATABASE_URL = $DatabaseUrl
    $env:M3_RESTART_STATE_PATH = $statePath
    $env:M3_RESTART_PHASE = "prepare"
    & go test -count=1 -run '^TestDurableCommitmentSurvivesPostgresRestart$' -v ./internal/reconciliation
    if ($LASTEXITCODE -ne 0) { throw "restart prepare phase failed" }

    & $PgCtl -D $PostgresDataDirectory restart -m fast -w -o $PostgresOptions
    if ($LASTEXITCODE -ne 0) { throw "PostgreSQL restart failed" }

    $env:M3_RESTART_PHASE = "verify"
    & go test -count=1 -run '^TestDurableCommitmentSurvivesPostgresRestart$' -v ./internal/reconciliation
    if ($LASTEXITCODE -ne 0) { throw "restart verification phase failed" }
}
finally {
    Remove-Item Env:M3_RESTART_PHASE -ErrorAction SilentlyContinue
    Remove-Item Env:M3_RESTART_STATE_PATH -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $statePath -ErrorAction SilentlyContinue
}
