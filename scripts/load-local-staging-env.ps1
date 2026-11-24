# TransactX Local Staging Environment Loader
# Safely parses deploy/env/local-staging.env without arbitrary code execution
param(
    [string]$EnvFile,
    [switch]$SkipValidation = $false
)

$ErrorActionPreference = "Stop"
$root = (Get-Item $PSScriptRoot\..).FullName

if (-not $EnvFile) {
    $EnvFile = Join-Path $root "deploy\env\local-staging.env"
}

# 1. Parse the environment file if it exists
if (Test-Path $EnvFile) {
    $lines = Get-Content -Path $EnvFile -Encoding UTF8
    $lineNum = 0
    foreach ($rawLine in $lines) {
        $lineNum++
        $line = $rawLine.Trim()
        # Ignore comments and empty lines
        if ($line.Length -eq 0 -or $line.StartsWith("#")) {
            continue
        }

        # Must contain an '=' separator
        $eqIndex = $line.IndexOf('=')
        if ($eqIndex -le 0) {
            [Console]::Error.WriteLine("Malformed environment line $lineNum in $($EnvFile): missing '=' or empty key.")
            throw "Malformed environment line $lineNum in $($EnvFile): missing '=' or empty key."
        }

        $key = $line.Substring(0, $eqIndex).Trim()
        $val = $line.Substring($eqIndex + 1).Trim()

        # Validate key format
        if ($key -notmatch '^[A-Za-z_][A-Za-z0-9_]*$') {
            [Console]::Error.WriteLine("Invalid environment variable name '$key' at line $lineNum in $EnvFile.")
            throw "Invalid environment variable name '$key' at line $lineNum in $EnvFile."
        }

        # Strip matching surrounding quotes if present
        if (($val.StartsWith('"') -and $val.EndsWith('"') -and $val.Length -ge 2) -or
            ($val.StartsWith("'") -and $val.EndsWith("'") -and $val.Length -ge 2)) {
            $val = $val.Substring(1, $val.Length - 2)
        }

        # Precedence: Do NOT overwrite already-set process environment variables
        $existingVal = [System.Environment]::GetEnvironmentVariable($key, "Process")
        if ([string]::IsNullOrWhiteSpace($existingVal)) {
            [System.Environment]::SetEnvironmentVariable($key, $val, "Process")
        }
    }
}

# 2. Derive defaults from primary settings if secondary variables are absent
if ([string]::IsNullOrWhiteSpace([System.Environment]::GetEnvironmentVariable("BANK_A_DATABASE_URL", "Process"))) {
    $db = [System.Environment]::GetEnvironmentVariable("DATABASE_URL", "Process")
    if ($db) { [System.Environment]::SetEnvironmentVariable("BANK_A_DATABASE_URL", $db, "Process") }
}
if ([string]::IsNullOrWhiteSpace([System.Environment]::GetEnvironmentVariable("BANK_B_DATABASE_URL", "Process"))) {
    $db = [System.Environment]::GetEnvironmentVariable("DATABASE_URL", "Process")
    if ($db) { [System.Environment]::SetEnvironmentVariable("BANK_B_DATABASE_URL", $db, "Process") }
}
if ([string]::IsNullOrWhiteSpace([System.Environment]::GetEnvironmentVariable("JWT_ISSUER", "Process"))) {
    [System.Environment]::SetEnvironmentVariable("JWT_ISSUER", "transactx-api", "Process")
}
if ([string]::IsNullOrWhiteSpace([System.Environment]::GetEnvironmentVariable("JWT_LIFETIME", "Process"))) {
    [System.Environment]::SetEnvironmentVariable("JWT_LIFETIME", "15m", "Process")
}
if ([string]::IsNullOrWhiteSpace([System.Environment]::GetEnvironmentVariable("BANK_A_ADDR", "Process"))) {
    [System.Environment]::SetEnvironmentVariable("BANK_A_ADDR", "127.0.0.1:8081", "Process")
}
if ([string]::IsNullOrWhiteSpace([System.Environment]::GetEnvironmentVariable("BANK_B_ADDR", "Process"))) {
    [System.Environment]::SetEnvironmentVariable("BANK_B_ADDR", "127.0.0.1:8082", "Process")
}
if ([string]::IsNullOrWhiteSpace([System.Environment]::GetEnvironmentVariable("APP_ADDR", "Process"))) {
    [System.Environment]::SetEnvironmentVariable("APP_ADDR", "127.0.0.1:8080", "Process")
}
if ([string]::IsNullOrWhiteSpace([System.Environment]::GetEnvironmentVariable("BANK_A_URL", "Process"))) {
    [System.Environment]::SetEnvironmentVariable("BANK_A_URL", "http://127.0.0.1:8081", "Process")
}
if ([string]::IsNullOrWhiteSpace([System.Environment]::GetEnvironmentVariable("BANK_B_URL", "Process"))) {
    [System.Environment]::SetEnvironmentVariable("BANK_B_URL", "http://127.0.0.1:8082", "Process")
}
if ([string]::IsNullOrWhiteSpace([System.Environment]::GetEnvironmentVariable("CORS_ALLOWED_ORIGINS", "Process"))) {
    [System.Environment]::SetEnvironmentVariable("CORS_ALLOWED_ORIGINS", "http://127.0.0.1:3000,http://localhost:3000", "Process")
}

# 3. Validation: Verify all required configuration variables are present and non-empty
if (-not $SkipValidation) {
    $requiredVars = @(
        "DATABASE_URL",
        "BANK_A_DATABASE_URL",
        "BANK_B_DATABASE_URL",
        "JWT_SECRET",
        "JWT_ISSUER",
        "JWT_LIFETIME",
        "BANK_A_SERVICE_TOKEN",
        "BANK_B_SERVICE_TOKEN",
        "BANK_A_URL",
        "BANK_B_URL",
        "BANK_A_ADDR",
        "BANK_B_ADDR",
        "APP_ADDR",
        "CORS_ALLOWED_ORIGINS"
    )

    $missing = @()
    foreach ($varName in $requiredVars) {
        $val = [System.Environment]::GetEnvironmentVariable($varName, "Process")
        if ([string]::IsNullOrWhiteSpace($val)) {
            $missing += $varName
        }
    }

    if ($missing.Count -gt 0) {
        [Console]::Error.WriteLine("Environment validation failed. Missing required environment variables: $($missing -join ', ').")
        [Console]::Error.WriteLine("Configure deploy/env/local-staging.env (from deploy/env/local-staging.env.example) or supply them in your process environment.")
        throw "Environment validation failed: Missing required environment variables: $($missing -join ', ')."
    }

    # Validate secret strength invariants
    $jwtSecret = [System.Environment]::GetEnvironmentVariable("JWT_SECRET", "Process")
    if ($jwtSecret.Length -lt 32) {
        [Console]::Error.WriteLine("Environment validation failed: JWT_SECRET must be at least 32 characters long for security.")
        throw "Environment validation failed: JWT_SECRET must be at least 32 characters long for security."
    }
}
