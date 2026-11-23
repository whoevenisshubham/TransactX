# TransactX Database Restore Safety Acceptance Suite (M20-R.5.2)
param(
    [string]$HostName = "127.0.0.1",
    [int]$Port = 5432,
    [string]$User = "postgres",
    [string]$Password = "1234"
)

$ErrorActionPreference = "Continue"
$root = (Get-Item $PSScriptRoot\..).FullName
$scriptsDir = Join-Path $root "scripts"
$testArtifactsDir = Join-Path $root "logs\restore-tests"
New-Item -ItemType Directory -Force -Path $testArtifactsDir | Out-Null

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

# Pre-requisite: Generate a valid minimal test backup using clean transactx_local_staging
$env:PGPASSWORD = $Password
$validBackupFile = Join-Path $testArtifactsDir "valid_test_backup.sql"
& pg_dump -h $HostName -p $Port -U $User -d transactx_local_staging --clean --if-exists --no-owner --no-privileges -f $validBackupFile
if ($LASTEXITCODE -ne 0 -or !(Test-Path $validBackupFile)) {
    Write-Error "Failed to generate initial test backup"
    exit 1
}

# 1. Valid restore into a fresh disposable database
$freshDb = "transactx_disposable_fresh_$(Get-Random)"
$env:PGPASSWORD = $Password
$output1 = & powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$scriptsDir\restore-db.ps1" -BackupFile $validBackupFile -TargetDb $freshDb 2>&1
$pass1 = ($LASTEXITCODE -eq 0) -and (($output1 | Out-String) -match "completed successfully")
Record-TestResult -TestID "TEST-RESTORE-01" -Description "Valid restore into a fresh disposable database" -Passed $pass1 -Details ($output1 -join "`n")

# Cleanup freshDb
& dropdb -h $HostName -p $Port -U $User --if-exists $freshDb 2>$null

# 2. Restore into existing disposable database WITH -Force
$existingDb = "transactx_disposable_exist_$(Get-Random)"
& createdb -h $HostName -p $Port -U $User $existingDb 2>$null
$env:PGPASSWORD = $Password
$output2 = & powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$scriptsDir\restore-db.ps1" -BackupFile $validBackupFile -TargetDb $existingDb -Force 2>&1
$pass2 = ($LASTEXITCODE -eq 0) -and (($output2 | Out-String) -match "exists and -Force specified") -and (($output2 | Out-String) -match "completed successfully")
Record-TestResult -TestID "TEST-RESTORE-02" -Description "Restore into an existing disposable database with explicit -Force" -Passed $pass2 -Details ($output2 -join "`n")

# 2b. Attempt restore into existing database WITHOUT -Force (must fail)
$output2b = & powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$scriptsDir\restore-db.ps1" -BackupFile $validBackupFile -TargetDb $existingDb 2>&1
$pass2b = ($LASTEXITCODE -ne 0) -and (($output2b | Out-String) -match "already exists. Re-run with -Force")
Record-TestResult -TestID "TEST-RESTORE-02B" -Description "Restore into an existing disposable database without -Force is rejected" -Passed $pass2b -Details ($output2b -join "`n")

# Cleanup existingDb
& dropdb -h $HostName -p $Port -U $User --if-exists $existingDb 2>$null

# 3. Source database selected as restore target (must fail)
$env:TX_SOURCE_DB = "transactx_local_staging"
$output3 = & powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$scriptsDir\restore-db.ps1" -BackupFile $validBackupFile -TargetDb "transactx_local_staging" -Force 2>&1
$pass3 = ($LASTEXITCODE -ne 0) -and (($output3 | Out-String) -match "Safety violation: Target database cannot equal source database")
Record-TestResult -TestID "TEST-RESTORE-03" -Description "Source database selected as restore target is rejected" -Passed $pass3 -Details ($output3 -join "`n")

# 4. System database selected as target (must fail)
$output4 = & powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$scriptsDir\restore-db.ps1" -BackupFile $validBackupFile -TargetDb "postgres" -Force 2>&1
$pass4 = ($LASTEXITCODE -ne 0) -and (($output4 | Out-String) -match "Safety violation: Database 'postgres' is a protected system database")
Record-TestResult -TestID "TEST-RESTORE-04" -Description "System database (postgres) selected as restore target is rejected" -Passed $pass4 -Details ($output4 -join "`n")

# 5. Invalid identifier (must fail)
$output5 = & powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$scriptsDir\restore-db.ps1" -BackupFile $validBackupFile -TargetDb "123-invalid-name!" 2>&1
$pass5 = ($LASTEXITCODE -ne 0) -and (($output5 | Out-String) -match "Invalid target database identifier")
Record-TestResult -TestID "TEST-RESTORE-05" -Description "Invalid identifier format is rejected" -Passed $pass5 -Details ($output5 -join "`n")

# 6. SQL-injection-style database identifier (must fail)
$output6 = & powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$scriptsDir\restore-db.ps1" -BackupFile $validBackupFile -TargetDb "target_db; DROP DATABASE postgres; --" 2>&1
$pass6 = ($LASTEXITCODE -ne 0) -and (($output6 | Out-String) -match "Invalid target database identifier")
Record-TestResult -TestID "TEST-RESTORE-06" -Description "SQL-injection-style database identifier is rejected before invocation" -Passed $pass6 -Details ($output6 -join "`n")

# 7. Missing password configuration (must fail)
$cmd7 = "`$env:PGPASSWORD = ''; & '$scriptsDir\restore-db.ps1' -BackupFile '$validBackupFile' -TargetDb 'transactx_disposable_nopass' -Password ''"
$output7 = & powershell.exe -NoProfile -ExecutionPolicy Bypass -Command $cmd7 2>&1
$pass7 = ($LASTEXITCODE -ne 0) -and (($output7 | Out-String) -match "PGPASSWORD is not configured")
Record-TestResult -TestID "TEST-RESTORE-07" -Description "Missing password configuration is rejected" -Passed $pass7 -Details ($output7 -join "`n")

# 8. Invalid/corrupted backup file (0 bytes or missing)
$emptyBackupFile = Join-Path $testArtifactsDir "empty_backup.sql"
New-Item -ItemType File -Path $emptyBackupFile -Force | Out-Null
$output8 = & powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$scriptsDir\restore-db.ps1" -BackupFile $emptyBackupFile -TargetDb "transactx_disposable_empty" 2>&1
$pass8 = ($LASTEXITCODE -ne 0) -and (($output8 | Out-String) -match "empty \(0 bytes\)")
Record-TestResult -TestID "TEST-RESTORE-08" -Description "Empty (0 bytes) backup file is rejected" -Passed $pass8 -Details ($output8 -join "`n")

# 9. Simulated SQL restore error (with ON_ERROR_STOP=1 must fail)
$corruptSqlFile = Join-Path $testArtifactsDir "syntax_error_backup.sql"
"CREATE TABLE test_table (id INT); INSERT INTO non_existent_table VALUES (1); CREATE TABLE should_not_run (id INT);" | Set-Content $corruptSqlFile
$errDb = "transactx_disposable_sqlerr_$(Get-Random)"
$env:PGPASSWORD = $Password
$output9 = & powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$scriptsDir\restore-db.ps1" -BackupFile $corruptSqlFile -TargetDb $errDb 2>&1
$exitCode9 = $LASTEXITCODE
$pass9 = ($exitCode9 -ne 0) -and (($output9 | Out-String) -match "psql restore failed with exit code")
Record-TestResult -TestID "TEST-RESTORE-09" -Description "Simulated SQL restore error triggers ON_ERROR_STOP=1 and halts" -Passed $pass9 -Details ($output9 -join "`n")

# 10. Ensure failed restore returns non-zero exit code and never reports success
$pass10 = ($exitCode9 -ne 0) -and (($output9 | Out-String) -notmatch "completed successfully")
Record-TestResult -TestID "TEST-RESTORE-10" -Description "Failed restore returns non-zero exit code and never reports success" -Passed $pass10 -Details "ExitCode=$exitCode9, Success reported=$((($output9 | Out-String) -match 'completed successfully'))"

# Cleanup errDb
& dropdb -h $HostName -p $Port -U $User --if-exists $errDb 2>$null

# Cleanup temporary files
Remove-Item -Path $testArtifactsDir -Recurse -Force -ErrorAction SilentlyContinue

Write-Host "`nSummary of Restore Safety Tests:"
$results | Format-Table -AutoSize
$allPassed = ($results | Where-Object { $_.Result -ne "PASS" }).Count -eq 0
if (-not $allPassed) {
    Write-Error "Some restore safety tests failed"
    exit 1
}
Write-Host "All 10 Restore Safety Tests PASSED!"
