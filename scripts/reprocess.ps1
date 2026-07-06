param(
    [switch]$IncludeTest,
    [switch]$Rebuild
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
$nativeReprocess = Join-Path $root "tools\albion-market-reprocess.exe"
$nativeRebuild = Join-Path $root "tools\albion-market-rebuilddb.exe"
$sourceReprocess = Join-Path $root "apps\collector\cmd\reprocess"
$sourceRebuild = Join-Path $root "apps\collector\cmd\rebuilddb"

function Reset-NormalizedDirectory {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path
    )

    if (Test-Path $Path) {
        $stamp = Get-Date -Format "yyyyMMdd-HHmmss"
        $backup = "$Path-backup-$stamp"
        Move-Item $Path $backup
        Write-Host "Copia de seguridad creada en $backup"
    }

    New-Item -ItemType Directory -Force -Path $Path | Out-Null
}

function Invoke-ReprocessTool {
    param(
        [string]$InputDirectory,
        [string]$OutputDirectory,
        [string]$CatalogDirectory
    )
    $argsList = @("-input-dir", $InputDirectory, "-output-dir", $OutputDirectory, "-catalog-dir", $CatalogDirectory)
    if (Test-Path $nativeReprocess) {
        & $nativeReprocess @argsList
    } elseif (Test-Path $sourceReprocess) {
        & go @("run", "./apps/collector/cmd/reprocess") @argsList
    } else {
        throw "No se encontró albion-market-reprocess.exe ni el código fuente de reprocess."
    }
    if ($LASTEXITCODE -ne 0) { throw "reprocess terminó con código $LASTEXITCODE." }
}

function Invoke-RebuildDbTool {
    param(
        [string]$NormalizedDirectory,
        [string]$DatabasePath,
        [switch]$Reset
    )
    $argsList = @("-normalized-dir", $NormalizedDirectory, "-database", $DatabasePath)
    if ($Reset) { $argsList += "-reset" }
    if (Test-Path $nativeRebuild) {
        & $nativeRebuild @argsList
    } elseif (Test-Path $sourceRebuild) {
        & go @("run", "./apps/collector/cmd/rebuilddb") @argsList
    } else {
        throw "No se encontró albion-market-rebuilddb.exe ni el código fuente de rebuilddb."
    }
    if ($LASTEXITCODE -ne 0) { throw "rebuilddb terminó con código $LASTEXITCODE." }
}

Push-Location $root
try {
    if ($Rebuild) {
        Reset-NormalizedDirectory -Path ./data/normalized
        Remove-Item ./data/database/market-state.json -Force -ErrorAction SilentlyContinue
    }

    Invoke-ReprocessTool -InputDirectory ./data/raw -OutputDirectory ./data/normalized -CatalogDirectory ./catalog
    Invoke-RebuildDbTool -NormalizedDirectory ./data/normalized -DatabasePath ./data/database/market-state.json -Reset:$Rebuild

    if ($IncludeTest) {
        if ($Rebuild) {
            Reset-NormalizedDirectory -Path ./data/test/normalized
            Remove-Item ./data/test/database/market-state.json -Force -ErrorAction SilentlyContinue
        }

        Invoke-ReprocessTool -InputDirectory ./data/test/raw -OutputDirectory ./data/test/normalized -CatalogDirectory ./catalog
        Invoke-RebuildDbTool -NormalizedDirectory ./data/test/normalized -DatabasePath ./data/test/database/market-state.json -Reset:$Rebuild
    }
}
finally {
    Pop-Location
}
