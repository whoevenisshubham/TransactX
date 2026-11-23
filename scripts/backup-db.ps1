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
trap {
    exit 1
}

# 1. Identifier Validation (Regex allowlist: ^[A-Za-z_][A-Za-z0-9_]{0,62}$)
$identRegex = '^[A-Za-z_][A-Za-z0-9_]{0,62}$'
if ($SourceDb -notmatch $identRegex) {
    Write-Error "Invalid source database identifier: '$SourceDb'. Must match pattern: ^[A-Za-z_][A-Za-z0-9_]{0,62}$"
    exit 1
}

# 2. Credential Validation - No hardcoded passwords or fallback
if ($Password) {
    $env:PGPASSWORD = $Password
}
if (-not $env:PGPASSWORD) {
    Write-Error "PGPASSWORD is not configured. Set the PGPASSWORD environment variable or provide -Password parameter."
    exit 1
}

$root = (Get-Item $PSScriptRoot\..).FullName
$targetDir = Join-Path $root $BackupDir
New-Item -ItemType Directory -Force -Path $targetDir | Out-Null

$timestamp = (Get-Date).ToUniversalTime().ToString("yyyyMMddTHHmmssZ")
$backupFile = Join-Path $targetDir "transactx-backup-$timestamp.sql"

Write-Host "Creating backup of $SourceDb to $backupFile..."

# Execute pg_dump with safe argument separation
& pg_dump -h $HostName -p $Port -U $User -d $SourceDb --clean --if-exists --no-owner --no-privileges -f $backupFile

if ($LASTEXITCODE -ne 0) {
    Write-Error "pg_dump failed with exit code $LASTEXITCODE"
    if (Test-Path $backupFile) { Remove-Item $backupFile -Force -ErrorAction SilentlyContinue }
    exit 1
}

if (-not (Test-Path $backupFile) -or ((Get-Item $backupFile).Length -eq 0)) {
    Write-Error "Backup failed: generated backup file is missing or 0 bytes."
    if (Test-Path $backupFile) { Remove-Item $backupFile -Force -ErrorAction SilentlyContinue }
    exit 1
}

$size = (Get-Item $backupFile).Length
Write-Host "Backup completed successfully ($size bytes): $backupFile"
Write-Output $backupFile
