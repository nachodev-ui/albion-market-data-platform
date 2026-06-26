param(
    [switch]$IncludeTest,
    [switch]$Rebuild
)

$ErrorActionPreference = "Stop"

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

if ($Rebuild) {
    Reset-NormalizedDirectory -Path ./data/normalized
    Remove-Item ./data/database/market-state.json -Force -ErrorAction SilentlyContinue
}

go run ./apps/collector/cmd/reprocess `
    -input-dir ./data/raw `
    -output-dir ./data/normalized `
    -catalog-dir ./catalog

$databaseArguments = @(
    "run", "./apps/collector/cmd/rebuilddb",
    "-normalized-dir", "./data/normalized",
    "-database", "./data/database/market-state.json"
)
if ($Rebuild) {
    $databaseArguments += "-reset"
}
& go @databaseArguments

if ($IncludeTest) {
    if ($Rebuild) {
        Reset-NormalizedDirectory -Path ./data/test/normalized
        Remove-Item ./data/test/database/market-state.json -Force -ErrorAction SilentlyContinue
    }

    go run ./apps/collector/cmd/reprocess `
        -input-dir ./data/test/raw `
        -output-dir ./data/test/normalized `
        -catalog-dir ./catalog

    $testDatabaseArguments = @(
        "run", "./apps/collector/cmd/rebuilddb",
        "-normalized-dir", "./data/test/normalized",
        "-database", "./data/test/database/market-state.json"
    )
    if ($Rebuild) {
        $testDatabaseArguments += "-reset"
    }
    & go @testDatabaseArguments
}
