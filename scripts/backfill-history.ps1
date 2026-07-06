[CmdletBinding()]
param(
    [string]$From,
    [string]$To,
    [switch]$All,
    [switch]$DryRun,
    [string]$Server = "",
    [int]$BatchSize = 100,
    [int]$MaxBuckets = 100000,
    [double]$RequestsPerSecond = 2,
    [string]$InputDirectory = ".\data\normalized"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

function Import-DotEnv([string]$Path) {
    if (-not (Test-Path $Path)) { return }
    Get-Content $Path | ForEach-Object {
        $line = $_.Trim()
        if (-not $line -or $line.StartsWith("#")) { return }
        if ($line.StartsWith("export ")) { $line = $line.Substring(7).Trim() }
        $parts = $line.Split("=", 2)
        if ($parts.Count -ne 2) { return }
        $name = $parts[0].Trim()
        $value = $parts[1].Trim().Trim('"').Trim("'")
        if ($name -and -not [Environment]::GetEnvironmentVariable($name, "Process")) {
            [Environment]::SetEnvironmentVariable($name, $value, "Process")
        }
    }
}

$root = Split-Path -Parent $PSScriptRoot
$nativeTool = Join-Path $root "tools\albion-market-backfill-history.exe"
$sourceTool = Join-Path $root "apps\collector\cmd\backfillhistory"

Push-Location $root
try {
    Import-DotEnv ".env"

    $today = (Get-Date).ToUniversalTime().Date
    if ($All) {
        $From = "1970-01-01"
    } elseif (-not $From) {
        $From = $today.AddDays(-27).ToString("yyyy-MM-dd")
    }
    if (-not $To) {
        $To = $today.ToString("yyyy-MM-dd")
    }

    $argsList = @(
        "--input-dir", $InputDirectory,
        "--from", $From,
        "--to", $To,
        "--batch-size", $BatchSize,
        "--max-buckets", $MaxBuckets,
        "--requests-per-second", $RequestsPerSecond
    )
    if ($Server) { $argsList += @("--server", $Server) }
    if ($DryRun) { $argsList += "--dry-run" }

    Write-Host "== Backfill histórico ==" -ForegroundColor Cyan
    Write-Host "Rango: $From a $To"
    Write-Host "Directorio: $InputDirectory"
    Write-Host "Modo: $(if ($DryRun) { 'dry-run' } else { 'envío real' })"

    if (Test-Path $nativeTool) {
        & $nativeTool @argsList
    } elseif (Test-Path $sourceTool) {
        $goArgs = @("run", "./apps/collector/cmd/backfillhistory") + $argsList
        & go @goArgs
    } else {
        throw "No se encontró albion-market-backfill-history.exe ni el código fuente de backfillhistory."
    }
    if ($LASTEXITCODE -ne 0) {
        throw "El comando de backfill terminó con código $LASTEXITCODE."
    }
}
finally {
    Pop-Location
}
