$ErrorActionPreference = "Stop"
$root = (Get-Item $PSScriptRoot\..).FullName

# Load and validate local staging environment
& "$PSScriptRoot\load-local-staging-env.ps1"

$env:BANK_A_CODE = if ($env:BANK_A_CODE) { $env:BANK_A_CODE } else { "BANK-A" }
$env:BANK_B_CODE = if ($env:BANK_B_CODE) { $env:BANK_B_CODE } else { "BANK-B" }
$env:TX_RECOVERY_ENABLED = "true"
$env:TX_RECOVERY_CONCURRENCY = if ($env:TX_RECOVERY_CONCURRENCY) { $env:TX_RECOVERY_CONCURRENCY } else { "2" }
$env:TX_RECOVERY_SCAN_INTERVAL = if ($env:TX_RECOVERY_SCAN_INTERVAL) { $env:TX_RECOVERY_SCAN_INTERVAL } else { "5s" }
$env:TX_RECOVERY_STALE_AFTER = if ($env:TX_RECOVERY_STALE_AFTER) { $env:TX_RECOVERY_STALE_AFTER } else { "15s" }
$env:TX_DB_MAX_CONNS = if ($env:TX_DB_MAX_CONNS) { $env:TX_DB_MAX_CONNS } else { "4" }

Set-Location $root
& "$root\bin\worker.exe"
