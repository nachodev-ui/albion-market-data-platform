Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
$nativeReceiver = Join-Path $root "albion-market-receiver.exe"
$sourceReceiver = Join-Path $root "apps\collector\cmd\receiver"

New-Item -ItemType Directory -Force -Path (Join-Path $root "data\raw") | Out-Null
New-Item -ItemType Directory -Force -Path (Join-Path $root "data\normalized") | Out-Null
New-Item -ItemType Directory -Force -Path (Join-Path $root "data\database") | Out-Null

Push-Location $root
try {
    if (Test-Path $nativeReceiver) {
        & $nativeReceiver @args
    } elseif (Test-Path $sourceReceiver) {
        go run ./apps/collector/cmd/receiver @args
    } else {
        throw "No se encontró albion-market-receiver.exe ni el código fuente del receiver."
    }
}
finally {
    Pop-Location
}
