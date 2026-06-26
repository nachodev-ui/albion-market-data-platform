param(
    [string]$BaseUrl = "http://127.0.0.1:8787",
    [string]$ItemId = "T6_MAIN_CURSEDSTAFF_CRYSTAL@3",
    [string]$MarketKey = "brecilien",
    [int]$Quality = 4
)

$ErrorActionPreference = "Stop"

Write-Host "Estado del servicio"
Invoke-RestMethod "$BaseUrl/api/v1/status" | ConvertTo-Json -Depth 10

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
