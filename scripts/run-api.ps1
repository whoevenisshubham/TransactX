$ErrorActionPreference = "Stop"
$root = (Get-Item $PSScriptRoot\..).FullName

# Load and validate local staging environment
& "$PSScriptRoot\load-local-staging-env.ps1"

$env:DEFAULT_BANK_CODE = if ($env:DEFAULT_BANK_CODE) { $env:DEFAULT_BANK_CODE } else { "BANK-A" }
$env:BANK_A_CODE = if ($env:BANK_A_CODE) { $env:BANK_A_CODE } else { "BANK-A" }
$env:BANK_B_CODE = if ($env:BANK_B_CODE) { $env:BANK_B_CODE } else { "BANK-B" }
$env:APP_DEVELOPMENT = "true"
$env:TX_SIMULATION_MODE = "true"
$env:TX_ALLOW_REGISTRATION = "false"
$env:TX_PUBLIC_DEMO_AUTH = "true"
$env:TX_CHAOS_ENABLED = "true"
$env:TX_RECOVERY_CONTROL_ENABLED = "true"
$env:TX_DB_MAX_CONNS = if ($env:TX_DB_MAX_CONNS) { $env:TX_DB_MAX_CONNS } else { "8" }
$env:TX_PAYMENT_CONCURRENCY = if ($env:TX_PAYMENT_CONCURRENCY) { $env:TX_PAYMENT_CONCURRENCY } else { "2" }

Set-Location $root
& "$root\bin\api.exe"
