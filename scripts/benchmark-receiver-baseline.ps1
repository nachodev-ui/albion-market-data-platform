[CmdletBinding()]
param(
    [ValidateRange(3, 100)]
    [int]$Samples = 25,
    [string]$OutputDirectory = "artifacts/receiver-performance-baseline",
    [switch]$ValidateBudgets
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
$repositoryRoot = Split-Path -Parent $PSScriptRoot
Push-Location $repositoryRoot
try {
    $stamp = Get-Date -Format "yyyyMMdd-HHmmss"
    $outputRoot = Join-Path $OutputDirectory $stamp
    $profiles = Join-Path $outputRoot "profiles"
    $report = Join-Path $outputRoot "baseline.json"
    $budgetPath = "./performance/receiver-budgets.json"
    New-Item -ItemType Directory -Force -Path $profiles | Out-Null

    $arguments = @(
        "run", "./apps/collector/cmd/perfbaseline",
        "-samples", $Samples,
        "-output", $report,
        "-profiles-dir", $profiles
    )
    if (Test-Path $budgetPath) {
        $arguments += @("-budgets", $budgetPath)
    }
    elseif ($ValidateBudgets) {
        throw "performance budget file not found: $budgetPath"
    }

    & go @arguments
    if ($LASTEXITCODE -ne 0) {
        throw "receiver performance baseline failed with exit code $LASTEXITCODE"
    }

    $result = Get-Content $report -Raw | ConvertFrom-Json
    $result.scenarios |
        Select-Object name, samples, p50_ms, p95_ms, alloc_bytes_per_op, allocs_per_op |
        Format-Table -AutoSize

    Write-Host "Baseline=$report"
    Write-Host "Profiles=$profiles"
    if (Test-Path $budgetPath) {
        Write-Host "Budgets=$budgetPath status=validated"
    }
}
finally {
    Pop-Location
}
