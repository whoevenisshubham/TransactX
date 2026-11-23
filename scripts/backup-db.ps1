# PostgreSQL Database Backup Script for TransactX
param(
    [string]$SourceDb = $(if ($env:TX_SOURCE_DB) { $env:TX_SOURCE_DB } else { "transactx_local_staging" }),
    [string]$BackupDir = "backups",
    [string]$HostName = $(if ($env:PGHOST) { $env:PGHOST } else { "127.0.0.1" }),
    [int]$Port = $(if ($env:PGPORT) { [int]$env:PGPORT } else { 5432 }),
    [string]$User = $(if ($env:PGUSER) { $env:PGUSER } else { "postgres" }),
    [string]$Password = $env:PGPASSWORD
)

$ErrorActionPreference = "Stop"
$root = (Get-Item $PSScriptRoot\..).FullName
$targetDir = Join-Path $root $BackupDir
New-Item -ItemType Directory -Force -Path $targetDir | Out-Null

$timestamp = (Get-Date).ToUniversalTime().ToString("yyyyMMddTHHmmssZ")
$backupFile = Join-Path $targetDir "transactx-backup-$timestamp.sql"

Write-Host "Creating backup of $SourceDb to $backupFile..."
if ($Password) {
    $env:PGPASSWORD = $Password
} elseif (-not $env:PGPASSWORD) {
    $env:PGPASSWORD = "1234"
}
& pg_dump -h $HostName -p $Port -U $User -d $SourceDb --clean --if-exists --no-owner --no-privileges -f $backupFile

if ($LASTEXITCODE -ne 0 -or !(Test-Path $backupFile) -or ((Get-Item $backupFile).Length -eq 0)) {
    Write-Error "pg_dump failed with exit code $LASTEXITCODE"
    exit 1
}

$size = (Get-Item $backupFile).Length
Write-Host "Backup completed successfully ($size bytes): $backupFile"
Write-Output $backupFile
