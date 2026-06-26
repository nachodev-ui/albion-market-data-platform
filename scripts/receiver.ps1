Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot

New-Item -ItemType Directory -Force -Path (Join-Path $root "data\raw") | Out-Null
New-Item -ItemType Directory -Force -Path (Join-Path $root "data\normalized") | Out-Null
New-Item -ItemType Directory -Force -Path (Join-Path $root "data\database") | Out-Null

Push-Location $root
try {
    go run ./apps/collector/cmd/receiver @args
}
finally {
    Pop-Location
}
