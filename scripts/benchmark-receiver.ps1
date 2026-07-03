[CmdletBinding()]
param(
    [ValidateRange(1, 20)]
    [int]$Count = 3,
    [string]$OutputDirectory = ".\artifacts\receiver-benchmarks"
)

$ErrorActionPreference = "Stop"
$repositoryRoot = Split-Path -Parent $PSScriptRoot
Push-Location $repositoryRoot
try {
    New-Item -ItemType Directory -Force -Path $OutputDirectory | Out-Null
    $timestamp = Get-Date -Format "yyyyMMdd-HHmmss"
    $outputPath = Join-Path $OutputDirectory "receiver-benchmarks-$timestamp.txt"
    $packages = @(
        ".\apps\collector\internal\normalization",
        ".\apps\collector\internal\storage\normalizedjsonl",
        ".\apps\collector\internal\storage\localdb",
        ".\apps\collector\internal\upstream"
    )
    $benchmarkPattern = 'Benchmark(NormalizeOrders|AppendOrders|OutboxEnqueuePrices)(1000|10000)$'

    & go test `
        -run '^$' `
        -bench $benchmarkPattern `
        -benchmem `
        -count $Count `
        @packages 2>&1 | Tee-Object -FilePath $outputPath

    if ($LASTEXITCODE -ne 0) {
        throw "receiver benchmarks failed with exit code $LASTEXITCODE"
    }
    Write-Host "Benchmark=$outputPath"
}
finally {
    Pop-Location
}
