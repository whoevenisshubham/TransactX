$ErrorActionPreference = "Stop"
$root = (Get-Item $PSScriptRoot\..).FullName
$env:BANK_A_ADDR = "127.0.0.1:8081"
$env:BANK_A_DATABASE_URL = "postgres://postgres:1234@127.0.0.1:5432/transactx_local_staging?sslmode=disable"
$env:BANK_A_SERVICE_TOKEN = "local-staging-bank-a-token-32b"
$env:TX_DB_MAX_CONNS = "4"
Set-Location $root
& "$root\bin\bank-a.exe"
