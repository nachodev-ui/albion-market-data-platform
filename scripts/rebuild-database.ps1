param(
    [switch]$IncludeTest
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
$nativeTool = Join-Path $root "tools\albion-market-rebuilddb.exe"
$sourceTool = Join-Path $root "apps\collector\cmd\rebuilddb"

function Invoke-RebuildDb {
    param(
        [string]$NormalizedDirectory,
        [string]$DatabasePath,
        [switch]$Reset
    )

    $argsList = @(
        "-normalized-dir", $NormalizedDirectory,
        "-database", $DatabasePath
    )
    if ($Reset) { $argsList += "-reset" }

    if (Test-Path $nativeTool) {
        & $nativeTool @argsList
    } elseif (Test-Path $sourceTool) {
        & go @("run", "./apps/collector/cmd/rebuilddb") @argsList
    } else {
        throw "No se encontró albion-market-rebuilddb.exe ni el código fuente de rebuilddb."
    }
    if ($LASTEXITCODE -ne 0) {
        throw "rebuilddb terminó con código $LASTEXITCODE."
    }
}

Push-Location $root
try {
    New-Item -ItemType Directory -Force -Path ./data/database | Out-Null
    Invoke-RebuildDb -NormalizedDirectory ./data/normalized -DatabasePath ./data/database/market-state.json -Reset

    if ($IncludeTest) {
        New-Item -ItemType Directory -Force -Path ./data/test/database | Out-Null
        Invoke-RebuildDb -NormalizedDirectory ./data/test/normalized -DatabasePath ./data/test/database/market-state.json -Reset
    }
}
finally {
    Pop-Location
}
