# PostgreSQL Database Backup Script for TransactX
param(
    [string]$SourceDb = "transactx_local_staging",
    [string]$BackupDir = "backups",
    [string]$HostName = "127.0.0.1",
    [int]$Port = 5432,
    [string]$User = "postgres"
)

$ErrorActionPreference = "Stop"
$root = (Get-Item $PSScriptRoot\..).FullName
$targetDir = Join-Path $root $BackupDir
New-Item -ItemType Directory -Force -Path $targetDir | Out-Null

$timestamp = (Get-Date).ToUniversalTime().ToString("yyyyMMddTHHmmssZ")
$backupFile = Join-Path $targetDir "transactx-backup-$timestamp.sql"

Write-Host "Creating backup of $SourceDb to $backupFile..."
$env:PGPASSWORD = "1234"
& pg_dump -h $HostName -p $Port -U $User -d $SourceDb --clean --if-exists --no-owner --no-privileges -f $backupFile

if ($LASTEXITCODE -ne 0 -or !(Test-Path $backupFile) -or ((Get-Item $backupFile).Length -eq 0)) {
    Write-Error "pg_dump failed with exit code $LASTEXITCODE"
    exit 1
}

$size = (Get-Item $backupFile).Length
Write-Host "Backup completed successfully ($size bytes): $backupFile"
Write-Output $backupFile
