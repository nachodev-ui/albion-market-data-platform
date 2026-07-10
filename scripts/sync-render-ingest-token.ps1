[CmdletBinding()]
param(
    [string]$TokenSourcePath = "",
    [string]$ServiceName = "albion-market-api",
    [string]$RenderApiBaseUrl = "https://api.render.com/v1",
    [string]$ApplicationBaseUrl = "https://albion-market-api.onrender.com",
    [int]$DeployTimeoutSeconds = 900,
    [int]$PollIntervalSeconds = 5,
    [switch]$SkipDeploy
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
$renderApiOrigin = $RenderApiBaseUrl.TrimEnd('/')
$applicationOrigin = $ApplicationBaseUrl.TrimEnd('/')

function Resolve-TokenSource {
    param([string]$RequestedPath)

    $candidates = [System.Collections.Generic.List[string]]::new()
    if (-not [string]::IsNullOrWhiteSpace($RequestedPath)) {
        [void]$candidates.Add($RequestedPath)
    }
    [void]$candidates.Add((Join-Path $root "secrets\upstream-current.token"))
    [void]$candidates.Add((Join-Path (Split-Path -Parent $root) "albion-market-api\secrets\deployment\ingest-current.token"))

    foreach ($candidate in $candidates) {
        if (Test-Path -LiteralPath $candidate -PathType Leaf) {
            return (Resolve-Path -LiteralPath $candidate).Path
        }
    }

    throw "No se encontró el token local. Usa -TokenSourcePath o crea secrets\upstream-current.token."
}

function ConvertFrom-SecureValue {
    param([Parameter(Mandatory)] [Security.SecureString]$Value)

    $pointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($Value)
    try {
        return [Runtime.InteropServices.Marshal]::PtrToStringBSTR($pointer)
    }
    finally {
        [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($pointer)
    }
}

function Invoke-RenderApi {
    param(
        [Parameter(Mandatory)] [ValidateSet("GET", "POST", "PUT")] [string]$Method,
        [Parameter(Mandatory)] [string]$Uri,
        [Parameter(Mandatory)] [string]$ApiKey,
        $Body = $null
    )

    $parameters = @{
        Method = $Method
        Uri = $Uri
        Headers = @{
            Authorization = "Bearer $ApiKey"
            Accept = "application/json"
        }
        TimeoutSec = 60
    }

    if ($null -ne $Body) {
        $parameters.ContentType = "application/json"
        $parameters.Body = $Body | ConvertTo-Json -Depth 20 -Compress
    }

    return Invoke-RestMethod @parameters
}

function Get-ServiceCandidates {
    param($Response)

    $services = [System.Collections.Generic.List[object]]::new()
    foreach ($entry in @($Response)) {
        if ($null -ne $entry.PSObject.Properties["service"]) {
            [void]$services.Add($entry.service)
        }
        else {
            [void]$services.Add($entry)
        }
    }
    return @($services)
}

function Test-IngestAuthentication {
    param(
        [Parameter(Mandatory)] [string]$BaseUrl,
        [Parameter(Mandatory)] [string]$Token
    )

    $handler = [System.Net.Http.HttpClientHandler]::new()
    $client = [System.Net.Http.HttpClient]::new($handler)
    try {
        $client.Timeout = [TimeSpan]::FromSeconds(30)
        $request = [System.Net.Http.HttpRequestMessage]::new(
            [System.Net.Http.HttpMethod]::Post,
            "$BaseUrl/api/v1/prices"
        )
        $request.Headers.Authorization = [System.Net.Http.Headers.AuthenticationHeaderValue]::new("Bearer", $Token)
        $request.Content = [System.Net.Http.StringContent]::new(
            "{}",
            [System.Text.Encoding]::UTF8,
            "application/json"
        )

        $response = $client.SendAsync($request).GetAwaiter().GetResult()
        $statusCode = [int]$response.StatusCode
        $body = $response.Content.ReadAsStringAsync().GetAwaiter().GetResult()

        if ($statusCode -eq 401 -or $statusCode -eq 403) {
            throw "Render sigue rechazando el token de ingesta con HTTP $statusCode."
        }
        if ($statusCode -eq 404) {
            throw "No se encontró el endpoint de ingesta esperado en $BaseUrl."
        }
        if ($statusCode -ge 500) {
            throw "La API respondió HTTP $statusCode durante la validación autenticada."
        }

        return [pscustomobject]@{
            status_code = $statusCode
            authentication_accepted = $true
            response_body_present = -not [string]::IsNullOrWhiteSpace($body)
        }
    }
    finally {
        $client.Dispose()
        $handler.Dispose()
    }
}

$resolvedTokenPath = Resolve-TokenSource -RequestedPath $TokenSourcePath
$ingestToken = [System.IO.File]::ReadAllText($resolvedTokenPath).Trim()
if ([string]::IsNullOrWhiteSpace($ingestToken)) {
    throw "El archivo de token está vacío."
}
if ($ingestToken -match "\s") {
    throw "El token local contiene espacios o saltos de línea internos."
}

$secureApiKey = Read-Host "Pega el API key de Render" -AsSecureString
$renderApiKey = ConvertFrom-SecureValue -Value $secureApiKey
$renderApiKey = $renderApiKey.Trim()
if ([string]::IsNullOrWhiteSpace($renderApiKey)) {
    throw "El API key de Render quedó vacío."
}

try {
    Write-Host "Localizando el servicio de Render..." -ForegroundColor Cyan
    $encodedServiceName = [Uri]::EscapeDataString($ServiceName)
    $serviceResponse = Invoke-RenderApi `
        -Method GET `
        -Uri "$renderApiOrigin/services?name=$encodedServiceName&limit=20" `
        -ApiKey $renderApiKey

    $services = @(Get-ServiceCandidates -Response $serviceResponse)
    $matchingServices = @($services | Where-Object { [string]$_.name -ceq $ServiceName })
    if ($matchingServices.Count -ne 1) {
        throw "Se esperaba exactamente un servicio llamado '$ServiceName'; se encontraron $($matchingServices.Count)."
    }

    $service = $matchingServices[0]
    $serviceId = [string]$service.id
    if ([string]::IsNullOrWhiteSpace($serviceId)) {
        throw "Render no devolvió el ID del servicio."
    }

    Write-Host "Actualizando únicamente INGEST_BEARER_TOKEN..." -ForegroundColor Cyan
    $encodedVariableName = [Uri]::EscapeDataString("INGEST_BEARER_TOKEN")
    $null = Invoke-RenderApi `
        -Method PUT `
        -Uri "$renderApiOrigin/services/$serviceId/env-vars/$encodedVariableName" `
        -ApiKey $renderApiKey `
        -Body @{ value = $ingestToken }

    $deployId = ""
    if (-not $SkipDeploy) {
        Write-Host "Iniciando deploy de configuración..." -ForegroundColor Cyan
        $deploy = Invoke-RenderApi `
            -Method POST `
            -Uri "$renderApiOrigin/services/$serviceId/deploys" `
            -ApiKey $renderApiKey `
            -Body @{ deployMode = "deploy_only" }

        $deployId = [string]$deploy.id
        if ([string]::IsNullOrWhiteSpace($deployId)) {
            throw "Render no devolvió el ID del deploy."
        }

        $deadline = (Get-Date).AddSeconds($DeployTimeoutSeconds)
        $terminalFailureStates = @("build_failed", "update_failed", "pre_deploy_failed", "canceled", "cancelled")
        do {
            Start-Sleep -Seconds $PollIntervalSeconds
            $deployState = Invoke-RenderApi `
                -Method GET `
                -Uri "$renderApiOrigin/services/$serviceId/deploys/$deployId" `
                -ApiKey $renderApiKey

            $status = [string]$deployState.status
            Write-Host "Deploy: $status"
            if ($status -eq "live") {
                break
            }
            if ($terminalFailureStates -contains $status) {
                throw "El deploy de Render terminó en estado '$status'."
            }
        } while ((Get-Date) -lt $deadline)

        if ($status -ne "live") {
            throw "El deploy no llegó a estado live dentro de $DeployTimeoutSeconds segundos."
        }
    }

    $ready = Invoke-RestMethod -Method Get -Uri "$applicationOrigin/readyz" -TimeoutSec 60
    if ([string]$ready.status -ne "ok") {
        throw "La API no quedó ready después de sincronizar el token."
    }

    $authCheck = Test-IngestAuthentication -BaseUrl $applicationOrigin -Token $ingestToken

    Write-Host "" 
    Write-Host "Token de ingesta sincronizado y aceptado por Render." -ForegroundColor Green
    [pscustomobject]@{
        service_name = $ServiceName
        service_id = $serviceId
        deploy_id = $deployId
        token_source = $resolvedTokenPath
        readiness = [string]$ready.status
        authentication_status_code = $authCheck.status_code
        authentication_accepted = $authCheck.authentication_accepted
    }
}
finally {
    $ingestToken = $null
    $renderApiKey = $null
    $secureApiKey = $null
    [GC]::Collect()
    [GC]::WaitForPendingFinalizers()
}
