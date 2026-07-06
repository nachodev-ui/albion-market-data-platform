param(
    [string]$BaseUrl = "http://127.0.0.1:8787",
    [string]$ItemId = "T6_MAIN_CURSEDSTAFF_CRYSTAL@3",
    [string]$MarketKey = "brecilien",
    [int]$Quality = 4
)

$ErrorActionPreference = "Stop"
$BaseUrl = $BaseUrl.TrimEnd("/")

Write-Host "Estado del servicio"
$status = Invoke-RestMethod "$BaseUrl/api/v1/status"
$status | ConvertTo-Json -Depth 10

Write-Host "`nMétricas Prometheus"
$metricsResponse = Invoke-WebRequest "$BaseUrl/metrics"
if ($metricsResponse.StatusCode -ne 200) {
    throw "metrics returned $($metricsResponse.StatusCode)"
}
if ($metricsResponse.Headers["Content-Type"] -notmatch "text/plain") {
    throw "metrics Content-Type is not text/plain"
}
$metrics = $metricsResponse.Content
$requiredMetrics = @(
    "albion_receiver_uptime_seconds",
    "albion_receiver_build_info",
    "albion_receiver_captures_received_total",
    "albion_receiver_storage_errors_total",
    "albion_receiver_forwarder_enabled",
    "albion_receiver_outbox_depth"
)
foreach ($metric in $requiredMetrics) {
    if ($metrics -notmatch $metric) {
        throw "metric $metric is missing"
    }
}
Write-Host "Métricas obligatorias presentes"

Write-Host "`nCatálogo de mercados"
Invoke-RestMethod "$BaseUrl/api/v1/markets" | ConvertTo-Json -Depth 10

Write-Host "`nPrecios actuales"
$encodedItem = [Uri]::EscapeDataString($ItemId)
$encodedMarket = [Uri]::EscapeDataString($MarketKey)
Invoke-RestMethod "$BaseUrl/api/v1/prices?server=west&itemIds=$encodedItem&marketKey=$encodedMarket&quality=$Quality" |
    ConvertTo-Json -Depth 10

Write-Host "`nHistorial de cuatro semanas"
Invoke-RestMethod "$BaseUrl/api/v1/history?server=west&itemId=$encodedItem&marketKey=$encodedMarket&quality=$Quality&period=4-weeks&limit=1" |
    ConvertTo-Json -Depth 10
