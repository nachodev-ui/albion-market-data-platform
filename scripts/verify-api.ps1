param(
    [string]$BaseUrl = "http://127.0.0.1:8787",
    [string]$ItemId = "T6_MAIN_CURSEDSTAFF_CRYSTAL@3",
    [string]$MarketKey = "brecilien",
    [int]$Quality = 4,
    [string]$RequestId = "verify-api-0001"
)

$ErrorActionPreference = "Stop"
$BaseUrl = $BaseUrl.TrimEnd("/")

Write-Host "Health y correlación"
$health = Invoke-WebRequest "$BaseUrl/healthz" -Headers @{
    "X-Request-ID" = $RequestId
}
if ($health.StatusCode -ne 200) {
    throw "healthz returned $($health.StatusCode)"
}
if ($health.Headers["X-Request-ID"] -ne $RequestId) {
    throw "X-Request-ID was not preserved"
}

Write-Host "`nEstado del servicio"
$status = Invoke-RestMethod "$BaseUrl/api/v1/status"
$status | ConvertTo-Json -Depth 10
if ($null -eq $status.price_forwarder) {
    throw "price_forwarder field is missing"
}
if ($null -eq $status.history_forwarder) {
    throw "history_forwarder field is missing"
}
if ($null -eq $status.forwarder) {
    throw "forwarder compatibility alias is missing"
}

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
    "albion_receiver_entries_received_total",
    "albion_receiver_storage_errors_total",
    "albion_receiver_storage_bytes",
    "albion_receiver_forwarder_enabled",
    "albion_receiver_forwarder_running",
    "albion_receiver_outbox_depth",
    "albion_receiver_outbox_capacity"
)
foreach ($metric in $requiredMetrics) {
    if ($metrics -notmatch "(?m)^# HELP $metric\b") {
        throw "metric $metric is missing HELP"
    }
    if ($metrics -notmatch "(?m)^$metric(\{|\s)") {
        throw "metric $metric is missing samples"
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
