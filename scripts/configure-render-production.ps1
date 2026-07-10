[CmdletBinding()]
param(
    [string]$EnvironmentPath = "",
    [string]$TokenSourcePath = "",
    [string]$ApiBaseUrl = "https://albion-market-api.onrender.com",
    [switch]$SkipApiCheck,
    [switch]$NoBackup
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
$templatePath = Join-Path $root "docs\examples\render-production.env.example"
$tokenDestinationPath = Join-Path $root "secrets\upstream-current.token"
$utf8NoBom = [System.Text.UTF8Encoding]::new($false)

function Resolve-ProjectPath {
    param(
        [string]$Path,
        [string]$DefaultRelativePath
    )

    if ([string]::IsNullOrWhiteSpace($Path)) {
        return [System.IO.Path]::GetFullPath((Join-Path $root $DefaultRelativePath))
    }
    if ([System.IO.Path]::IsPathRooted($Path)) {
        return [System.IO.Path]::GetFullPath($Path)
    }
    return [System.IO.Path]::GetFullPath((Join-Path $root $Path))
}

function Set-DotEnvValue {
    param(
        [string[]]$Lines,
        [string]$Name,
        [string]$Value
    )

    $result = [System.Collections.Generic.List[string]]::new()
    $pattern = '^\s*' + [regex]::Escape($Name) + '\s*='
    $replaced = $false

    foreach ($line in $Lines) {
        if ($line -match $pattern) {
            if (-not $replaced) {
                $result.Add("$Name=$Value")
                $replaced = $true
            }
            continue
        }
        $result.Add($line)
    }

    if (-not $replaced) {
        $result.Add("$Name=$Value")
    }

    return $result.ToArray()
}

function Read-ValidatedToken {
    param([string]$Path)

    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        throw "No se encontró el token de ingesta en '$Path'."
    }

    $token = [System.IO.File]::ReadAllText($Path).Trim()
    if ($token.Length -ge 2) {
        $first = $token.Substring(0, 1)
        $last = $token.Substring($token.Length - 1, 1)
        if (($first -eq '"' -and $last -eq '"') -or ($first -eq "'" -and $last -eq "'")) {
            $token = $token.Substring(1, $token.Length - 2).Trim()
        }
    }

    if ([string]::IsNullOrWhiteSpace($token)) {
        throw "El archivo de token está vacío."
    }
    if ($token.Length -lt 32) {
        throw "El token tiene menos de 32 caracteres."
    }
    if ($token -match '\s') {
        throw "El token contiene espacios o saltos de línea internos."
    }

    return $token
}

function Test-RenderApi {
    param([string]$BaseUrl)

    $lastError = $null
    for ($attempt = 1; $attempt -le 12; $attempt++) {
        try {
            $health = Invoke-RestMethod -Method Get -Uri "$BaseUrl/healthz" -TimeoutSec 30
            $ready = Invoke-RestMethod -Method Get -Uri "$BaseUrl/readyz" -TimeoutSec 30
            if ($health.status -ne "ok" -or $ready.status -ne "ok") {
                throw "La API respondió, pero health/readiness no están en estado ok."
            }
            return
        }
        catch {
            $lastError = $_
            if ($attempt -lt 12) {
                Start-Sleep -Seconds 5
            }
        }
    }

    throw "Render no quedó disponible después de 12 intentos. Último error: $($lastError.Exception.Message)"
}

if (-not (Test-Path -LiteralPath $templatePath -PathType Leaf)) {
    throw "Falta el perfil '$templatePath'."
}

$ApiBaseUrl = $ApiBaseUrl.Trim().TrimEnd('/')
if ($ApiBaseUrl -notmatch '^https://') {
    throw "ApiBaseUrl debe usar HTTPS."
}

$resolvedEnvironmentPath = Resolve-ProjectPath -Path $EnvironmentPath -DefaultRelativePath ".env"

if ([string]::IsNullOrWhiteSpace($TokenSourcePath)) {
    $workspaceRoot = Split-Path $root -Parent
    $desktop = [Environment]::GetFolderPath("Desktop")
    $candidates = @(
        $tokenDestinationPath,
        (Join-Path $workspaceRoot "albion-market-api\secrets\deployment\ingest-current.token"),
        (Join-Path $desktop "albion-market-api\secrets\deployment\ingest-current.token")
    )

    $TokenSourcePath = $candidates |
        Where-Object { Test-Path -LiteralPath $_ -PathType Leaf } |
        Select-Object -First 1

    if ([string]::IsNullOrWhiteSpace($TokenSourcePath)) {
        throw "No se encontró el token automáticamente. Usa -TokenSourcePath con el archivo ingest-current.token de albion-market-api."
    }
}
else {
    $TokenSourcePath = Resolve-ProjectPath -Path $TokenSourcePath -DefaultRelativePath ""
}

$token = Read-ValidatedToken -Path $TokenSourcePath

if (-not $SkipApiCheck) {
    Write-Host "Comprobando health y readiness de Render..." -ForegroundColor Cyan
    Test-RenderApi -BaseUrl $ApiBaseUrl
}

$tokenDirectory = Split-Path -Parent $tokenDestinationPath
New-Item -ItemType Directory -Force -Path $tokenDirectory | Out-Null
[System.IO.File]::WriteAllText($tokenDestinationPath, $token, $utf8NoBom)

if (Test-Path -LiteralPath $resolvedEnvironmentPath -PathType Leaf) {
    $lines = @(Get-Content -LiteralPath $resolvedEnvironmentPath)
    if (-not $NoBackup) {
        $stamp = Get-Date -Format "yyyyMMdd-HHmmss"
        $backupPath = "$resolvedEnvironmentPath.backup-$stamp"
        Copy-Item -LiteralPath $resolvedEnvironmentPath -Destination $backupPath -Force
    }
}
else {
    $environmentDirectory = Split-Path -Parent $resolvedEnvironmentPath
    if (-not [string]::IsNullOrWhiteSpace($environmentDirectory)) {
        New-Item -ItemType Directory -Force -Path $environmentDirectory | Out-Null
    }
    $lines = @(Get-Content -LiteralPath $templatePath)
}

$settings = [ordered]@{
    APP_ENV = "production"
    LOAD_DOTENV = "true"
    LOG_COLOR = "never"
    LOG_FORMAT = "json"
    COLLECTOR_LISTEN = "127.0.0.1:8787"
    COLLECTOR_ALLOW_REMOTE = "false"
    UPSTREAM_ENABLED = "true"
    UPSTREAM_HISTORY_ENABLED = "true"
    UPSTREAM_BASE_URL = $ApiBaseUrl
    UPSTREAM_TOKEN = ""
    UPSTREAM_TOKEN_FILE = "./secrets/upstream-current.token"
    UPSTREAM_MIN_TOKEN_LENGTH = "32"
    UPSTREAM_REQUIRE_HTTPS = "true"
    UPSTREAM_RETRY_COUNT = "5"
    UPSTREAM_RETRY_DELAY = "1s"
    UPSTREAM_MAX_DELIVERY_ATTEMPTS = "12"
    UPSTREAM_MAX_RETRY_DELAY = "5m"
    UPSTREAM_TIMEOUT = "30s"
}

foreach ($entry in $settings.GetEnumerator()) {
    $lines = @(Set-DotEnvValue -Lines $lines -Name $entry.Key -Value ([string]$entry.Value))
}

$environmentContent = ($lines -join [Environment]::NewLine).TrimEnd() + [Environment]::NewLine
[System.IO.File]::WriteAllText($resolvedEnvironmentPath, $environmentContent, $utf8NoBom)

$token = $null
[GC]::Collect()
[GC]::WaitForPendingFinalizers()

Write-Host "Configuración de Render aplicada correctamente." -ForegroundColor Green
[pscustomobject]@{
    environment_path = $resolvedEnvironmentPath
    token_file = $tokenDestinationPath
    upstream_base_url = $ApiBaseUrl
    app_environment = "production"
    dotenv_enabled = $true
    https_required = $true
    api_checked = -not [bool]$SkipApiCheck
}
