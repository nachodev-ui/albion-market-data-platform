[CmdletBinding()]
param(
    [string]$ReceiverBaseUrl = "http://127.0.0.1:8787",
    [string]$CentralApiBaseUrl = "http://127.0.0.1:8080",
    [string]$Server = "west",
    [string]$MarketKey = "fort_sterling",
    [int]$LocationId = 4002,
    [int]$AlbionId = 1131,
    [string]$ItemId = "T5_LEATHER_LEVEL4@4",
    [int]$Quality = 1,
    [int]$AverageUnitPrice = 18750,
    [int]$ItemCount = 42
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

function Get-Json([string]$Url) {
    Invoke-RestMethod -Method Get -Uri $Url -TimeoutSec 5 -ErrorAction Stop
}

Write-Host "== Estado del receiver ==" -ForegroundColor Cyan
$status = Get-Json "$ReceiverBaseUrl/api/v1/status"
if (-not $status.history_forwarder.enabled) {
    throw "history_forwarder está deshabilitado. Revisa UPSTREAM_HISTORY_ENABLED y reinicia el receiver."
}
$status.history_forwarder | ConvertTo-Json -Depth 10

Write-Host "`n== Estado de la API central ==" -ForegroundColor Cyan
Get-Json "$CentralApiBaseUrl/healthz" | ConvertTo-Json -Depth 5

$bucketDate = (Get-Date).ToUniversalTime().Date.AddDays(-1)
$bucketDateText = $bucketDate.ToString("yyyy-MM-dd")
$wireSilver = [int64]$AverageUnitPrice * [int64]$ItemCount * 10000L

$payload = @{
    AlbionId = $AlbionId
    LocationId = [string]$LocationId
    QualityLevel = $Quality
    Timescale = 2
    MarketHistories = @(
        @{
            ItemAmount = $ItemCount
            SilverAmount = $wireSilver
            Timestamp = [uint64]$bucketDate.Ticks
        }
    )
} | ConvertTo-Json -Depth 8

Write-Host "`n== Enviando captura al receiver ==" -ForegroundColor Cyan
$receiverResult = Invoke-RestMethod `
    -Method Post `
    -Uri "$ReceiverBaseUrl/markethistories.ingest" `
    -ContentType "application/json" `
    -Body $payload `
    -TimeoutSec 10
$receiverResult | ConvertTo-Json -Depth 10

if ($receiverResult.forwarded -ne 1) {
    throw "El receiver normalizó la captura, pero no la encoló para historial central."
}

$encodedItem = [uri]::EscapeDataString($ItemId)
$encodedMarket = [uri]::EscapeDataString($MarketKey)
$queryUrl = "$CentralApiBaseUrl/api/v1/history?server=$Server&marketKey=$encodedMarket&itemId=$encodedItem&quality=$Quality&rangeStart=$bucketDateText&rangeEnd=$bucketDateText"

Write-Host "`n== Esperando persistencia central ==" -ForegroundColor Cyan
$found = $false
$result = $null
1..20 | ForEach-Object {
    if (-not $found) {
        Start-Sleep -Milliseconds 500
        $result = Get-Json $queryUrl
        if ($result.count -gt 0 -and $result.bucketCount -gt 0) {
            $found = $true
        }
    }
}

if (-not $found) {
    $latestStatus = Get-Json "$ReceiverBaseUrl/api/v1/status"
    Write-Host "Último estado history_forwarder:" -ForegroundColor Yellow
    $latestStatus.history_forwarder | ConvertTo-Json -Depth 10
    throw "La captura no apareció en la API central dentro del tiempo esperado."
}

$result | ConvertTo-Json -Depth 12
Write-Host "`nForwarder histórico verificado correctamente." -ForegroundColor Green
