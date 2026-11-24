# TransactX Local Staging Environment Configuration Acceptance Test
# Validates Section 3 of Milestone M20-R.5.3
param()

$ErrorActionPreference = "Continue"
$root = (Get-Item $PSScriptRoot\..).FullName
$scriptsDir = Join-Path $root "scripts"
$loaderScript = Join-Path $scriptsDir "load-local-staging-env.ps1"
$tempDir = Join-Path $root "logs\test-env"
New-Item -ItemType Directory -Force -Path $tempDir | Out-Null

$results = @()
function Record-Test {
    param([string]$ID, [string]$Desc, [bool]$Passed, [string]$Details)
    $obj = [PSCustomObject]@{
        TestID      = $ID
        Description = $Desc
        Result      = if ($Passed) { "PASS" } else { "FAIL" }
        Details     = $Details
    }
    $script:results += $obj
    Write-Host ("[{0}] {1}: {2}" -f $obj.Result, $ID, $Desc)
    if (-not $Passed) {
        Write-Host "   Details: $Details" -ForegroundColor Red
    }
}

try {
    # Test 1: Missing JWT secret fails startup
    $dummyEnvFile = Join-Path $tempDir "missing_jwt.env"
    @"
DATABASE_URL=postgres://test:test@127.0.0.1:5432/test
BANK_A_SERVICE_TOKEN=token-a-32-bytes-long-secret-val
BANK_B_SERVICE_TOKEN=token-b-32-bytes-long-secret-val
"@ | Set-Content $dummyEnvFile -Encoding UTF8

    $cmd1 = "`$env:JWT_SECRET = ''; & '$loaderScript' -EnvFile '$dummyEnvFile'"
    $out1 = & powershell.exe -NoProfile -ExecutionPolicy Bypass -Command $cmd1 2>&1
    $pass1 = ($LASTEXITCODE -ne 0) -and (($out1 | Out-String) -match "JWT_SECRET")
    Record-Test -ID "TEST-ENV-01" -Desc "Missing JWT_SECRET fails validation" -Passed $pass1 -Details ($out1 -join " ")

    # Test 2: Short JWT secret (< 32 chars) fails startup
    $shortJwtFile = Join-Path $tempDir "short_jwt.env"
    @"
DATABASE_URL=postgres://test:test@127.0.0.1:5432/test
JWT_SECRET=tooshortsecret
BANK_A_SERVICE_TOKEN=token-a-32-bytes-long-secret-val
BANK_B_SERVICE_TOKEN=token-b-32-bytes-long-secret-val
"@ | Set-Content $shortJwtFile -Encoding UTF8

    $cmd2 = "& '$loaderScript' -EnvFile '$shortJwtFile'"
    $out2 = & powershell.exe -NoProfile -ExecutionPolicy Bypass -Command $cmd2 2>&1
    $pass2 = ($LASTEXITCODE -ne 0) -and (($out2 | Out-String) -match "at least 32 characters")
    Record-Test -ID "TEST-ENV-02" -Desc "Short JWT_SECRET (<32 chars) fails validation" -Passed $pass2 -Details ($out2 -join " ")

    # Test 3: Missing Bank service token fails startup
    $missingTokenFile = Join-Path $tempDir "missing_token.env"
    @"
DATABASE_URL=postgres://test:test@127.0.0.1:5432/test
JWT_SECRET=supersecretjwtkeythatisatleast32byteslong!
BANK_B_SERVICE_TOKEN=token-b-32-bytes-long-secret-val
"@ | Set-Content $missingTokenFile -Encoding UTF8

    $cmd3 = "`$env:BANK_A_SERVICE_TOKEN = ''; & '$loaderScript' -EnvFile '$missingTokenFile'"
    $out3 = & powershell.exe -NoProfile -ExecutionPolicy Bypass -Command $cmd3 2>&1
    $pass3 = ($LASTEXITCODE -ne 0) -and (($out3 | Out-String) -match "BANK_A_SERVICE_TOKEN")
    Record-Test -ID "TEST-ENV-03" -Desc "Missing BANK_A_SERVICE_TOKEN fails validation" -Passed $pass3 -Details ($out3 -join " ")

    # Test 4: Missing Database URL fails startup
    $missingDbFile = Join-Path $tempDir "missing_db.env"
    @"
JWT_SECRET=supersecretjwtkeythatisatleast32byteslong!
BANK_A_SERVICE_TOKEN=token-a-32-bytes-long-secret-val
BANK_B_SERVICE_TOKEN=token-b-32-bytes-long-secret-val
"@ | Set-Content $missingDbFile -Encoding UTF8

    $cmd4 = "`$env:DATABASE_URL = ''; `$env:BANK_A_DATABASE_URL = ''; `$env:BANK_B_DATABASE_URL = ''; & '$loaderScript' -EnvFile '$missingDbFile'"
    $out4 = & powershell.exe -NoProfile -ExecutionPolicy Bypass -Command $cmd4 2>&1
    $pass4 = ($LASTEXITCODE -ne 0) -and (($out4 | Out-String) -match "DATABASE_URL")
    Record-Test -ID "TEST-ENV-04" -Desc "Missing DATABASE_URL fails validation" -Passed $pass4 -Details ($out4 -join " ")

    # Test 5: Valid file loads configuration successfully
    $validEnvFile = Join-Path $tempDir "valid.env"
    @"
DATABASE_URL=postgres://user:pass@127.0.0.1:5432/testdb
JWT_SECRET=thisisavalidsecretkeywith32characterslength!
BANK_A_SERVICE_TOKEN=custom-token-a-32-bytes-value-here
BANK_B_SERVICE_TOKEN=custom-token-b-32-bytes-value-here
APP_ADDR=127.0.0.1:8080
"@ | Set-Content $validEnvFile -Encoding UTF8

    $cmd5 = "& '$loaderScript' -EnvFile '$validEnvFile'; if (`$env:DATABASE_URL -eq 'postgres://user:pass@127.0.0.1:5432/testdb' -and `$env:JWT_ISSUER -eq 'transactx-api') { Write-Host 'LOAD_SUCCESS' }"
    $out5 = & powershell.exe -NoProfile -ExecutionPolicy Bypass -Command $cmd5 2>&1
    $pass5 = ($LASTEXITCODE -eq 0) -and (($out5 | Out-String) -match "LOAD_SUCCESS")
    Record-Test -ID "TEST-ENV-05" -Desc "Valid environment file loads values and derives defaults" -Passed $pass5 -Details ($out5 -join " ")

    # Test 6: Pre-existing process environment variable takes precedence over file value
    $precedenceFile = Join-Path $tempDir "precedence.env"
    @"
DATABASE_URL=postgres://file_user:pass@127.0.0.1:5432/filedb
JWT_SECRET=thisisavalidsecretkeywith32characterslength!
BANK_A_SERVICE_TOKEN=custom-token-a-32-bytes-value-here
BANK_B_SERVICE_TOKEN=custom-token-b-32-bytes-value-here
"@ | Set-Content $precedenceFile -Encoding UTF8

    $cmd6 = "`$env:DATABASE_URL = 'postgres://env_user:pass@127.0.0.1:5432/envdb'; & '$loaderScript' -EnvFile '$precedenceFile'; if (`$env:DATABASE_URL -eq 'postgres://env_user:pass@127.0.0.1:5432/envdb') { Write-Host 'PRECEDENCE_SUCCESS' }"
    $out6 = & powershell.exe -NoProfile -ExecutionPolicy Bypass -Command $cmd6 2>&1
    $pass6 = ($LASTEXITCODE -eq 0) -and (($out6 | Out-String) -match "PRECEDENCE_SUCCESS")
    Record-Test -ID "TEST-ENV-06" -Desc "Pre-existing environment variables take precedence over file" -Passed $pass6 -Details ($out6 -join " ")

    # Test 7: Secrets are not leaked in stdout/stderr during loader execution
    $pass7 = ($out5 -notmatch "thisisavalidsecretkeywith32characterslength") -and ($out5 -notmatch "custom-token-a")
    Record-Test -ID "TEST-ENV-07" -Desc "Secret values are not printed to output streams" -Passed $pass7 -Details "Secrets absent from output"

} finally {
    Remove-Item $tempDir -Recurse -Force -ErrorAction SilentlyContinue
}

Write-Host "`nEnvironment Configuration Test Summary:"
$results | Format-Table -AutoSize
$allPassed = ($results | Where-Object { $_.Result -ne "PASS" }).Count -eq 0
if (-not $allPassed) {
    Write-Error "Some environment configuration tests failed"
    exit 1
}
Write-Host "All Environment Configuration Tests PASSED!"
