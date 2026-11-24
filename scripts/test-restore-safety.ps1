# TransactX Hermetic Database Restore Safety Acceptance Suite (M20-R.5.3)
param(
    [string]$HostName = "127.0.0.1",
    [int]$Port = 5432,
    [string]$User = "postgres",
    [string]$Password = $env:PGPASSWORD
)

$ErrorActionPreference = "Continue"
$root = (Get-Item $PSScriptRoot\..).FullName
$scriptsDir = Join-Path $root "scripts"
$testArtifactsDir = Join-Path $root "logs\restore-tests"
New-Item -ItemType Directory -Force -Path $testArtifactsDir | Out-Null

# 1. Credential Validation - No hardcoded fallback
if (-not $Password) {
    [Console]::Error.WriteLine("PGPASSWORD is not configured. Provide -Password parameter or set PGPASSWORD environment variable.")
    exit 1
}
$env:PGPASSWORD = $Password

$results = @()
function Record-TestResult {
    param([string]$TestID, [string]$Description, [bool]$Passed, [string]$Details)
    $obj = [PSCustomObject]@{
        TestID      = $TestID
        Description = $Description
        Result      = if ($Passed) { "PASS" } else { "FAIL" }
        Details     = $Details
    }
    $script:results += $obj
    Write-Host ("[{0}] {1}: {2}" -f $obj.Result, $TestID, $Description)
    if (-not $Passed) {
        Write-Host "   Details: $Details" -ForegroundColor Red
    }
}

# Unique run identifier for disposable databases
$runId = (Get-Random -Minimum 100000 -Maximum 999999).ToString()
$disposableSourceDb = "transactx_disposable_source_$runId"
$freshRestoreDb = "transactx_disposable_fresh_$runId"
$existingDb = "transactx_disposable_exist_$runId"
$errDb = "transactx_disposable_sqlerr_$runId"

$createdDbs = @()

# Verify initial baseline of transactx_local_staging
$originalStagingExists = (& psql -h $HostName -p $Port -U $User -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname='transactx_local_staging'" 2>$null)
$initialTableCount = 0
if ("$originalStagingExists".Trim() -eq "1") {
    $initialTableCountStr = & psql -h $HostName -p $Port -U $User -d transactx_local_staging -tAc "SELECT count(*) FROM information_schema.tables WHERE table_schema='public';" 2>$null
    if ($initialTableCountStr -match '^\d+$') {
        $initialTableCount = [int]$initialTableCountStr
    }
}

try {
    # 2. Setup: Create and populate unique disposable source database
    Write-Host "Setting up disposable source database '$disposableSourceDb'..."
    & createdb -h $HostName -p $Port -U $User $disposableSourceDb 2>&1 | Out-Null
    $createdDbs += $disposableSourceDb

    $seedSql = @"
CREATE TABLE test_users (
    id UUID PRIMARY KEY,
    username VARCHAR(50) NOT NULL
);
CREATE TABLE test_ledger (
    entry_id SERIAL PRIMARY KEY,
    user_id UUID REFERENCES test_users(id),
    amount BIGINT NOT NULL
);
INSERT INTO test_users VALUES ('a0000000-0000-0000-0000-000000000001', 'alice');
INSERT INTO test_ledger (user_id, amount) VALUES ('a0000000-0000-0000-0000-000000000001', 5000);
"@
    $seedFile = Join-Path $testArtifactsDir "source_seed.sql"
    $seedSql | Set-Content $seedFile -Encoding UTF8
    & psql -h $HostName -p $Port -U $User -d $disposableSourceDb -v ON_ERROR_STOP=1 -f $seedFile 2>&1 | Out-Null

    # Set TX_SOURCE_DB to the disposable source database
    $env:TX_SOURCE_DB = $disposableSourceDb

    # 3. Generate test backup using the real backup-db.ps1 script
    $backupOutput = & powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$scriptsDir\backup-db.ps1" -SourceDb $disposableSourceDb -BackupDir "logs\restore-tests" -HostName $HostName -Port $Port -User $User -Password $Password 2>&1
    $validBackupFile = ($backupOutput | Select-Object -Last 1).Trim()

    if ($LASTEXITCODE -ne 0 -or -not (Test-Path $validBackupFile)) {
        Write-Error "Failed to generate initial test backup using backup-db.ps1: $($backupOutput -join ' ')"
        exit 1
    }

    # TEST-RESTORE-01: Valid restore into fresh disposable database
    $createdDbs += $freshRestoreDb
    $out1 = & powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$scriptsDir\restore-db.ps1" -BackupFile $validBackupFile -TargetDb $freshRestoreDb -Password $Password 2>&1
    $rowCountStr = & psql -h $HostName -p $Port -U $User -d $freshRestoreDb -tAc "SELECT count(*) FROM test_ledger;" 2>$null
    $pass1 = ($LASTEXITCODE -eq 0) -and (($out1 | Out-String) -match "completed successfully") -and ("$rowCountStr".Trim() -eq "1")
    Record-TestResult -TestID "TEST-RESTORE-01" -Description "Valid restore into a fresh disposable database with data integrity" -Passed $pass1 -Details ($out1 -join "`n")

    # TEST-RESTORE-02: Restore into existing disposable database with explicit -Force
    $createdDbs += $existingDb
    & createdb -h $HostName -p $Port -U $User $existingDb 2>&1 | Out-Null
    $out2 = & powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$scriptsDir\restore-db.ps1" -BackupFile $validBackupFile -TargetDb $existingDb -Force -Password $Password 2>&1
    $pass2 = ($LASTEXITCODE -eq 0) -and (($out2 | Out-String) -match "exists and -Force specified") -and (($out2 | Out-String) -match "completed successfully")
    Record-TestResult -TestID "TEST-RESTORE-02" -Description "Restore into an existing disposable database with explicit -Force" -Passed $pass2 -Details ($out2 -join "`n")

    # TEST-RESTORE-02B: Restore into existing disposable database without -Force is rejected
    $out2b = & powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$scriptsDir\restore-db.ps1" -BackupFile $validBackupFile -TargetDb $existingDb -Password $Password 2>&1
    $pass2b = ($LASTEXITCODE -ne 0) -and (($out2b | Out-String) -match "already exists. Re-run with -Force")
    Record-TestResult -TestID "TEST-RESTORE-02B" -Description "Restore into an existing disposable database without -Force is rejected" -Passed $pass2b -Details ($out2b -join "`n")

    # TEST-RESTORE-03: Target equals custom configured source database (TX_SOURCE_DB) is rejected
    $out3 = & powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$scriptsDir\restore-db.ps1" -BackupFile $validBackupFile -TargetDb $disposableSourceDb -Force -Password $Password 2>&1
    $pass3 = ($LASTEXITCODE -ne 0) -and (($out3 | Out-String) -match "Safety violation: Target database cannot equal source database")
    Record-TestResult -TestID "TEST-RESTORE-03" -Description "Target equals custom source database (TX_SOURCE_DB) is rejected" -Passed $pass3 -Details ($out3 -join "`n")

    # TEST-RESTORE-04: System database (postgres) selected as restore target is rejected
    $out4 = & powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$scriptsDir\restore-db.ps1" -BackupFile $validBackupFile -TargetDb "postgres" -Force -Password $Password 2>&1
    $pass4 = ($LASTEXITCODE -ne 0) -and (($out4 | Out-String) -match "Safety violation: Database 'postgres' is a protected system database")
    Record-TestResult -TestID "TEST-RESTORE-04" -Description "System database (postgres) selected as restore target is rejected" -Passed $pass4 -Details ($out4 -join "`n")

    # TEST-RESTORE-05: Invalid identifier format is rejected
    $out5 = & powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$scriptsDir\restore-db.ps1" -BackupFile $validBackupFile -TargetDb "123-invalid-name!" -Password $Password 2>&1
    $pass5 = ($LASTEXITCODE -ne 0) -and (($out5 | Out-String) -match "Invalid target database identifier")
    Record-TestResult -TestID "TEST-RESTORE-05" -Description "Invalid identifier format is rejected" -Passed $pass5 -Details ($out5 -join "`n")

    # TEST-RESTORE-06: SQL-injection-style identifier is rejected before invocation
    $out6 = & powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$scriptsDir\restore-db.ps1" -BackupFile $validBackupFile -TargetDb "target_db; DROP DATABASE postgres; --" -Password $Password 2>&1
    $pass6 = ($LASTEXITCODE -ne 0) -and (($out6 | Out-String) -match "Invalid target database identifier")
    Record-TestResult -TestID "TEST-RESTORE-06" -Description "SQL-injection-style database identifier is rejected before invocation" -Passed $pass6 -Details ($out6 -join "`n")

    # TEST-RESTORE-07: Missing password without inherited credentials is rejected
    $cmd7 = "`$env:PGPASSWORD = ''; & '$scriptsDir\restore-db.ps1' -BackupFile '$validBackupFile' -TargetDb 'transactx_disposable_nopass' -Password ''"
    $out7 = & powershell.exe -NoProfile -ExecutionPolicy Bypass -Command $cmd7 2>&1
    $pass7 = ($LASTEXITCODE -ne 0) -and (($out7 | Out-String) -match "PGPASSWORD is not configured")
    Record-TestResult -TestID "TEST-RESTORE-07" -Description "Missing password configuration is rejected without inherited credentials" -Passed $pass7 -Details ($out7 -join "`n")

    # TEST-RESTORE-08: Empty (0 bytes) backup file is rejected
    $emptyBackupFile = Join-Path $testArtifactsDir "empty_backup.sql"
    New-Item -ItemType File -Path $emptyBackupFile -Force | Out-Null
    $out8 = & powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$scriptsDir\restore-db.ps1" -BackupFile $emptyBackupFile -TargetDb "transactx_disposable_empty" -Password $Password 2>&1
    $pass8 = ($LASTEXITCODE -ne 0) -and (($out8 | Out-String) -match "empty \(0 bytes\)")
    Record-TestResult -TestID "TEST-RESTORE-08" -Description "Empty (0 bytes) backup file is rejected" -Passed $pass8 -Details ($out8 -join "`n")

    # TEST-RESTORE-09: Simulated SQL restore error triggers ON_ERROR_STOP=1 and halts
    $createdDbs += $errDb
    $corruptSqlFile = Join-Path $testArtifactsDir "syntax_error_backup.sql"
    $errSql = @"
CREATE TABLE pre_err_table (id INT PRIMARY KEY);
INSERT INTO non_existent_table VALUES (1);
CREATE TABLE post_err_table (id INT PRIMARY KEY);
"@
    $errSql | Set-Content $corruptSqlFile -Encoding UTF8
    $out9 = & powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$scriptsDir\restore-db.ps1" -BackupFile $corruptSqlFile -TargetDb $errDb -Password $Password 2>&1
    $exitCode9 = $LASTEXITCODE
    $pass9 = ($exitCode9 -ne 0) -and (($out9 | Out-String) -match "psql restore failed with exit code")
    Record-TestResult -TestID "TEST-RESTORE-09" -Description "Simulated SQL restore error triggers ON_ERROR_STOP=1 and halts" -Passed $pass9 -Details ($out9 -join "`n")

    # TEST-RESTORE-10: Verify statement following deliberate error did NOT execute (post_err_table does NOT exist)
    $postErrTableExists = & psql -h $HostName -p $Port -U $User -d $errDb -tAc "SELECT 1 FROM information_schema.tables WHERE table_name='post_err_table';" 2>$null
    $pass10 = ("$postErrTableExists".Trim() -ne "1")
    Record-TestResult -TestID "TEST-RESTORE-10" -Description "Statements after failing SQL statement did not execute (ON_ERROR_STOP verified)" -Passed $pass10 -Details "post_err_table exists: $([string]::IsNullOrWhiteSpace($postErrTableExists) -eq $false)"

    # TEST-RESTORE-11: Failed restore returns non-zero exit code and never reports success
    $pass11 = ($exitCode9 -ne 0) -and (($out9 | Out-String) -notmatch "completed successfully")
    Record-TestResult -TestID "TEST-RESTORE-11" -Description "Failed restore returns non-zero exit code and never reports success" -Passed $pass11 -Details "ExitCode=$exitCode9, Success reported=$((($out9 | Out-String) -match 'completed successfully'))"

    # TEST-RESTORE-12: Original local staging database (transactx_local_staging) remains completely untouched
    $finalStagingExists = (& psql -h $HostName -p $Port -U $User -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname='transactx_local_staging'" 2>$null)
    $finalTableCount = 0
    if ("$finalStagingExists".Trim() -eq "1") {
        $finalTableCountStr = & psql -h $HostName -p $Port -U $User -d transactx_local_staging -tAc "SELECT count(*) FROM information_schema.tables WHERE table_schema='public';" 2>$null
        if ($finalTableCountStr -match '^\d+$') {
            $finalTableCount = [int]$finalTableCountStr
        }
    }
    $pass12 = ("$originalStagingExists".Trim() -eq "$finalStagingExists".Trim()) -and ($initialTableCount -eq $finalTableCount)
    Record-TestResult -TestID "TEST-RESTORE-12" -Description "Original staging database (transactx_local_staging) remained completely untouched" -Passed $pass12 -Details "Initial tables: $initialTableCount, Final tables: $finalTableCount"

} finally {
    Write-Host "`nCleaning up disposable test databases and artifacts..."
    foreach ($db in ($createdDbs | Select-Object -Unique)) {
        & psql -h $HostName -p $Port -U $User -d postgres -v ON_ERROR_STOP=1 -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '$db' AND pid <> pg_backend_pid();" 2>$null | Out-Null
        & dropdb -h $HostName -p $Port -U $User --if-exists $db 2>$null
    }
    Remove-Item -Path $testArtifactsDir -Recurse -Force -ErrorAction SilentlyContinue
}

Write-Host "`nSummary of Hermetic Restore Safety Tests:"
$results | Format-Table -AutoSize
$allPassed = ($results | Where-Object { $_.Result -ne "PASS" }).Count -eq 0
if (-not $allPassed) {
    Write-Error "Some restore safety tests failed"
    exit 1
}
Write-Host "All 12 Hermetic Restore Safety Tests PASSED!"
