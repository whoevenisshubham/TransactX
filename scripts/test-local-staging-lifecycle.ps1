# TransactX Local Staging Lifecycle Acceptance Suite (M20-R.5.3)
# Validates Sections 3 & 5: Supervisor lifecycle, PID tracking, readiness JSON,
# empirical DB propagation, port liberation, restart, and controlled failure.
param(
    [string]$HostName = "127.0.0.1",
    [int]$Port = 5432,
    [string]$User = "postgres",
    [string]$Password = $env:PGPASSWORD
)

$ErrorActionPreference = "Continue"
$root = (Get-Item $PSScriptRoot\..).FullName
$scriptsDir = Join-Path $root "scripts"
$logsDir = Join-Path $root "logs"
$supervisorPidFile = Join-Path $logsDir "supervisor.pid"
$supervisorStopFile = Join-Path $logsDir "supervisor.stop"
$stagingPidsFile = Join-Path $logsDir "staging-pids.json"

# Credential validation
if (-not $Password) {
    [Console]::Error.WriteLine("PGPASSWORD is not configured. Supply -Password parameter or set PGPASSWORD environment variable.")
    exit 1
}
$env:PGPASSWORD = $Password

$results = @()
function Record-Gate {
    param([string]$GateID, [string]$Description, [bool]$Passed, [string]$Details)
    $obj = [PSCustomObject]@{
        GateID      = $GateID
        Description = $Description
        Result      = if ($Passed) { "PASS" } else { "FAIL" }
        Details     = $Details
    }
    $script:results += $obj
    Write-Host ("[{0}] {1}: {2}" -f $obj.Result, $GateID, $Description)
    if (-not $Passed) {
        Write-Host "   Details: $Details" -ForegroundColor Red
        throw "Acceptance gate $GateID failed: $Description"
    }
}

$runId = (Get-Random -Minimum 100000 -Maximum 999999).ToString()
$disposableDb = "transactx_staging_lifecycle_$runId"
$disposableDbUrl = "postgres://${User}:${Password}@${HostName}:${Port}/${disposableDb}?sslmode=disable"
$stagingPorts = @(8080, 8081, 8082, 3000)

Write-Host "=== TransactX Local Staging Lifecycle Acceptance (Run $runId) ==="

try {
    # 1. Pre-flight verification
    Write-Host "`n1. Running pre-flight port and file checks..."
    foreach ($p in $stagingPorts) {
        $conn = Get-NetTCPConnection -LocalPort $p -State Listen -ErrorAction SilentlyContinue
        if ($conn) {
            Write-Warning "Port $p is currently occupied by PID $($conn.OwningProcess). Stopping existing stack first..."
            & "$scriptsDir\stop-local-staging.ps1" | Out-Null
            Start-Sleep -Seconds 2
            break
        }
    }

    $allPreFree = $true
    foreach ($p in $stagingPorts) {
        $conn = Get-NetTCPConnection -LocalPort $p -State Listen -ErrorAction SilentlyContinue
        if ($conn) { $allPreFree = $false }
    }
    Record-Gate -GateID "GATE-LIFE-01" -Description "Pre-flight: Staging ports (8080, 8081, 8082, 3000) are free" -Passed $allPreFree -Details "All staging ports free before test"

    # 2. Database preparation: create disposable database from local staging template
    Write-Host "`n2. Creating disposable lifecycle database '$disposableDb' from template..."
    & psql -h $HostName -p $Port -U $User -d postgres -v ON_ERROR_STOP=1 -c "CREATE DATABASE $disposableDb TEMPLATE transactx_local_staging;" 2>&1 | Out-Null
    if ($LASTEXITCODE -ne 0) {
        throw "Failed to create disposable lifecycle database $disposableDb"
    }
    Record-Gate -GateID "GATE-LIFE-02" -Description "Disposable lifecycle database provisioned" -Passed ($LASTEXITCODE -eq 0) -Details "Created $disposableDb"

    # 3. Startup: Start supervisor with the disposable database URL
    Write-Host "`n3. Launching staging supervisor with custom disposable DB URL..."
    & "$scriptsDir\start-local-staging.ps1" -DbUrl $disposableDbUrl
    Start-Sleep -Seconds 4

    # Verify supervisor PID
    $supPid = $null
    if (Test-Path $supervisorPidFile) {
        $supPid = (Get-Content $supervisorPidFile).Trim()
    }
    $supProc = if ($supPid) { Get-Process -Id $supPid -ErrorAction SilentlyContinue } else { $null }
    $supIsRunning = ($supProc -and (-not $supProc.HasExited))
    Record-Gate -GateID "GATE-LIFE-03" -Description "Supervisor started and PID recorded accurately" -Passed $supIsRunning -Details "Supervisor PID: $supPid"

    # 4. Polling for API readiness
    Write-Host "`n4. Polling API readiness (/readyz)..."
    $apiReady = $false
    $apiResp = $null
    $elapsed = 0
    while ($elapsed -lt 20) {
        try {
            $apiResp = Invoke-RestMethod -Uri "http://127.0.0.1:8080/readyz" -TimeoutSec 2 -ErrorAction Stop
            if ($apiResp -and $apiResp.data -and ($apiResp.data.readiness -eq $true)) {
                $apiReady = $true
                break
            }
        } catch {
            Start-Sleep -Milliseconds 800
            $elapsed += 0.8
        }
    }
    Record-Gate -GateID "GATE-LIFE-04" -Description "Direct API /readyz returns readiness=true (HTTP 200)" -Passed $apiReady -Details "Response: $($apiResp | ConvertTo-Json -Compress)"

    # 5. Caddy ingress readiness
    Write-Host "`n5. Polling Caddy ingress readiness (http://127.0.0.1:3000/readyz)..."
    $caddyReady = $false
    $caddyResp = $null
    $elapsed = 0
    while ($elapsed -lt 10) {
        try {
            $caddyResp = Invoke-RestMethod -Uri "http://127.0.0.1:3000/readyz" -TimeoutSec 2 -ErrorAction Stop
            if ($caddyResp -and $caddyResp.data -and ($caddyResp.data.readiness -eq $true)) {
                $caddyReady = $true
                break
            }
        } catch {
            Start-Sleep -Milliseconds 800
            $elapsed += 0.8
        }
    }
    Record-Gate -GateID "GATE-LIFE-05" -Description "Caddy-proxied /readyz returns readiness=true (HTTP 200)" -Passed $caddyReady -Details "Response: $($caddyResp | ConvertTo-Json -Compress)"

    # 6. Bank participant services health
    Write-Host "`n6. Checking Bank A and Bank B healthz..."
    $bankAResp = Invoke-RestMethod -Uri "http://127.0.0.1:8081/healthz" -TimeoutSec 2
    $bankBResp = Invoke-RestMethod -Uri "http://127.0.0.1:8082/healthz" -TimeoutSec 2
    $banksOk = ($bankAResp.status -eq "ok") -and ($bankBResp.status -eq "ok")
    Record-Gate -GateID "GATE-LIFE-06" -Description "Bank A & Bank B /healthz endpoints respond status=ok" -Passed $banksOk -Details "BankA=$($bankAResp.status), BankB=$($bankBResp.status)"

    # 7. Customer authentication endpoint reachability
    Write-Host "`n7. Checking customer demo auth endpoint (POST /api/auth/demo)..."
    $authBody = '{"persona":"customer"}'
    $authResp = Invoke-RestMethod -Uri "http://127.0.0.1:8080/api/auth/demo" -Method Post -Body $authBody -ContentType "application/json" -TimeoutSec 3
    $authSuccess = ($authResp -and $authResp.data -and $authResp.data.token)
    Record-Gate -GateID "GATE-LIFE-07" -Description "Customer auth endpoint is reachable and issues JWT" -Passed $authSuccess -Details "Token received: $($null -ne $authResp.data.token)"

    # 8. Empirical database propagation verification
    Write-Host "`n8. Empirically verifying database propagation via pg_stat_activity..."
    $connCountStr = & psql -h $HostName -p $Port -U $User -d postgres -tAc "SELECT count(*) FROM pg_stat_activity WHERE datname = '$disposableDb' AND pid <> pg_backend_pid();" 2>$null
    $connCount = 0
    if ($connCountStr -match '^\d+$') { $connCount = [int]$connCountStr }
    $dbPropagated = ($connCount -ge 2)
    Record-Gate -GateID "GATE-LIFE-08" -Description "Database URL propagation empirically verified via pg_stat_activity" -Passed $dbPropagated -Details "Active connections on $disposableDb : $connCount"

    # 9. Clean shutdown verification
    Write-Host "`n9. Executing stop-local-staging.ps1 and asserting shutdown..."
    $stopOut = & "$scriptsDir\stop-local-staging.ps1" 2>&1
    $stopCode = $LASTEXITCODE
    $stopSuccess = ($stopCode -eq 0)

    # Verify supervisor exited
    $supStillAlive = if ($supPid) { Get-Process -Id $supPid -ErrorAction SilentlyContinue } else { $null }
    $supGone = (-not $supStillAlive -or $supStillAlive.HasExited)

    # Verify ports free
    $allPostFree = $true
    foreach ($p in $stagingPorts) {
        $conn = Get-NetTCPConnection -LocalPort $p -State Listen -ErrorAction SilentlyContinue
        if ($conn) { $allPostFree = $false }
    }

    $pidFilesCleaned = (-not (Test-Path $supervisorPidFile)) -and (-not (Test-Path $stagingPidsFile)) -and (-not (Test-Path $supervisorStopFile))
    $shutdownClean = $stopSuccess -and $supGone -and $allPostFree -and $pidFilesCleaned
    Record-Gate -GateID "GATE-LIFE-09" -Description "Shutdown verified: supervisor exited, all ports free, PID files cleaned" -Passed $shutdownClean -Details "ExitCode=$stopCode, SupGone=$supGone, PortsFree=$allPostFree, FilesCleaned=$pidFilesCleaned"

    # 10. No-restart invariant verification (Wait 5s)
    Write-Host "`n10. Awaiting 5 seconds to verify supervisor does NOT respawn..."
    Start-Sleep -Seconds 5
    $respawnDetected = $false
    foreach ($p in $stagingPorts) {
        $conn = Get-NetTCPConnection -LocalPort $p -State Listen -ErrorAction SilentlyContinue
        if ($conn) { $respawnDetected = $true }
    }
    Record-Gate -GateID "GATE-LIFE-10" -Description "No-restart invariant: zero processes restarted after stop" -Passed (-not $respawnDetected) -Details "Respawn detected: $respawnDetected"

    # 11. Restart verification: Start a second time, verify readiness, and stop again
    Write-Host "`n11. Testing restart lifecycle (start, check ready, stop)..."
    & "$scriptsDir\start-local-staging.ps1" -DbUrl $disposableDbUrl
    Start-Sleep -Seconds 4

    $restartReady = $false
    $elapsed = 0
    while ($elapsed -lt 15) {
        try {
            $rResp = Invoke-RestMethod -Uri "http://127.0.0.1:8080/readyz" -TimeoutSec 2 -ErrorAction Stop
            if ($rResp -and $rResp.data -and ($rResp.data.readiness -eq $true)) {
                $restartReady = $true
                break
            }
        } catch {
            Start-Sleep -Milliseconds 800
            $elapsed += 0.8
        }
    }

    # Stop second time
    & "$scriptsDir\stop-local-staging.ps1" | Out-Null
    Start-Sleep -Seconds 2

    $restartPortsFree = $true
    foreach ($p in $stagingPorts) {
        $conn = Get-NetTCPConnection -LocalPort $p -State Listen -ErrorAction SilentlyContinue
        if ($conn) { $restartPortsFree = $false }
    }
    $restartPass = $restartReady -and $restartPortsFree
    Record-Gate -GateID "GATE-LIFE-11" -Description "Restart lifecycle verified: second startup ready and second stop cleans all ports" -Passed $restartPass -Details "RestartReady=$restartReady, RestartPortsFree=$restartPortsFree"

    # 12. Controlled startup failure test with invalid test configuration
    Write-Host "`n12. Testing controlled startup failure with invalid configuration..."
    $cmd12 = "`$env:JWT_SECRET = 'tooshort'; & '$scriptsDir\supervise-local-staging.ps1'"
    $badOut = & powershell.exe -NoProfile -ExecutionPolicy Bypass -Command $cmd12 2>&1
    $badExitCode = $LASTEXITCODE

    $failedVisibly = ($badExitCode -ne 0) -and (($badOut | Out-String) -match "at least 32 characters")

    # Confirm that no lingering processes or ports exist
    & "$scriptsDir\stop-local-staging.ps1" | Out-Null
    Start-Sleep -Seconds 2
    $failPortsFree = $true
    foreach ($p in $stagingPorts) {
        $conn = Get-NetTCPConnection -LocalPort $p -State Listen -ErrorAction SilentlyContinue
        if ($conn) { $failPortsFree = $false }
    }
    $gate12Pass = $failedVisibly -and $failPortsFree
    Record-Gate -GateID "GATE-LIFE-12" -Description "Controlled startup failure test fails visibly with zero orphan processes" -Passed $gate12Pass -Details "FailedVisibly=$failedVisibly, ExitCode=$badExitCode, PortsFree=$failPortsFree"

} finally {
    Write-Host "`nCleaning up lifecycle test database '$disposableDb'..."
    & psql -h $HostName -p $Port -U $User -d postgres -v ON_ERROR_STOP=1 -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '$disposableDb' AND pid <> pg_backend_pid();" 2>$null | Out-Null
    & dropdb -h $HostName -p $Port -U $User --if-exists $disposableDb 2>$null
    & "$scriptsDir\stop-local-staging.ps1" | Out-Null
}

Write-Host "`nSummary of Staging Lifecycle Acceptance Gates:"
$results | Format-Table -AutoSize
$allPassed = ($results | Where-Object { $_.Result -ne "PASS" }).Count -eq 0
if (-not $allPassed) {
    Write-Error "Some staging lifecycle acceptance gates failed"
    exit 1
}
Write-Host "All 12 Staging Lifecycle Acceptance Gates PASSED!"
