[CmdletBinding()]
param(
    [string]$ReceiverBaseUrl = "http://127.0.0.1:8787",
    [string]$CentralApiBaseUrl = "http://127.0.0.1:8080",
    [int]$LocationId = 4002,
    [int]$AlbionId = 1131,
    [int]$Quality = 1,
    [int]$AverageUnitPrice = 19001,
    [int]$ItemCount = 7
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

function Get-Status {
    Invoke-RestMethod -Method Get -Uri "$ReceiverBaseUrl/api/v1/status" -TimeoutSec 5
}

function Wait-Receiver([int]$Attempts = 40) {
    for ($attempt = 1; $attempt -le $Attempts; $attempt++) {
        try {
            return Get-Status
        } catch {
            Start-Sleep -Milliseconds 500
        }
    }
    throw "El receiver no respondió dentro del tiempo esperado."
}

$status = Wait-Receiver
if (-not $status.history_forwarder.enabled) {
    throw "history_forwarder está deshabilitado."
}

Write-Host "Esta prueba valida persistencia después de caída y reinicio." -ForegroundColor Cyan
Write-Host "1. Detén albion-market-api, pero deja el receiver activo."
Read-Host "Presiona Enter cuando la API central esté apagada"

try {
    Invoke-RestMethod -Method Get -Uri "$CentralApiBaseUrl/healthz" -TimeoutSec 2 | Out-Null
    throw "La API central todavía responde. Deténla antes de continuar."
} catch {
    if ($_.Exception.Message -like "*todavía responde*") { throw }
}

$bucketDate = (Get-Date).ToUniversalTime().Date.AddDays(-1)
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

Write-Host "`n== Captura con API central apagada ==" -ForegroundColor Cyan
$result = Invoke-RestMethod -Method Post -Uri "$ReceiverBaseUrl/markethistories.ingest" -ContentType "application/json" -Body $payload -TimeoutSec 10
$result | ConvertTo-Json -Depth 10
if ($result.forwarded -ne 1) {
    throw "La captura no se persistió en la outbox."
}

Start-Sleep -Seconds 2
$beforeRestart = Get-Status
$beforeRestart.history_forwarder | ConvertTo-Json -Depth 12
if ($beforeRestart.history_forwarder.outbox.pending_entries -lt 1 -and
    $beforeRestart.history_forwarder.outbox.retrying_batches -lt 1 -and
    $beforeRestart.history_forwarder.outbox.processing_batches -lt 1) {
    throw "No se observan entradas persistentes pendientes antes del reinicio."
}

Write-Host "`n2. Reinicia el receiver mientras la API central continúa apagada." -ForegroundColor Yellow
Read-Host "Presiona Enter cuando el receiver vuelva a estar activo"
$afterRestart = Wait-Receiver
$afterRestart.history_forwarder | ConvertTo-Json -Depth 12
if ($afterRestart.history_forwarder.outbox.pending_entries -lt 1 -and
    $afterRestart.history_forwarder.outbox.retrying_batches -lt 1 -and
    $afterRestart.history_forwarder.outbox.processing_batches -lt 1) {
    throw "La outbox no conservó la captura después del reinicio."
}

Write-Host "`n3. Inicia albion-market-api." -ForegroundColor Yellow
Read-Host "Presiona Enter cuando la API central vuelva a estar activa"

$drained = $false
$current = $null
for ($attempt = 1; $attempt -le 60 -and -not $drained; $attempt++) {
    Start-Sleep -Seconds 1
    $current = Wait-Receiver 2
    $outbox = $current.history_forwarder.outbox
    if ($outbox.pending_entries -eq 0 -and $outbox.pending_batches -eq 0 -and
        $outbox.retrying_batches -eq 0 -and $outbox.processing_batches -eq 0) {
        $drained = $true
    }
}
if (-not $drained) {
    throw "La outbox no se vació después de reactivar la API central."
}

$current.history_forwarder | ConvertTo-Json -Depth 12
Write-Host "`nOutbox persistente y recuperación al reiniciar verificadas correctamente." -ForegroundColor Green
