[CmdletBinding()]
param(
    [ValidateRange(5, 100)]
    [int]$Count = 20,
    [string]$OutputDirectory = ".\artifacts\receiver-benchmarks",
    [string]$UpstreamUrl = "",
    [string]$UpstreamToken = "",
    [ValidateRange(5, 100)]
    [int]$UpstreamSamples = 20,
    [switch]$ConfirmUpstreamWrite
)

$ErrorActionPreference = "Stop"
$repositoryRoot = Split-Path -Parent $PSScriptRoot

function Get-Percentile {
    param([double[]]$Values, [double]$Percentile)
    if ($Values.Count -eq 0) { return 0 }
    $sorted = @($Values | Sort-Object)
    $index = [Math]::Ceiling(($Percentile / 100) * $sorted.Count) - 1
    $index = [Math]::Max(0, [Math]::Min($index, $sorted.Count - 1))
    return [double]$sorted[$index]
}

function Measure-UpstreamLatency {
    param([string]$BaseUrl, [string]$Token, [int]$Samples)
    if (-not $ConfirmUpstreamWrite) {
        throw "-ConfirmUpstreamWrite is required because latency measurement writes synthetic price rows"
    }
    $headers = @{ "Content-Type" = "application/json" }
    if ($Token) { $headers.Authorization = "Bearer $Token" }
    $values = New-Object System.Collections.Generic.List[double]
    for ($index = 0; $index -lt $Samples; $index++) {
        $observedAt = [DateTime]::UtcNow.ToString("o")
        $body = @{
            request_id = "perf-$([guid]::NewGuid().ToString('N'))"
            server = "west"
            entries = @(@{
                observed_at = $observedAt
                location_id = 3005
                item_key = "T4_BAG"
                quality = 1
                sell_price_min = 1
                sell_price_min_at = $observedAt
            })
        } | ConvertTo-Json -Depth 8 -Compress
        $watch = [System.Diagnostics.Stopwatch]::StartNew()
        Invoke-RestMethod -Method Post -Uri ($BaseUrl.TrimEnd('/') + "/api/v1/ingest/prices") -Headers $headers -Body $body | Out-Null
        $watch.Stop()
        $values.Add($watch.Elapsed.TotalMilliseconds)
    }
    [pscustomobject]@{
        name = "upstream_postgresql_roundtrip"
        samples = $values.Count
        p50_ms = Get-Percentile $values.ToArray() 50
        p95_ms = Get-Percentile $values.ToArray() 95
        max_ms = ($values | Measure-Object -Maximum).Maximum
    }
}

Push-Location $repositoryRoot
try {
    New-Item -ItemType Directory -Force -Path $OutputDirectory | Out-Null
    $timestamp = Get-Date -Format "yyyyMMdd-HHmmss"
    $rawPath = Join-Path $OutputDirectory "receiver-benchmarks-$timestamp.txt"
    $jsonPath = Join-Path $OutputDirectory "receiver-baseline-$timestamp.json"
    $csvPath = Join-Path $OutputDirectory "receiver-baseline-$timestamp.csv"
    $packages = @(
        ".\apps\collector\internal\httpingest",
        ".\apps\collector\internal\normalization",
        ".\apps\collector\internal\storage\normalizedjsonl",
        ".\apps\collector\internal\storage\localdb",
        ".\apps\collector\internal\upstream"
    )
    $benchmarkPattern = 'Benchmark(CaptureOrders|NormalizeOrders|NormalizeHistory68Buckets|AppendOrders|AppendHistory68Buckets|ReadPrices|ReadHistory68Buckets|SerializePrices|OutboxEnqueuePrices|OutboxRestart)(1000|10000)?$'

    $benchmarkOutput = & go test -run '^$' -bench $benchmarkPattern -benchmem -count $Count @packages 2>&1
    $benchmarkOutput | Tee-Object -FilePath $rawPath
    if ($LASTEXITCODE -ne 0) {
        throw "receiver benchmarks failed with exit code $LASTEXITCODE"
    }

    $samples = foreach ($line in $benchmarkOutput) {
        if ($line -match '^(Benchmark\S+)-\d+\s+\d+\s+([0-9.]+) ns/op\s+([0-9.]+) B/op\s+([0-9.]+) allocs/op') {
            [pscustomobject]@{
                Name = $Matches[1]
                NsPerOp = [double]$Matches[2]
                BytesPerOp = [double]$Matches[3]
                AllocsPerOp = [double]$Matches[4]
            }
        }
    }

    $summary = foreach ($group in ($samples | Group-Object Name)) {
        $times = [double[]]@($group.Group.NsPerOp)
        $bytes = [double[]]@($group.Group.BytesPerOp)
        $allocs = [double[]]@($group.Group.AllocsPerOp)
        [pscustomobject]@{
            Name = $group.Name
            Samples = $times.Count
            P50Ms = [Math]::Round((Get-Percentile $times 50) / 1000000, 3)
            P95Ms = [Math]::Round((Get-Percentile $times 95) / 1000000, 3)
            MaxMs = [Math]::Round((($times | Measure-Object -Maximum).Maximum) / 1000000, 3)
            P50BytesPerOp = [Math]::Round((Get-Percentile $bytes 50), 0)
            P95BytesPerOp = [Math]::Round((Get-Percentile $bytes 95), 0)
            P50AllocsPerOp = [Math]::Round((Get-Percentile $allocs 50), 0)
            P95AllocsPerOp = [Math]::Round((Get-Percentile $allocs 95), 0)
        }
    }

    $operationalOutput = & go test -run 'TestPerformanceBaselineOutboxRecovery$' -v .\apps\collector\internal\upstream 2>&1
    $operationalOutput | Add-Content $rawPath
    if ($LASTEXITCODE -ne 0) {
        throw "operational baseline failed with exit code $LASTEXITCODE"
    }
    $operationalMetrics = foreach ($line in $operationalOutput) {
        if ($line -match '^PERF_METRIC\s+(.+)$') {
            $Matches[1] | ConvertFrom-Json
        }
    }

    $storageFiles = Get-ChildItem .\data -Recurse -File -ErrorAction SilentlyContinue | ForEach-Object {
        [pscustomobject]@{
            path = $_.FullName.Substring((Get-Location).Path.Length + 1)
            bytes = $_.Length
        }
    }

    $upstreamMetric = $null
    if ($UpstreamUrl) {
        $upstreamMetric = Measure-UpstreamLatency -BaseUrl $UpstreamUrl -Token $UpstreamToken -Samples $UpstreamSamples
    }

    $result = [ordered]@{
        generated_at = [DateTime]::UtcNow.ToString("o")
        go_version = (& go version)
        machine = [ordered]@{
            os = [System.Environment]::OSVersion.VersionString
            processors = [System.Environment]::ProcessorCount
        }
        benchmark_count = $Count
        benchmarks = @($summary)
        operational_metrics = @($operationalMetrics)
        storage_files = @($storageFiles)
        upstream_postgresql = $upstreamMetric
    }
    $result | ConvertTo-Json -Depth 10 | Set-Content -Encoding utf8 $jsonPath
    $summary | Export-Csv -NoTypeInformation -Encoding utf8 $csvPath
    $summary | Format-Table -AutoSize

    Write-Host "Raw=$rawPath"
    Write-Host "JSON=$jsonPath"
    Write-Host "CSV=$csvPath"
    if (-not $UpstreamUrl) {
        Write-Warning "PostgreSQL round-trip was not measured. Supply -UpstreamUrl, -UpstreamToken and -ConfirmUpstreamWrite while the central API is running."
    }
}
finally {
    Pop-Location
}
