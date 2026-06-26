Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSCommandPath
$exe = Join-Path $root "bin\albion-market-receiver.exe"

if (-not (Test-Path -LiteralPath $exe)) {
    throw "No se encontró el ejecutable del receiver en $exe"
}

New-Item -ItemType Directory -Force -Path (Join-Path $root "data\raw") | Out-Null
New-Item -ItemType Directory -Force -Path (Join-Path $root "data\normalized") | Out-Null
New-Item -ItemType Directory -Force -Path (Join-Path $root "data\database") | Out-Null

Push-Location $root
try {
    & $exe @args
}
finally {
    Pop-Location
}
