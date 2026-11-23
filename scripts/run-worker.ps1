$ErrorActionPreference = "Stop"
$root = (Get-Item $PSScriptRoot\..).FullName
if (-not $env:DATABASE_URL) {
    $env:DATABASE_URL = "postgres://postgres:1234@127.0.0.1:5432/transactx_local_staging?sslmode=disable"
}
$env:JWT_SECRET = "supersecretjwtkeythatisatleast32byteslongforsecurity!"
$env:BANK_A_CODE = "BANK-A"
$env:BANK_B_CODE = "BANK-B"
$env:BANK_A_URL = "http://127.0.0.1:8081"
$env:BANK_B_URL = "http://127.0.0.1:8082"
$env:BANK_A_SERVICE_TOKEN = "local-staging-bank-a-token-32b"
$env:BANK_B_SERVICE_TOKEN = "local-staging-bank-b-token-32b"
$env:TX_RECOVERY_ENABLED = "true"
$env:TX_RECOVERY_CONCURRENCY = "2"
$env:TX_RECOVERY_SCAN_INTERVAL = "5s"
$env:TX_RECOVERY_STALE_AFTER = "15s"
$env:TX_DB_MAX_CONNS = "4"
Set-Location $root
& "$root\bin\worker.exe"
