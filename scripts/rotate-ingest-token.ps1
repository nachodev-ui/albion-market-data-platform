[CmdletBinding()]
param(
    [string]$ApiPath = "$HOME\Desktop\albion-market-api",
    [string]$PlatformPath = (Split-Path -Parent $PSScriptRoot),
    [int]$TokenBytes = 32,
    [switch]$DropPrevious
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

if ($TokenBytes -lt 32 -or $TokenBytes -gt 1024) {
    throw "TokenBytes debe estar entre 32 y 1024."
}

$ApiPath = (Resolve-Path $ApiPath).Path
$PlatformPath = (Resolve-Path $PlatformPath).Path
$apiSecrets = Join-Path $ApiPath "secrets"
$platformSecrets = Join-Path $PlatformPath "secrets"
$apiCurrent = Join-Path $apiSecrets "ingest-current.token"
$apiPrevious = Join-Path $apiSecrets "ingest-previous.token"
$platformCurrent = Join-Path $platformSecrets "upstream-current.token"
$apiEnv = Join-Path $ApiPath ".env.local"
$platformEnv = Join-Path $PlatformPath ".env"

function New-SecureToken([int]$ByteCount) {
    $bytes = New-Object byte[] $ByteCount
    $rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
    try {
        $rng.GetBytes($bytes)
    } finally {
        $rng.Dispose()
    }
    return [Convert]::ToBase64String($bytes).TrimEnd('=').Replace('+', '-').Replace('/', '_')
}

function Get-EnvValue([string]$Path, [string]$Key) {
    if (-not (Test-Path $Path)) { return "" }
    foreach ($line in Get-Content $Path) {
        if ($line -match ('^' + [regex]::Escape($Key) + '=(.*)$')) {
            return $Matches[1].Trim()
        }
    }
    return ""
}

function Set-EnvValue([string]$Path, [string]$Key, [string]$Value) {
    $lines = @()
    if (Test-Path $Path) {
        $lines = @(Get-Content $Path)
    }
    $pattern = '^' + [regex]::Escape($Key) + '='
    $replacement = "$Key=$Value"
    $updated = $false
    for ($index = 0; $index -lt $lines.Count; $index++) {
        if ($lines[$index] -match $pattern) {
            $lines[$index] = $replacement
            $updated = $true
            break
        }
    }
    if (-not $updated) {
        $lines += $replacement
    }
    [System.IO.File]::WriteAllLines($Path, $lines, (New-Object System.Text.UTF8Encoding($false)))
}

New-Item -ItemType Directory -Force -Path $apiSecrets, $platformSecrets | Out-Null

$oldKeyId = Get-EnvValue -Path $apiEnv -Key "INGEST_BEARER_TOKEN_ID"
if ([string]::IsNullOrWhiteSpace($oldKeyId)) {
    $oldKeyId = "previous"
}

if ($DropPrevious) {
    if (Test-Path $apiPrevious) {
        Remove-Item -Force $apiPrevious
    }
    Set-EnvValue -Path $apiEnv -Key "INGEST_BEARER_TOKEN_PREVIOUS" -Value ""
    Set-EnvValue -Path $apiEnv -Key "INGEST_BEARER_TOKEN_PREVIOUS_FILE" -Value ""
    Set-EnvValue -Path $apiEnv -Key "INGEST_BEARER_TOKEN_PREVIOUS_ID" -Value "previous"
    Write-Host "Credencial anterior retirada. Reinicia albion-market-api." -ForegroundColor Green
    return
}

if (Test-Path $apiCurrent) {
    Copy-Item -Force $apiCurrent $apiPrevious
}

$newToken = New-SecureToken -ByteCount $TokenBytes
[System.IO.File]::WriteAllText($apiCurrent, $newToken, [System.Text.Encoding]::ASCII)
[System.IO.File]::WriteAllText($platformCurrent, $newToken, [System.Text.Encoding]::ASCII)

$newKeyId = "receiver-" + (Get-Date).ToUniversalTime().ToString("yyyyMMdd-HHmmss")
Set-EnvValue -Path $apiEnv -Key "INGEST_BEARER_TOKEN" -Value ""
Set-EnvValue -Path $apiEnv -Key "INGEST_BEARER_TOKEN_FILE" -Value "./secrets/ingest-current.token"
Set-EnvValue -Path $apiEnv -Key "INGEST_BEARER_TOKEN_ID" -Value $newKeyId
Set-EnvValue -Path $apiEnv -Key "INGEST_MIN_TOKEN_LENGTH" -Value "32"

if (Test-Path $apiPrevious) {
    Set-EnvValue -Path $apiEnv -Key "INGEST_BEARER_TOKEN_PREVIOUS" -Value ""
    Set-EnvValue -Path $apiEnv -Key "INGEST_BEARER_TOKEN_PREVIOUS_FILE" -Value "./secrets/ingest-previous.token"
    Set-EnvValue -Path $apiEnv -Key "INGEST_BEARER_TOKEN_PREVIOUS_ID" -Value $oldKeyId
} else {
    Set-EnvValue -Path $apiEnv -Key "INGEST_BEARER_TOKEN_PREVIOUS" -Value ""
    Set-EnvValue -Path $apiEnv -Key "INGEST_BEARER_TOKEN_PREVIOUS_FILE" -Value ""
    Set-EnvValue -Path $apiEnv -Key "INGEST_BEARER_TOKEN_PREVIOUS_ID" -Value "previous"
}

Set-EnvValue -Path $platformEnv -Key "UPSTREAM_TOKEN" -Value ""
Set-EnvValue -Path $platformEnv -Key "UPSTREAM_TOKEN_FILE" -Value "./secrets/upstream-current.token"
Set-EnvValue -Path $platformEnv -Key "UPSTREAM_MIN_TOKEN_LENGTH" -Value "32"

Write-Host "Credencial de ingesta rotada sin mostrar el token." -ForegroundColor Green
Write-Host "API actual:       $apiCurrent"
if (Test-Path $apiPrevious) { Write-Host "API anterior:     $apiPrevious" }
Write-Host "Platform actual:  $platformCurrent"
Write-Host "Reinicia primero albion-market-api y luego el receiver." -ForegroundColor Yellow
Write-Host "Cuando ningún log use el auth_key_id anterior, ejecuta nuevamente con -DropPrevious para retirarlo." -ForegroundColor Yellow
