$ErrorActionPreference = "Stop"
$root = (Get-Item $PSScriptRoot\..).FullName
Set-Location $root
& "$root\bin\caddy.exe" run --config "$root\deploy\caddy\Caddyfile.local-staging"
