$ErrorActionPreference = "Stop"
$root = (Get-Item $PSScriptRoot\..).FullName
$env:BANK_B_ADDR = if ($env:BANK_B_ADDR) { $env:BANK_B_ADDR } else { "127.0.0.1:8082" }
if (-not $env:BANK_B_DATABASE_URL) {
    $env:BANK_B_DATABASE_URL = if ($env:DATABASE_URL) { $env:DATABASE_URL } else { "postgres://postgres:1234@127.0.0.1:5432/transactx_local_staging?sslmode=disable" }
}
$env:BANK_B_SERVICE_TOKEN = "local-staging-bank-b-token-32b"
$env:TX_DB_MAX_CONNS = "4"
Set-Location $root
& "$root\bin\bank-b.exe"
