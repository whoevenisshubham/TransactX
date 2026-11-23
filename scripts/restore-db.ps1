# PostgreSQL Database Restore Script for TransactX
param(
    [Parameter(Mandatory=$true)]
    [string]$BackupFile,
    [string]$TargetDb = $(if ($env:TX_TARGET_DB) { $env:TX_TARGET_DB } else { "transactx_restore_drill" }),
    [string]$HostName = $(if ($env:PGHOST) { $env:PGHOST } else { "127.0.0.1" }),
    [int]$Port = $(if ($env:PGPORT) { [int]$env:PGPORT } else { 5432 }),
    [string]$User = $(if ($env:PGUSER) { $env:PGUSER } else { "postgres" }),
    [string]$Password = $env:PGPASSWORD
)

$ErrorActionPreference = "Stop"
if (-not (Test-Path $BackupFile)) {
    Write-Error "Backup file not found: $BackupFile"
    exit 1
}

Write-Host "Creating target database $TargetDb..."
if ($Password) {
    $env:PGPASSWORD = $Password
} elseif (-not $env:PGPASSWORD) {
    $env:PGPASSWORD = "1234"
}
& psql -h $HostName -p $Port -U $User -d postgres -c "DROP DATABASE IF EXISTS $TargetDb WITH (FORCE);"
& psql -h $HostName -p $Port -U $User -d postgres -c "CREATE DATABASE $TargetDb;"

Write-Host "Restoring $BackupFile into $TargetDb..."
& psql -h $HostName -p $Port -U $User -d $TargetDb -q -f $BackupFile

if ($LASTEXITCODE -ne 0) {
    Write-Error "psql restore failed with exit code $LASTEXITCODE"
    exit 1
}

Write-Host "Restore into $TargetDb completed successfully."
