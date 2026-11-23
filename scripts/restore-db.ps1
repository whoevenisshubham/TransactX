# PostgreSQL Database Restore Script for TransactX
param(
    [Parameter(Mandatory=$true)]
    [string]$BackupFile,
    [string]$TargetDb = $(if ($env:TX_TARGET_DB) { $env:TX_TARGET_DB } else { "transactx_restore_drill" }),
    [string]$HostName = $(if ($env:PGHOST) { $env:PGHOST } else { "127.0.0.1" }),
    [int]$Port = $(if ($env:PGPORT) { [int]$env:PGPORT } else { 5432 }),
    [string]$User = $(if ($env:PGUSER) { $env:PGUSER } else { "postgres" }),
    [string]$Password = $env:PGPASSWORD,
    [switch]$Force = $false
)

$ErrorActionPreference = "Continue"

# 1. Backup file validation
if (-not (Test-Path $BackupFile)) {
    [Console]::Error.WriteLine("Backup file not found: $BackupFile")
    exit 1
}
$fileItem = Get-Item $BackupFile
if ($fileItem.Length -eq 0) {
    [Console]::Error.WriteLine("Backup file is empty (0 bytes): $BackupFile")
    exit 1
}

# 2. Identifier validation (Regex allowlist: ^[A-Za-z_][A-Za-z0-9_]{0,62}$)
$identRegex = '^[A-Za-z_][A-Za-z0-9_]{0,62}$'
if ($TargetDb -notmatch $identRegex) {
    [Console]::Error.WriteLine("Invalid target database identifier: '$TargetDb'. Must match pattern: ^[A-Za-z_][A-Za-z0-9_]{0,62}$")
    exit 1
}

# 3. Source & System Database Protection
$configuredSource = if ($env:TX_SOURCE_DB) { $env:TX_SOURCE_DB } else { "transactx_local_staging" }
if ($TargetDb -and $configuredSource -and ($TargetDb.ToLowerInvariant() -eq $configuredSource.ToLowerInvariant())) {
    [Console]::Error.WriteLine("Safety violation: Target database cannot equal source database '$configuredSource'. Aborting to prevent accidental data destruction.")
    exit 1
}

$protectedDbs = @("postgres", "template0", "template1", "rdsadmin", "azure_maintenance")
if ($TargetDb -and ($protectedDbs -contains $TargetDb.ToLowerInvariant())) {
    [Console]::Error.WriteLine("Safety violation: Database '$TargetDb' is a protected system database. Restore target rejected.")
    exit 1
}

# 4. Credential Validation - No hardcoded passwords or fallback
if ($Password) {
    $env:PGPASSWORD = $Password
}
if (-not $env:PGPASSWORD) {
    [Console]::Error.WriteLine("PGPASSWORD is not configured. Set the PGPASSWORD environment variable or provide -Password parameter.")
    exit 1
}

# 5. Check if target database already exists and enforce -Force
$dbExistsOutput = & psql -h $HostName -p $Port -U $User -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname='$TargetDb'" 2>&1
if ($LASTEXITCODE -ne 0) {
    [Console]::Error.WriteLine("Failed to connect to PostgreSQL to check database existence: $dbExistsOutput")
    exit 1
}
$exists = ("$dbExistsOutput".Trim() -eq "1")

if ($exists) {
    if (-not $Force) {
        [Console]::Error.WriteLine("Target database '$TargetDb' already exists. Re-run with -Force to explicitly confirm dropping and recreating this database.")
        exit 1
    }
    Write-Host "Target database '$TargetDb' exists and -Force specified. Dropping existing target database..."
    # Terminate active connections first
    & psql -h $HostName -p $Port -U $User -d postgres -v ON_ERROR_STOP=1 -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '$TargetDb' AND pid <> pg_backend_pid();" 2>$null
    & dropdb -h $HostName -p $Port -U $User --if-exists $TargetDb
    if ($LASTEXITCODE -ne 0) {
        [Console]::Error.WriteLine("Failed to drop database '$TargetDb' (exit code $LASTEXITCODE).")
        exit 1
    }
}

# 6. Create target database using createdb tool
Write-Host "Creating target database '$TargetDb'..."
& createdb -h $HostName -p $Port -U $User $TargetDb
if ($LASTEXITCODE -ne 0) {
    [Console]::Error.WriteLine("createdb failed for '$TargetDb' with exit code $LASTEXITCODE.")
    exit 1
}

# 7. Restore with ON_ERROR_STOP=1
Write-Host "Restoring $BackupFile into '$TargetDb' with ON_ERROR_STOP=1..."
& psql -h $HostName -p $Port -U $User -d $TargetDb -v ON_ERROR_STOP=1 -q -f $BackupFile
if ($LASTEXITCODE -ne 0) {
    [Console]::Error.WriteLine("psql restore failed with exit code $LASTEXITCODE. Target database '$TargetDb' may be incomplete.")
    exit 1
}

Write-Host "Restore into '$TargetDb' completed successfully."
