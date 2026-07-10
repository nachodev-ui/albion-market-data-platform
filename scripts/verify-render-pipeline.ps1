[CmdletBinding()]
param(
    [string]$TokenSourcePath = "",
    [string]$ReceiverBaseUrl = "http://127.0.0.1:8787",
    [string]$RenderBaseUrl = "https://albion-market-api.onrender.com",
    [string]$AlbionDataClientPath = "",
    [int]$TimeoutSeconds = 600,
    [int]$PollIntervalSeconds = 3,
    [switch]$SkipConfiguration,
    [switch]$SkipAlbionDataClientLaunch,
    [switch]$KeepExistingReceiver,
    [switch]$KeepExistingAlbionDataClient
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
$runId = Get-Date -Format "yyyyMMdd-HHmmss"
$artifactRoot = Join-Path $root ".e2e\artifacts\render-pipeline-$runId"
$receiverOrigin = $ReceiverBaseUrl.TrimEnd('/')
$renderOrigin = $RenderBaseUrl.TrimEnd('/')
$receiverStatusUrl = "$receiverOrigin/api/v1/status"
$receiverMetricsUrl = "$receiverOrigin/metrics"
$renderStatusUrl = "$renderOrigin/api/v1/status"
$renderReadyUrl = "$renderOrigin/readyz"
$receiverUri = [Uri]$receiverOrigin
$receiverProcess = $null
$clientProcess = $null

New-Item -ItemType Directory -Force -Path $artifactRoot | Out-Null

function Write-Utf8Json {
    param(
        [Parameter(Mandatory)] $Value,
        [Parameter(Mandatory)] [string]$Path
    )
    $json = $Value | ConvertTo-Json -Depth 30
    [System.IO.File]::WriteAllText($Path, $json, [System.Text.UTF8Encoding]::new($false))
}

function Wait-JsonEndpoint {
    param(
        [Parameter(Mandatory)] [string]$Uri,
        [int]$Attempts = 40,
        [int]$DelaySeconds = 3
    )

    $lastError = $null
    for ($attempt = 1; $attempt -le $Attempts; $attempt++) {
        try {
            return Invoke-RestMethod -Method Get -Uri $Uri -TimeoutSec 30
        }
        catch {
            $lastError = $_
            if ($attempt -lt $Attempts) {
                Start-Sleep -Seconds $DelaySeconds
            }
        }
    }
    throw "No hubo respuesta de '$Uri'. Último error: $($lastError.Exception.Message)"
}

function Test-JsonEndpoint {
    param([Parameter(Mandatory)] [string]$Uri)
    try {
        Invoke-RestMethod -Method Get -Uri $Uri -TimeoutSec 3 | Out-Null
        return $true
    }
    catch {
        return $false
    }
}

function Get-MetricsText {
    param([Parameter(Mandatory)] [string]$Uri)
    return (Invoke-WebRequest -UseBasicParsing -Method Get -Uri $Uri -TimeoutSec 15).Content
}

function Get-MetricTotal {
    param(
        [Parameter(Mandatory)] [string]$Text,
        [Parameter(Mandatory)] [string]$Name
    )

    $sum = 0.0
    $pattern = '^' + [regex]::Escape($Name) + '(?:\{[^}]*\})?\s+([-+0-9.eE]+)\s*$'
    foreach ($line in ($Text -split "`r?`n")) {
        $match = [regex]::Match($line, $pattern)
        if (-not $match.Success) { continue }
        $value = 0.0
        if ([double]::TryParse(
            $match.Groups[1].Value,
            [System.Globalization.NumberStyles]::Float,
            [System.Globalization.CultureInfo]::InvariantCulture,
            [ref]$value
        )) {
            $sum += $value
        }
    }
    return $sum
}

function Get-ApiIngestCounters {
    param([Parameter(Mandatory)] $Value)

    $matches = [System.Collections.Generic.List[object]]::new()

    function Visit-Value {
        param(
            $Current,
            [string]$Path
        )

        if ($null -eq $Current -or $Current -is [string]) { return }

        if ($Current -is [System.Collections.IDictionary]) {
            foreach ($key in $Current.Keys) {
                Visit-Value -Current $Current[$key] -Path "$Path.$key"
            }
            return
        }

        if ($Current -is [System.Collections.IEnumerable] -and -not ($Current -is [pscustomobject])) {
            $index = 0
            foreach ($entry in $Current) {
                Visit-Value -Current $entry -Path "$Path[$index]"
                $index++
            }
            return
        }

        foreach ($property in $Current.PSObject.Properties) {
            $propertyPath = if ([string]::IsNullOrWhiteSpace($Path)) {
                $property.Name
            } else {
                "$Path.$($property.Name)"
            }
            $propertyValue = $property.Value

            if ($propertyValue -is [byte] -or
                $propertyValue -is [int16] -or
                $propertyValue -is [int32] -or
                $propertyValue -is [int64] -or
                $propertyValue -is [uint16] -or
                $propertyValue -is [uint32] -or
                $propertyValue -is [uint64] -or
                $propertyValue -is [single] -or
                $propertyValue -is [double] -or
                $propertyValue -is [decimal]) {
                if ($propertyPath -match '(?i)(ingest|history).*(received|accepted|stored|touched|request|batch|entr|row)') {
                    $matches.Add([pscustomobject]@{
                        path = $propertyPath
                        value = [double]$propertyValue
                    })
                }
                continue
            }

            Visit-Value -Current $propertyValue -Path $propertyPath
        }
    }

    Visit-Value -Current $Value -Path ""
    return @($matches)
}

function Get-CounterSum {
    param([object[]]$Counters)
    $sum = 0.0
    foreach ($counter in $Counters) {
        $sum += [double]$counter.value
    }
    return $sum
}

function Resolve-AlbionDataClient {
    param(
        [string]$RequestedPath,
        [string]$ExistingProcessPath = ""
    )

    foreach ($candidate in @($RequestedPath, $ExistingProcessPath)) {
        if ([string]::IsNullOrWhiteSpace($candidate)) { continue }
        if (Test-Path -LiteralPath $candidate -PathType Leaf) {
            return (Resolve-Path -LiteralPath $candidate).Path
        }
    }

    $candidates = [System.Collections.Generic.List[string]]::new()
    if (-not [string]::IsNullOrWhiteSpace($env:ProgramFiles)) {
        $candidates.Add((Join-Path $env:ProgramFiles "Albion Data Client\albiondata-client.exe"))
    }
    $programFilesX86 = [Environment]::GetEnvironmentVariable("ProgramFiles(x86)")
    if (-not [string]::IsNullOrWhiteSpace($programFilesX86)) {
        $candidates.Add((Join-Path $programFilesX86 "Albion Data Client\albiondata-client.exe"))
    }

    return $candidates |
        Where-Object { Test-Path -LiteralPath $_ -PathType Leaf } |
        Select-Object -First 1
}

function Stop-ReceiverListener {
    param([int]$Port)

    if (-not (Get-Command Get-NetTCPConnection -ErrorAction SilentlyContinue)) {
        throw "No se puede reiniciar el receiver automáticamente porque Get-NetTCPConnection no está disponible. Usa -KeepExistingReceiver o detén el receiver manualmente."
    }

    $listeners = @(Get-NetTCPConnection -LocalPort $Port -State Listen -ErrorAction SilentlyContinue)
    foreach ($listener in $listeners) {
        if ([int]$listener.OwningProcess -le 0) { continue }
        Stop-Process -Id ([int]$listener.OwningProcess) -Force -ErrorAction Stop
    }

    for ($attempt = 1; $attempt -le 20; $attempt++) {
        if (-not (Test-JsonEndpoint -Uri $receiverStatusUrl)) { return }
        Start-Sleep -Milliseconds 250
    }
    throw "El receiver anterior sigue respondiendo después de intentar reiniciarlo."
}

if (-not $SkipConfiguration) {
    $configureScript = Join-Path $PSScriptRoot "configure-render-production.ps1"
    $configureParameters = @{ ApiBaseUrl = $renderOrigin }
    if (-not [string]::IsNullOrWhiteSpace($TokenSourcePath)) {
        $configureParameters.TokenSourcePath = $TokenSourcePath
    }
    & $configureScript @configureParameters | Out-Host
}

Write-Host "Comprobando Render..." -ForegroundColor Cyan
$renderReady = Wait-JsonEndpoint -Uri $renderReadyUrl -Attempts 12 -DelaySeconds 5
if ($renderReady.status -ne "ok") {
    throw "Render readiness no está en estado ok."
}

$receiverAlreadyRunning = Test-JsonEndpoint -Uri $receiverStatusUrl
if ($receiverAlreadyRunning -and -not $KeepExistingReceiver) {
    Write-Host "Reiniciando el receiver para aplicar el perfil de Render..." -ForegroundColor Cyan
    Stop-ReceiverListener -Port $receiverUri.Port
    $receiverAlreadyRunning = $false
}

if (-not $receiverAlreadyRunning) {
    Write-Host "Iniciando receiver..." -ForegroundColor Cyan
    $receiverScript = Join-Path $PSScriptRoot "receiver.ps1"
    $shell = Get-Command pwsh -ErrorAction SilentlyContinue
    if ($null -eq $shell) {
        $shell = Get-Command powershell.exe -ErrorAction Stop
    }
    $receiverStdout = Join-Path $artifactRoot "receiver.stdout.log"
    $receiverStderr = Join-Path $artifactRoot "receiver.stderr.log"
    $receiverProcess = Start-Process `
        -FilePath $shell.Source `
        -WorkingDirectory $root `
        -ArgumentList @(
            "-NoProfile",
            "-ExecutionPolicy", "Bypass",
            "-File", "`"$receiverScript`""
        ) `
        -RedirectStandardOutput $receiverStdout `
        -RedirectStandardError $receiverStderr `
        -PassThru
}

$receiverBefore = Wait-JsonEndpoint -Uri $receiverStatusUrl -Attempts 60 -DelaySeconds 1
if (-not [bool]$receiverBefore.price_forwarder.enabled) {
    throw "El forwarder de precios no quedó habilitado."
}
if (-not [bool]$receiverBefore.history_forwarder.enabled) {
    throw "El forwarder histórico no quedó habilitado."
}

$metricsBefore = Get-MetricsText -Uri $receiverMetricsUrl
$apiBefore = Wait-JsonEndpoint -Uri $renderStatusUrl -Attempts 12 -DelaySeconds 5
$apiCountersBefore = @(Get-ApiIngestCounters -Value $apiBefore)

Write-Utf8Json -Value $receiverBefore -Path (Join-Path $artifactRoot "receiver-before.json")
Write-Utf8Json -Value $apiBefore -Path (Join-Path $artifactRoot "render-before.json")
[System.IO.File]::WriteAllText(
    (Join-Path $artifactRoot "receiver-metrics-before.prom"),
    $metricsBefore,
    [System.Text.UTF8Encoding]::new($false)
)
Write-Utf8Json -Value $apiCountersBefore -Path (Join-Path $artifactRoot "render-counters-before.json")

$capturesBefore = Get-MetricTotal -Text $metricsBefore -Name "albion_receiver_captures_received_total"
$entriesBefore = Get-MetricTotal -Text $metricsBefore -Name "albion_receiver_entries_received_total"
$batchesBefore = Get-MetricTotal -Text $metricsBefore -Name "albion_receiver_forwarder_batches_sent_total"
$lastSuccessBefore = Get-MetricTotal -Text $metricsBefore -Name "albion_receiver_upstream_last_success_timestamp_seconds"
$deadLettersBefore = Get-MetricTotal -Text $metricsBefore -Name "albion_receiver_dead_letter_batches_total"
$apiSumBefore = Get-CounterSum -Counters $apiCountersBefore

if (-not $SkipAlbionDataClientLaunch) {
    $existingClients = @(Get-Process -Name "albiondata-client" -ErrorAction SilentlyContinue)
    $existingClientPath = ""
    if ($existingClients.Count -gt 0) {
        try { $existingClientPath = [string]$existingClients[0].Path } catch {}
    }

    if ($existingClients.Count -gt 0 -and $KeepExistingAlbionDataClient) {
        Write-Host "Albion Data Client ya estaba ejecutándose; se conservará por solicitud." -ForegroundColor Yellow
    }
    else {
        $resolvedClientPath = Resolve-AlbionDataClient `
            -RequestedPath $AlbionDataClientPath `
            -ExistingProcessPath $existingClientPath
        if ([string]::IsNullOrWhiteSpace($resolvedClientPath)) {
            throw "No se encontró Albion Data Client. Usa -AlbionDataClientPath o -SkipAlbionDataClientLaunch."
        }

        foreach ($existingClient in $existingClients) {
            Stop-Process -Id $existingClient.Id -Force -ErrorAction Stop
        }
        if ($existingClients.Count -gt 0) {
            Start-Sleep -Seconds 1
        }

        $clientStdout = Join-Path $artifactRoot "albion-data-client.stdout.log"
        $clientStderr = Join-Path $artifactRoot "albion-data-client.stderr.log"
        $destinations = "https+pow://albion-online-data.com,http://127.0.0.1:8787"
        $clientProcess = Start-Process `
            -FilePath $resolvedClientPath `
            -ArgumentList @("-i", "`"$destinations`"") `
            -RedirectStandardOutput $clientStdout `
            -RedirectStandardError $clientStderr `
            -PassThru
        Write-Host "Albion Data Client iniciado con AODP y el receiver local." -ForegroundColor Green
    }
}

Write-Host ""
Write-Host "Abre Albion Online, cambia de zona si es necesario y visita un mercado." -ForegroundColor Yellow
Write-Host "El verificador esperará una captura real y un batch aceptado por Render." -ForegroundColor Yellow

$deadline = (Get-Date).AddSeconds($TimeoutSeconds)
$success = $false
$metricsAfter = $metricsBefore
$receiverAfter = $receiverBefore
$apiAfter = $apiBefore
$apiCountersAfter = $apiCountersBefore

while ((Get-Date) -lt $deadline) {
    Start-Sleep -Seconds $PollIntervalSeconds

    try {
        $metricsAfter = Get-MetricsText -Uri $receiverMetricsUrl
        $receiverAfter = Invoke-RestMethod -Method Get -Uri $receiverStatusUrl -TimeoutSec 10
        $apiAfter = Invoke-RestMethod -Method Get -Uri $renderStatusUrl -TimeoutSec 30
        $apiCountersAfter = @(Get-ApiIngestCounters -Value $apiAfter)
    }
    catch {
        Write-Host "Esperando servicios: $($_.Exception.Message)" -ForegroundColor DarkYellow
        continue
    }

    $capturesAfter = Get-MetricTotal -Text $metricsAfter -Name "albion_receiver_captures_received_total"
    $entriesAfter = Get-MetricTotal -Text $metricsAfter -Name "albion_receiver_entries_received_total"
    $batchesAfter = Get-MetricTotal -Text $metricsAfter -Name "albion_receiver_forwarder_batches_sent_total"
    $lastSuccessAfter = Get-MetricTotal -Text $metricsAfter -Name "albion_receiver_upstream_last_success_timestamp_seconds"
    $deadLettersAfter = Get-MetricTotal -Text $metricsAfter -Name "albion_receiver_dead_letter_batches_total"
    $apiSumAfter = Get-CounterSum -Counters $apiCountersAfter

    $captureObserved = $capturesAfter -gt $capturesBefore -and $entriesAfter -gt $entriesBefore
    $forwardObserved = $batchesAfter -gt $batchesBefore -and $lastSuccessAfter -gt $lastSuccessBefore
    $noNewDeadLetter = $deadLettersAfter -le $deadLettersBefore
    $apiCounterObservable = $apiCountersBefore.Count -gt 0 -or $apiCountersAfter.Count -gt 0
    $apiObserved = (-not $apiCounterObservable) -or ($apiSumAfter -gt $apiSumBefore)

    Write-Host (
        "capturas +{0}; entradas +{1}; batches +{2}; API +{3}; dead-letter +{4}" -f
        ($capturesAfter - $capturesBefore),
        ($entriesAfter - $entriesBefore),
        ($batchesAfter - $batchesBefore),
        ($apiSumAfter - $apiSumBefore),
        ($deadLettersAfter - $deadLettersBefore)
    )

    if ($captureObserved -and $forwardObserved -and $noNewDeadLetter -and $apiObserved) {
        $success = $true
        break
    }
}

Write-Utf8Json -Value $receiverAfter -Path (Join-Path $artifactRoot "receiver-after.json")
Write-Utf8Json -Value $apiAfter -Path (Join-Path $artifactRoot "render-after.json")
[System.IO.File]::WriteAllText(
    (Join-Path $artifactRoot "receiver-metrics-after.prom"),
    $metricsAfter,
    [System.Text.UTF8Encoding]::new($false)
)
Write-Utf8Json -Value $apiCountersAfter -Path (Join-Path $artifactRoot "render-counters-after.json")

$summary = [ordered]@{
    success = $success
    completed_at = (Get-Date).ToUniversalTime().ToString("o")
    receiver_url = $receiverOrigin
    render_url = $renderOrigin
    captures_delta = (Get-MetricTotal -Text $metricsAfter -Name "albion_receiver_captures_received_total") - $capturesBefore
    entries_delta = (Get-MetricTotal -Text $metricsAfter -Name "albion_receiver_entries_received_total") - $entriesBefore
    batches_sent_delta = (Get-MetricTotal -Text $metricsAfter -Name "albion_receiver_forwarder_batches_sent_total") - $batchesBefore
    last_success_before = $lastSuccessBefore
    last_success_after = Get-MetricTotal -Text $metricsAfter -Name "albion_receiver_upstream_last_success_timestamp_seconds"
    dead_letter_delta = (Get-MetricTotal -Text $metricsAfter -Name "albion_receiver_dead_letter_batches_total") - $deadLettersBefore
    render_ingest_counter_delta = (Get-CounterSum -Counters $apiCountersAfter) - $apiSumBefore
    render_counter_fields = $apiCountersAfter.Count
    receiver_started_by_script = $null -ne $receiverProcess
    albion_data_client_started_by_script = $null -ne $clientProcess
    artifact_directory = $artifactRoot
}
Write-Utf8Json -Value $summary -Path (Join-Path $artifactRoot "summary.json")

if (-not $success) {
    throw "No se confirmó el pipeline Albion Data Client → receiver → Render dentro de $TimeoutSeconds segundos. Evidencia: $artifactRoot"
}

Write-Host ""
Write-Host "Pipeline Albion Data Client → receiver → Render confirmado." -ForegroundColor Green
Write-Host "Evidencia: $artifactRoot" -ForegroundColor Green
[pscustomobject]$summary
