$ErrorActionPreference = "Stop"
$root = (Get-Item $PSScriptRoot\..).FullName

# Load and validate local staging environment
& "$PSScriptRoot\load-local-staging-env.ps1"

$env:TX_DB_MAX_CONNS = if ($env:TX_DB_MAX_CONNS) { $env:TX_DB_MAX_CONNS } else { "4" }

Set-Location $root
& "$root\bin\bank-b.exe"
