param(
  [switch]$ValidateOnly,
  [switch]$AllowIncomplete,
  [switch]$AllowDirty,
  [string]$OutputDirectory
)
$ErrorActionPreference = 'Stop'

$repo = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..\..')).Path
if (-not $OutputDirectory) { $OutputDirectory = Join-Path $repo 'deploy\out' }
$sourceSha = (& git -C $repo rev-parse HEAD).Trim()
if ($LASTEXITCODE -ne 0 -or $sourceSha -notmatch '^[0-9a-f]{40}$') { throw 'Cannot determine source SHA.' }
$dirty = @(& git -C $repo status --porcelain).Count -gt 0
$commands = @('api', 'bank-a', 'bank-b', 'worker', 'migrate', 'maintain-merkle', 'corpus', 'rebuild-worklist')
$commandSources = @{}
foreach ($command in $commands) { $commandSources[$command] = "cmd/$command" }
$commandSources['corpus'] = 'internal/investigator/cmd/corpus'
$missing = @($commands | Where-Object { -not (Test-Path -LiteralPath (Join-Path $repo "backend/$($commandSources[$_])")) })
if ($missing.Count -gt 0 -and -not $AllowIncomplete) { throw "Required v2 commands missing: $($missing -join ', '). Use -AllowIncomplete only for an inspection archive." }
if ($dirty -and -not $AllowDirty -and -not $ValidateOnly) { throw 'Git tree is dirty; commit and rerun for a release artifact.' }
if (-not (Get-Command go -ErrorAction SilentlyContinue)) { throw 'Go is required.' }
if (-not (Get-Command npm -ErrorAction SilentlyContinue)) { throw 'npm is required.' }
if (-not (Get-Command node -ErrorAction SilentlyContinue)) { throw 'Node.js is required.' }
if (-not (Get-Command tar -ErrorAction SilentlyContinue)) { throw 'tar is required.' }
$originOptions = @()
if ($ValidateOnly) { $originOptions += '--allow-missing' }
$frontendApiOrigin = @(& node (Join-Path $PSScriptRoot 'verify-api-origin.mjs') @originOptions) -join ''
if ($LASTEXITCODE -ne 0) { throw 'Frontend API origin validation failed.' }
$frontendApiOrigin = $frontendApiOrigin.Trim()
if ($missing -notcontains 'corpus') {
  & node (Join-Path $PSScriptRoot 'stage-corpus.mjs') $repo
  if ($LASTEXITCODE -ne 0) { throw 'Approved corpus input validation failed.' }
}
if ($ValidateOnly) {
  Write-Output "Source: $sourceSha"
  Write-Output "Dirty tree: $dirty"
  Write-Output "Missing planned commands: $($missing -join ', ')"
  Write-Output "Frontend API origin: $(if ($frontendApiOrigin) { $frontendApiOrigin } else { 'unset; required for an archive' })"
  Write-Output 'Validation only: no archive was written.'
  return
}

$null = New-Item -ItemType Directory -Force -Path $OutputDirectory
$stamp = [DateTime]::UtcNow.ToString('yyyyMMddTHHmmssZ')
$name = "transactx-$($sourceSha.Substring(0,12))-$stamp"
$stage = Join-Path $OutputDirectory $name
if (Test-Path -LiteralPath $stage) { throw "Output already exists: $stage" }
$null = New-Item -ItemType Directory -Path $stage
$null = New-Item -ItemType Directory -Path (Join-Path $stage 'bin')
$null = New-Item -ItemType Directory -Path (Join-Path $stage 'migrations')
$null = New-Item -ItemType Directory -Path (Join-Path $stage 'frontend')
$null = New-Item -ItemType Directory -Path (Join-Path $stage 'deploy')
$null = New-Item -ItemType Directory -Path (Join-Path $stage 'docs')

Push-Location (Join-Path $repo 'frontend')
$previousFrontendApiOrigin = $env:VITE_API_BASE_URL
try {
  $env:VITE_API_BASE_URL = $frontendApiOrigin
  npm ci; if ($LASTEXITCODE -ne 0) { throw 'npm ci failed.' }
  npm test; if ($LASTEXITCODE -ne 0) { throw 'Frontend tests failed.' }
  npm run lint; if ($LASTEXITCODE -ne 0) { throw 'Frontend type check failed.' }
  npm run build; if ($LASTEXITCODE -ne 0) { throw 'Frontend build failed.' }
} finally { $env:VITE_API_BASE_URL = $previousFrontendApiOrigin; Pop-Location }

$oldGoos = $env:GOOS; $oldGoarch = $env:GOARCH; $oldCgo = $env:CGO_ENABLED
try {
  $env:GOOS = 'linux'; $env:CGO_ENABLED = '0'
  foreach ($arch in @('amd64', 'arm64')) {
    $env:GOARCH = $arch
    $binDir = Join-Path $stage "bin\linux-$arch"
    $null = New-Item -ItemType Directory -Path $binDir
    Push-Location (Join-Path $repo 'backend')
    try {
      foreach ($command in $commands) {
        if ($missing -contains $command) { continue }
        $output = Join-Path $binDir "transactx-$command"
        & go build -buildvcs=true -trimpath -ldflags '-s -w' -o $output "./$($commandSources[$command])"
        if ($LASTEXITCODE -ne 0) { throw "Go build failed: $command/$arch" }
      }
    } finally { Pop-Location }
  }
} finally { $env:GOOS = $oldGoos; $env:GOARCH = $oldGoarch; $env:CGO_ENABLED = $oldCgo }

Copy-Item -Path (Join-Path $repo 'backend\migrations\*.sql') -Destination (Join-Path $stage 'migrations')
Copy-Item -Path (Join-Path $repo 'frontend\dist\*') -Destination (Join-Path $stage 'frontend') -Recurse
Copy-Item -Path (Join-Path $repo 'deploy\env') -Destination (Join-Path $stage 'deploy') -Recurse
Copy-Item -Path (Join-Path $repo 'deploy\systemd') -Destination (Join-Path $stage 'deploy') -Recurse
Copy-Item -Path (Join-Path $repo 'deploy\caddy') -Destination (Join-Path $stage 'deploy') -Recurse
Copy-Item -Path (Join-Path $repo 'deploy\scripts') -Destination (Join-Path $stage 'deploy') -Recurse
Copy-Item -LiteralPath (Join-Path $repo 'deploy\README.md') -Destination (Join-Path $stage 'deploy\README.md')
Copy-Item -LiteralPath (Join-Path $repo 'deploy\RESTORE_REHEARSAL.md') -Destination (Join-Path $stage 'deploy\RESTORE_REHEARSAL.md')
Copy-Item -LiteralPath (Join-Path $repo 'README.md') -Destination (Join-Path $stage 'docs\README.md')
if ($missing -notcontains 'corpus') {
  & node (Join-Path $PSScriptRoot 'stage-corpus.mjs') $repo $stage
  if ($LASTEXITCODE -ne 0) { throw 'Approved corpus staging failed.' }
}
Copy-Item -LiteralPath (Join-Path $repo 'docs\next-gen\execution-pack\docs\next-gen\DEPLOYMENT_AND_BACKUP.md') -Destination (Join-Path $stage 'docs\DEPLOYMENT_AND_BACKUP.md')

# Windows checkouts can have CRLF in shell scripts and service files. Normalize
# the staged Linux text before hashing and archiving it.
Get-ChildItem -LiteralPath (Join-Path $stage 'deploy') -Recurse -File |
  Where-Object { $_.Extension -in @('.sh', '.service', '.timer', '.example', '.md') } |
  ForEach-Object {
    $content = [IO.File]::ReadAllText($_.FullName)
    [IO.File]::WriteAllText($_.FullName, $content.Replace("`r`n", "`n"), [Text.UTF8Encoding]::new($false))
  }

Push-Location (Join-Path $repo 'backend')
try {
  $modules = & go list -m all
  if ($LASTEXITCODE -ne 0) { throw 'Go dependency inventory failed.' }
  [IO.File]::WriteAllLines((Join-Path $stage 'go-dependencies.txt'), [string[]]$modules)
} finally { Pop-Location }
Copy-Item -LiteralPath (Join-Path $repo 'frontend\package-lock.json') -Destination (Join-Path $stage 'frontend-package-lock.json')

$metadata = [ordered]@{
  schemaVersion = 'tx.release.v1'; sourceSha = $sourceSha; createdAt = [DateTime]::UtcNow.ToString('o');
  dirtyTree = $dirty; complete = ($missing.Count -eq 0); missingCommands = $missing;
  frontendApiOrigin = $frontendApiOrigin;
  goVersion = (& go version); nodeVersion = (& node --version); targetArchitectures = @('linux-amd64', 'linux-arm64')
}
$metadata | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath (Join-Path $stage 'metadata.json') -Encoding utf8
$hashes = Get-ChildItem -LiteralPath $stage -Recurse -File | Sort-Object FullName | ForEach-Object {
  $relative = [IO.Path]::GetRelativePath($stage, $_.FullName).Replace('\', '/')
  '{0}  {1}' -f (Get-FileHash -Algorithm SHA256 -LiteralPath $_.FullName).Hash.ToLowerInvariant(), $relative
}
[IO.File]::WriteAllText((Join-Path $stage 'SHA256SUMS'), (($hashes -join "`n") + "`n"), [Text.UTF8Encoding]::new($false))
$archive = Join-Path $OutputDirectory "$name.tar.gz"
& tar -C $stage -czf $archive .
if ($LASTEXITCODE -ne 0) { throw 'Archive creation failed.' }
$archiveHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $archive).Hash.ToLowerInvariant()
[IO.File]::WriteAllText("$archive.sha256", "$archiveHash  $([IO.Path]::GetFileName($archive))`n", [Text.UTF8Encoding]::new($false))
Write-Output "Archive: $archive"
Write-Output "SHA-256: $archiveHash"
Write-Output "Complete v2 bundle: $($missing.Count -eq 0)"
