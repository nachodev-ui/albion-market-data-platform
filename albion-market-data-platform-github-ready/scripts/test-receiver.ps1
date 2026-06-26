$ErrorActionPreference = "Stop"

$sample = Get-Content ./samples/market-history.json -Raw | ConvertFrom-Json
$body = $sample.payload | ConvertTo-Json -Depth 10 -Compress

try {
    $response = Invoke-RestMethod `
        -Method Post `
        -Uri "http://127.0.0.1:8788/markethistories.ingest" `
        -ContentType "application/json" `
        -Body $body
} catch {
    Write-Error "No se pudo conectar al receptor de pruebas. Ejecuta primero ./scripts/receiver-test.ps1 en otra terminal. Detalle: $($_.Exception.Message)"
}

$response | ConvertTo-Json -Depth 10
