[CmdletBinding()]
param(
    [switch]$Rebuild,
    [string]$CalculatorPath = "$HOME\Desktop\albion-craft-calculator",
    [string]$AlbionClientPath = "C:\Program Files\Albion Data Client\albiondata-client.exe",
    [string]$ApiPath = "$HOME\Desktop\albion-market-api"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$platformRoot = Split-Path -Parent $PSScriptRoot
$receiverScript = Join-Path $PSScriptRoot "receiver.ps1"
$reprocessScript = Join-Path $PSScriptRoot "reprocess.ps1"
$healthUrl = "http://127.0.0.1:8787/healthz"
$apiHealthUrl = "http://127.0.0.1:8080/healthz"
$ingestTargets = "https+pow://albion-online-data.com,http://127.0.0.1:8787"

function Test-Url([string]$Url) {
    try {
        Invoke-RestMethod -Uri $Url -TimeoutSec 2 -ErrorAction Stop | Out-Null
        return $true
    } catch {
        return $false
    }
}

if (-not (Test-Path $CalculatorPath)) { throw "No se encontró la calculadora en $CalculatorPath" }
if (-not (Test-Path $ApiPath)) { throw "No se encontró la API en $ApiPath" }
if (-not (Test-Path $AlbionClientPath)) { throw "No se encontró Albion Data Client en $AlbionClientPath" }
if (-not (Get-Command pnpm -ErrorAction SilentlyContinue)) { throw "pnpm no está disponible en PATH" }

if ($Rebuild) {
    if (Test-Url $healthUrl) { throw "Detén el receptor antes de usar -Rebuild" }
    Push-Location $platformRoot
    try { & $reprocessScript -Rebuild } finally { Pop-Location }
}

if (-not (Test-Url $apiHealthUrl)) {
    Start-Process powershell.exe -WorkingDirectory $ApiPath -ArgumentList @(
        '-NoExit', '-ExecutionPolicy', 'Bypass', '-Command', 'go run ./cmd/api'
    ) | Out-Null

    $apiReady = $false
    1..20 | ForEach-Object {
        if (-not $apiReady) {
            Start-Sleep -Seconds 1
            $apiReady = Test-Url $apiHealthUrl
        }
    }
    if (-not $apiReady) { throw "La API central no respondió después de 20 segundos" }
}

if (-not (Test-Url $healthUrl)) {
    Start-Process powershell.exe -WorkingDirectory $platformRoot -ArgumentList @(
        '-NoExit', '-ExecutionPolicy', 'Bypass', '-File', "`"$receiverScript`""
    ) | Out-Null

    $ready = $false
    1..20 | ForEach-Object {
        if (-not $ready) {
            Start-Sleep -Seconds 1
            $ready = Test-Url $healthUrl
        }
    }
    if (-not $ready) { throw "El receptor no respondió después de 20 segundos" }
}

if (-not (Get-Process albiondata-client -ErrorAction SilentlyContinue)) {
    Start-Process $AlbionClientPath -ArgumentList @('-i', $ingestTargets) | Out-Null
}

Start-Process powershell.exe -WorkingDirectory $CalculatorPath -ArgumentList @(
    '-NoExit', '-ExecutionPolicy', 'Bypass', '-Command', 'pnpm dev'
) | Out-Null

Write-Host "Sesión iniciada" -ForegroundColor Green
Write-Host "Mercados:    http://127.0.0.1:8787/api/v1/markets"
Write-Host "Estado:      http://127.0.0.1:8787/api/v1/status"
Write-Host "Calculadora: revisa la URL informada por Vite"
Write-Host "Cambia de zona en Albion si el cliente aún no detecta tu ubicación." -ForegroundColor Yellow
