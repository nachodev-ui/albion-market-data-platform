$ErrorActionPreference = "Stop"

Write-Host "== Go =="
go version

Write-Host "`n== Formato =="
$unformatted = gofmt -l ./apps/collector
if ($unformatted) {
    Write-Error "Hay archivos Go sin formatear:`n$unformatted"
}

Write-Host "`n== Pruebas =="
go test -count=1 -timeout 90s ./apps/collector/...

Write-Host "`n== Detector de carreras =="
$cgo = (go env CGO_ENABLED).Trim()
if ($cgo -eq "1") {
    go test -race -count=1 -timeout 120s ./apps/collector/...
} else {
    Write-Warning "Se omite -race porque CGO_ENABLED=$cgo. Las demás validaciones continúan."
}

Write-Host "`n== Vet =="
go vet ./apps/collector/...

Write-Host "`n== Build =="
$buildPath = Join-Path ([System.IO.Path]::GetTempPath()) "albion-market-receiver-check.exe"
go build -o $buildPath ./apps/collector/cmd/receiver
Remove-Item $buildPath -Force -ErrorAction SilentlyContinue

$backfillBuildPath = Join-Path ([System.IO.Path]::GetTempPath()) "albion-market-backfill-check.exe"
go build -o $backfillBuildPath ./apps/collector/cmd/backfillhistory
Remove-Item $backfillBuildPath -Force -ErrorAction SilentlyContinue

$outboxCtlBuildPath = Join-Path ([System.IO.Path]::GetTempPath()) "albion-market-outboxctl-check.exe"
go build -o $outboxCtlBuildPath ./apps/collector/cmd/outboxctl
Remove-Item $outboxCtlBuildPath -Force -ErrorAction SilentlyContinue

$storageCtlBuildPath = Join-Path ([System.IO.Path]::GetTempPath()) "albion-market-storagectl-check.exe"
go build -o $storageCtlBuildPath ./apps/collector/cmd/storagectl
Remove-Item $storageCtlBuildPath -Force -ErrorAction SilentlyContinue

Write-Host "`n== Paquete de fuente seguro =="
$sourcePackagePath = Join-Path ([System.IO.Path]::GetTempPath()) "albion-market-data-platform-source-check.zip"
& (Join-Path $PSScriptRoot "export-source.ps1") -OutputPath $sourcePackagePath
Remove-Item $sourcePackagePath -Force -ErrorAction SilentlyContinue

Write-Host "`nTodo correcto."
