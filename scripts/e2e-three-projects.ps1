[CmdletBinding()]
param(
    [string]$CraftRepo = "",
    [string]$ApiRepo = "",
    [string]$DatabaseUrl = $env:E2E_DATABASE_URL,
    [int]$ApiPort = 18080,
    [int]$ReceiverPort = 18787,
    [string]$IngestToken = "albion-e2e-local-token",
    [switch]$SkipQuality,
    [switch]$AllowNonE2EDatabase
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$PlatformRepo = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$WorkspaceRoot = Split-Path $PlatformRepo -Parent
if ([string]::IsNullOrWhiteSpace($CraftRepo)) {
    $CraftRepo = Join-Path $WorkspaceRoot "albion-craft-calculator"
}
if ([string]::IsNullOrWhiteSpace($ApiRepo)) {
    $ApiRepo = Join-Path $WorkspaceRoot "albion-market-api"
}
$CraftRepo = (Resolve-Path $CraftRepo).Path
$ApiRepo = (Resolve-Path $ApiRepo).Path

if ([string]::IsNullOrWhiteSpace($DatabaseUrl)) {
    throw "Debes indicar -DatabaseUrl o definir E2E_DATABASE_URL. Usa una base dedicada, por ejemplo albion_market_e2e."
}

try {
    $databaseUri = [Uri]$DatabaseUrl
    $databaseName = $databaseUri.AbsolutePath.Trim('/')
} catch {
    throw "DatabaseUrl no es una URI PostgreSQL válida: $DatabaseUrl"
}
if (-not $AllowNonE2EDatabase -and $databaseName -notmatch '(?i)e2e|test') {
    throw "La base '$databaseName' no parece dedicada a pruebas. Usa una base con e2e/test en el nombre o agrega -AllowNonE2EDatabase conscientemente."
}

$RunId = Get-Date -Format "yyyyMMdd-HHmmss"
$E2ERoot = Join-Path $PlatformRepo ".e2e"
$RuntimeRoot = Join-Path $E2ERoot "runtime"
$ArtifactRoot = Join-Path (Join-Path $E2ERoot "artifacts") $RunId
$ReceiverData = Join-Path $RuntimeRoot "receiver-data"
$BinRoot = Join-Path $RuntimeRoot "bin"
$ApiWorkDirectory = Join-Path $RuntimeRoot "api-work"
$ExecutableSuffix = if ($env:OS -eq "Windows_NT") { ".exe" } else { "" }
$ApiBinary = Join-Path $BinRoot "albion-market-api$ExecutableSuffix"
$ReceiverBinary = Join-Path $BinRoot "albion-market-receiver$ExecutableSuffix"
$ApiBaseUrl = "http://127.0.0.1:$ApiPort"
$ReceiverBaseUrl = "http://127.0.0.1:$ReceiverPort"
$CentralApiUrl = "$ApiBaseUrl/api/v1"
$LocalApiUrl = "$ReceiverBaseUrl/api/v1"
$Results = [System.Collections.Generic.List[object]]::new()
$RunningOnWindows = $env:OS -eq "Windows_NT"
$ApiProcess = $null
$ReceiverProcess = $null
$ProcessStarts = @{}

New-Item -ItemType Directory -Force -Path $RuntimeRoot, $ArtifactRoot, $ReceiverData | Out-Null
Get-ChildItem $RuntimeRoot -Force -ErrorAction SilentlyContinue | Remove-Item -Recurse -Force
New-Item -ItemType Directory -Force -Path $ReceiverData, $BinRoot, $ApiWorkDirectory | Out-Null

function Assert-True {
    param(
        [Parameter(Mandatory)] [bool]$Condition,
        [Parameter(Mandatory)] [string]$Message
    )
    if (-not $Condition) { throw $Message }
}

function Add-Result {
    param(
        [string]$Id,
        [string]$Name,
        [string]$Status,
        [double]$DurationSeconds,
        [string]$Detail = ""
    )
    $Results.Add([pscustomobject]@{
        id = $Id
        name = $Name
        status = $Status
        duration_seconds = [Math]::Round($DurationSeconds, 3)
        detail = $Detail
        completed_at = (Get-Date).ToUniversalTime().ToString("o")
    })
}

function Invoke-Scenario {
    param(
        [Parameter(Mandatory)] [string]$Id,
        [Parameter(Mandatory)] [string]$Name,
        [Parameter(Mandatory)] [scriptblock]$Action
    )
    Write-Host "`n[$Id] $Name" -ForegroundColor Cyan
    $watch = [System.Diagnostics.Stopwatch]::StartNew()
    try {
        & $Action
        $watch.Stop()
        Add-Result -Id $Id -Name $Name -Status "passed" -DurationSeconds $watch.Elapsed.TotalSeconds
        Write-Host "[OK] $Name" -ForegroundColor Green
    } catch {
        $watch.Stop()
        Add-Result -Id $Id -Name $Name -Status "failed" -DurationSeconds $watch.Elapsed.TotalSeconds -Detail $_.Exception.Message
        Write-Host "[FAIL] $Name`n$($_.Exception.Message)" -ForegroundColor Red
        throw
    }
}

function Assert-Command {
    param([Parameter(Mandatory)] [string]$Name)
    if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
        throw "No se encontró '$Name' en PATH."
    }
}

function Invoke-External {
    param(
        [Parameter(Mandatory)] [string]$WorkingDirectory,
        [Parameter(Mandatory)] [string]$FilePath,
        [Parameter(Mandatory)] [string[]]$Arguments,
        [hashtable]$Environment = @{}
    )
    $previous = @{}
    foreach ($key in $Environment.Keys) {
        $previous[$key] = [Environment]::GetEnvironmentVariable($key, "Process")
        [Environment]::SetEnvironmentVariable($key, [string]$Environment[$key], "Process")
    }
    Push-Location $WorkingDirectory
    try {
        & $FilePath @Arguments
        if ($LASTEXITCODE -ne 0) {
            throw "'$FilePath $($Arguments -join ' ')' terminó con código $LASTEXITCODE."
        }
    } finally {
        Pop-Location
        foreach ($key in $Environment.Keys) {
            [Environment]::SetEnvironmentVariable($key, $previous[$key], "Process")
        }
    }
}

function Start-LoggedProcess {
    param(
        [Parameter(Mandatory)] [string]$Name,
        [Parameter(Mandatory)] [string]$WorkingDirectory,
        [Parameter(Mandatory)] [string]$FilePath,
        [Parameter(Mandatory)] [string[]]$Arguments,
        [Parameter(Mandatory)] [hashtable]$Environment
    )
    if (-not $script:ProcessStarts.ContainsKey($Name)) {
        $script:ProcessStarts[$Name] = 0
    }
    $script:ProcessStarts[$Name] = [int]$script:ProcessStarts[$Name] + 1
    $attempt = "{0:D2}" -f $script:ProcessStarts[$Name]
    $stdout = Join-Path $ArtifactRoot "$Name-$attempt.stdout.log"
    $stderr = Join-Path $ArtifactRoot "$Name-$attempt.stderr.log"
    $previous = @{}
    foreach ($key in $Environment.Keys) {
        $previous[$key] = [Environment]::GetEnvironmentVariable($key, "Process")
        [Environment]::SetEnvironmentVariable($key, [string]$Environment[$key], "Process")
    }
    try {
        $startParameters = @{
            FilePath = $FilePath
            WorkingDirectory = $WorkingDirectory
            RedirectStandardOutput = $stdout
            RedirectStandardError = $stderr
            PassThru = $true
        }
        if ($Arguments.Count -gt 0) {
            $startParameters.ArgumentList = $Arguments
        }
        $process = Start-Process @startParameters
    } finally {
        foreach ($key in $Environment.Keys) {
            [Environment]::SetEnvironmentVariable($key, $previous[$key], "Process")
        }
    }
    return $process
}

function Stop-ProcessTree {
    param([System.Diagnostics.Process]$Process)
    if ($null -eq $Process) { return }
    try { $Process.Refresh() } catch { return }
    if ($Process.HasExited) { return }
    if ($RunningOnWindows -and (Get-Command taskkill.exe -ErrorAction SilentlyContinue)) {
        & taskkill.exe /PID $Process.Id /T /F | Out-Null
    } else {
        Stop-Process -Id $Process.Id -Force -ErrorAction SilentlyContinue
    }
    try { $Process.WaitForExit(5000) | Out-Null } catch {}
}

function Wait-HttpJson {
    param(
        [Parameter(Mandatory)] [string]$Uri,
        [System.Diagnostics.Process]$Process,
        [int]$Attempts = 80,
        [int]$DelayMilliseconds = 250
    )
    $lastError = $null
    for ($attempt = 1; $attempt -le $Attempts; $attempt++) {
        if ($null -ne $Process) {
            $Process.Refresh()
            if ($Process.HasExited) {
                throw "El proceso terminó antes de responder $Uri. Revisa los logs en $ArtifactRoot."
            }
        }
        try {
            return Invoke-RestMethod -Method Get -Uri $Uri -TimeoutSec 2
        } catch {
            $lastError = $_
            Start-Sleep -Milliseconds $DelayMilliseconds
        }
    }
    throw "No hubo respuesta de $Uri. Último error: $($lastError.Exception.Message)"
}

function Wait-Condition {
    param(
        [Parameter(Mandatory)] [scriptblock]$Condition,
        [Parameter(Mandatory)] [string]$FailureMessage,
        [int]$Attempts = 80,
        [int]$DelayMilliseconds = 250
    )
    $lastError = $null
    for ($attempt = 1; $attempt -le $Attempts; $attempt++) {
        try {
            if (& $Condition) { return }
        } catch {
            $lastError = $_
        }
        Start-Sleep -Milliseconds $DelayMilliseconds
    }
    if ($null -ne $lastError) {
        throw "$FailureMessage Último error: $($lastError.Exception.Message)"
    }
    throw $FailureMessage
}

function Invoke-JsonPost {
    param(
        [Parameter(Mandatory)] [string]$Uri,
        [Parameter(Mandatory)] $Body,
        [hashtable]$Headers = @{}
    )
    return Invoke-RestMethod `
        -Method Post `
        -Uri $Uri `
        -ContentType "application/json" `
        -Headers $Headers `
        -Body ($Body | ConvertTo-Json -Depth 20 -Compress) `
        -TimeoutSec 10
}

function Invoke-Psql {
    param([Parameter(Mandatory)] [string]$Sql)
    $output = & psql $DatabaseUrl -v ON_ERROR_STOP=1 -Atq -c $Sql
    if ($LASTEXITCODE -ne 0) { throw "psql falló al ejecutar SQL." }
    return ($output | Out-String).Trim()
}

function Start-Api {
    $environment = @{
        APP_ENV = "e2e"
        HTTP_ADDR = "127.0.0.1:$ApiPort"
        DATABASE_URL = $DatabaseUrl
        INGEST_BEARER_TOKEN = $IngestToken
        INGEST_BEARER_TOKEN_PREVIOUS = ""
        MAX_INGEST_BODY_BYTES = "5242880"
        READ_TIMEOUT = "5s"
        WRITE_TIMEOUT = "10s"
        IDLE_TIMEOUT = "30s"
        LOG_COLOR = "never"
    }
    $script:ApiProcess = Start-LoggedProcess -Name "api" -WorkingDirectory $ApiWorkDirectory -FilePath $ApiBinary -Arguments @() -Environment $environment
    Wait-HttpJson -Uri "$ApiBaseUrl/healthz" -Process $script:ApiProcess | Out-Null
}

function Start-Receiver {
    param(
        [string]$Token = $IngestToken,
        [int]$MaxDeliveryAttempts = 20
    )
    $environment = @{
        ALBION_SERVER = "west"
        APP_ENV = "e2e"
        LOG_COLOR = "never"
        COLLECTOR_DATA_DIR = $ReceiverData
        COLLECTOR_CATALOG_DIR = (Join-Path $PlatformRepo "catalog")
        COLLECTOR_LISTEN = "127.0.0.1:$ReceiverPort"
        LOCAL_DATABASE_PATH = (Join-Path $ReceiverData "database/market-state.json")
        UPSTREAM_ENABLED = "true"
        UPSTREAM_HISTORY_ENABLED = "true"
        UPSTREAM_BASE_URL = $ApiBaseUrl
        UPSTREAM_TOKEN = $Token
        UPSTREAM_BATCH_SIZE = "2"
        UPSTREAM_FLUSH_INTERVAL = "50ms"
        UPSTREAM_QUEUE_SIZE = "100"
        UPSTREAM_HISTORY_BATCH_SIZE = "2"
        UPSTREAM_HISTORY_MAX_BATCH_BUCKETS = "1000"
        UPSTREAM_HISTORY_FLUSH_INTERVAL = "50ms"
        UPSTREAM_HISTORY_QUEUE_SIZE = "100"
        UPSTREAM_OUTBOX_PATH = (Join-Path $ReceiverData "outbox/state.json")
        UPSTREAM_RETRY_COUNT = "1"
        UPSTREAM_RETRY_DELAY = "100ms"
        UPSTREAM_MAX_DELIVERY_ATTEMPTS = [string]$MaxDeliveryAttempts
        UPSTREAM_MAX_RETRY_DELAY = "500ms"
        UPSTREAM_TIMEOUT = "2s"
        UPSTREAM_GZIP = "false"
    }
    $script:ReceiverProcess = Start-LoggedProcess -Name "receiver" -WorkingDirectory $RuntimeRoot -FilePath $ReceiverBinary -Arguments @() -Environment $environment
    Wait-HttpJson -Uri "$LocalApiUrl/status" -Process $script:ReceiverProcess | Out-Null
}

function Stop-Api {
    Stop-ProcessTree $script:ApiProcess
    $script:ApiProcess = $null
}

function Stop-Receiver {
    Stop-ProcessTree $script:ReceiverProcess
    $script:ReceiverProcess = $null
}

function Assert-ApiUnavailable {
    try {
        Invoke-RestMethod -Method Get -Uri "$ApiBaseUrl/healthz" -TimeoutSec 1 | Out-Null
        throw "La API central aún responde."
    } catch {
        if ($_.Exception.Message -eq "La API central aún responde.") { throw }
    }
}

function Invoke-FrontendLiveStage {
    param([Parameter(Mandatory)] [ValidateSet("online", "local", "cache")] [string]$Stage)
    $bucketDate = (Get-Date).ToUniversalTime().Date.AddDays(-1)
    Invoke-External -WorkingDirectory $CraftRepo -FilePath "pnpm" -Arguments @("test:e2e:live") -Environment @{
        VITE_RUN_LIVE_E2E = "1"
        VITE_E2E_STAGE = $Stage
        VITE_E2E_RANGE_START = $bucketDate.AddDays(-1).ToString("yyyy-MM-dd")
        VITE_E2E_RANGE_END = $bucketDate.AddDays(1).ToString("yyyy-MM-dd")
        VITE_CENTRAL_MARKET_API_URL = $CentralApiUrl
        VITE_LOCAL_MARKET_API_URL = $LocalApiUrl
    }
}

function Get-OutboxActivity {
    param($Forwarder)
    return [int]$Forwarder.outbox.pending_entries +
        [int]$Forwarder.outbox.pending_batches +
        [int]$Forwarder.outbox.retrying_batches +
        [int]$Forwarder.outbox.processing_batches
}

function Write-Evidence {
    $jsonPath = Join-Path $ArtifactRoot "results.json"
    $markdownPath = Join-Path $ArtifactRoot "RESULTS.md"
    $Results | ConvertTo-Json -Depth 8 | Set-Content -Path $jsonPath -Encoding UTF8

    $passed = @($Results | Where-Object status -eq "passed").Count
    $failed = @($Results | Where-Object status -eq "failed").Count
    $lines = [System.Collections.Generic.List[string]]::new()
    $lines.Add("# Resultado E2E de los tres proyectos")
    $lines.Add("")
    $lines.Add("- Ejecución UTC: $((Get-Date).ToUniversalTime().ToString('o'))")
    $lines.Add("- API: $ApiBaseUrl")
    $lines.Add("- Receiver: $ReceiverBaseUrl")
    $lines.Add("- Base dedicada: $databaseName")
    $lines.Add("- Aprobados: $passed")
    $lines.Add("- Fallidos: $failed")
    $lines.Add("")
    $lines.Add("| ID | Escenario | Estado | Segundos | Detalle |")
    $lines.Add("|---|---|---:|---:|---|")
    foreach ($result in $Results) {
        $detail = ([string]$result.detail).Replace("|", "\\|").Replace("`r", " ").Replace("`n", " ")
        $lines.Add("| $($result.id) | $($result.name) | $($result.status) | $($result.duration_seconds) | $detail |")
    }
    $lines.Add("")
    $lines.Add("Los logs completos de API y receiver están en esta misma carpeta.")
    $lines | Set-Content -Path $markdownPath -Encoding UTF8
}

$bucketDate = (Get-Date).ToUniversalTime().Date.AddDays(-1)
$bucketTicks = [uint64]$bucketDate.Ticks
$expires = (Get-Date).ToUniversalTime().AddDays(30).ToString("o")
$authHeaders = @{ Authorization = "Bearer $IngestToken" }

$ordersSeed = @{
    Orders = @(
        @{ Id = 910000001; ItemTypeId = "T4_BAG"; ItemGroupTypeId = "T4_BAG"; LocationId = "4002"; QualityLevel = 1; EnchantmentLevel = 0; UnitPriceSilver = 125000000; Amount = 10; AuctionType = "offer"; Expires = $expires },
        @{ Id = 910000002; ItemTypeId = "T4_BAG"; ItemGroupTypeId = "T4_BAG"; LocationId = "4002"; QualityLevel = 1; EnchantmentLevel = 0; UnitPriceSilver = 110000000; Amount = 10; AuctionType = "request"; Expires = $expires },
        @{ Id = 910000003; ItemTypeId = "T5_BAG"; ItemGroupTypeId = "T5_BAG"; LocationId = "4002"; QualityLevel = 1; EnchantmentLevel = 0; UnitPriceSilver = 225000000; Amount = 10; AuctionType = "offer"; Expires = $expires },
        @{ Id = 910000004; ItemTypeId = "T5_BAG"; ItemGroupTypeId = "T5_BAG"; LocationId = "4002"; QualityLevel = 1; EnchantmentLevel = 0; UnitPriceSilver = 205000000; Amount = 10; AuctionType = "request"; Expires = $expires }
    )
}
$historyT4 = @{
    AlbionId = 2832
    LocationId = "4002"
    QualityLevel = 1
    Timescale = 2
    MarketHistories = @(@{ ItemAmount = 12; SilverAmount = [uint64](4500L * 12L * 10000L); Timestamp = $bucketTicks })
}
$historyT5 = @{
    AlbionId = 2837
    LocationId = "4002"
    QualityLevel = 1
    Timescale = 2
    MarketHistories = @(@{ ItemAmount = 9; SilverAmount = [uint64](8500L * 9L * 10000L); Timestamp = $bucketTicks })
}
$priceQuery = @{
    server = "west"
    marketKeys = @("fort_sterling")
    entries = @(
        @{ itemIdentifier = "T4_BAG"; quality = 1 },
        @{ itemIdentifier = "T5_BAG"; quality = 1 }
    )
}
$historyQuery = @{
    server = "west"
    marketKeys = @("fort_sterling")
    entries = @(
        @{ itemIdentifier = "T4_BAG"; quality = 1 },
        @{ itemIdentifier = "T5_BAG"; quality = 1 }
    )
    rangeStart = $bucketDate.AddDays(-1).ToString("yyyy-MM-dd")
    rangeEnd = $bucketDate.AddDays(1).ToString("yyyy-MM-dd")
}

try {
    Invoke-Scenario "E2E-00" "Precondiciones y herramientas" {
        Assert-Command "go"
        Assert-Command "pnpm"
        Assert-Command "psql"
        Assert-True (Test-Path (Join-Path $CraftRepo "package.json")) "No se encontró package.json en $CraftRepo"
        Assert-True (Test-Path (Join-Path $ApiRepo "go.mod")) "No se encontró go.mod en $ApiRepo"
        Assert-True (Test-Path (Join-Path $PlatformRepo "apps/collector/go.mod")) "No se encontró el módulo collector."
        Invoke-Psql "select current_database();" | Out-Null
    }

    if (-not $SkipQuality) {
        Invoke-Scenario "E2E-01" "Puertas de calidad de los tres repositorios" {
            Invoke-External -WorkingDirectory $CraftRepo -FilePath "pnpm" -Arguments @("install", "--frozen-lockfile")
            Invoke-External -WorkingDirectory $CraftRepo -FilePath "pnpm" -Arguments @("test")
            Invoke-External -WorkingDirectory $CraftRepo -FilePath "pnpm" -Arguments @("lint")
            Invoke-External -WorkingDirectory $CraftRepo -FilePath "pnpm" -Arguments @("build")
            Invoke-External -WorkingDirectory $CraftRepo -FilePath "pnpm" -Arguments @("docs:build")
            Invoke-External -WorkingDirectory $ApiRepo -FilePath "go" -Arguments @("test", "./...")
            Invoke-External -WorkingDirectory (Join-Path $PlatformRepo "apps/collector") -FilePath "go" -Arguments @("test", "./...")
        }
    }

    Invoke-Scenario "E2E-02" "Migraciones y base de datos aislada" {
        Get-ChildItem (Join-Path $ApiRepo "migrations") -Filter "*.sql" | Sort-Object Name | ForEach-Object {
            & psql $DatabaseUrl -v ON_ERROR_STOP=1 -f $_.FullName | Out-Null
            if ($LASTEXITCODE -ne 0) { throw "Falló la migración $($_.Name)." }
        }
        Invoke-Psql @"
truncate table
  market_history_ingest_raw,
  market_history_ingest_requests,
  market_history_buckets,
  market_ingest_raw,
  market_ingest_requests,
  current_market_prices
restart identity cascade;
"@ | Out-Null
    }

    Invoke-Scenario "E2E-03" "Build y arranque aislado de API central y receiver" {
        Invoke-External -WorkingDirectory $ApiRepo -FilePath "go" -Arguments @("build", "-o", $ApiBinary, "./cmd/api")
        Invoke-External -WorkingDirectory (Join-Path $PlatformRepo "apps/collector") -FilePath "go" -Arguments @("build", "-o", $ReceiverBinary, "./cmd/receiver")
        Start-Api
        Start-Receiver
        $apiStatus = Wait-HttpJson "$CentralApiUrl/status" $ApiProcess
        $receiverStatus = Wait-HttpJson "$LocalApiUrl/status" $ReceiverProcess
        Assert-True ($apiStatus.status -eq "ok") "La API no quedó saludable."
        Assert-True ([bool]$receiverStatus.price_forwarder.enabled) "El forwarder de precios no quedó habilitado."
        Assert-True ([bool]$receiverStatus.history_forwarder.enabled) "El forwarder histórico no quedó habilitado."
    }

    Invoke-Scenario "E2E-04" "Captura receiver → API → PostgreSQL" {
        $ordersResponse = Invoke-JsonPost "$ReceiverBaseUrl/marketorders.ingest" $ordersSeed
        Assert-True ([int]$ordersResponse.forwarded -eq 2) "Se esperaban dos snapshots de precio encolados."
        $h4Response = Invoke-JsonPost "$ReceiverBaseUrl/markethistories.ingest" $historyT4
        $h5Response = Invoke-JsonPost "$ReceiverBaseUrl/markethistories.ingest" $historyT5
        Assert-True ([int]$h4Response.forwarded -eq 1 -and [int]$h5Response.forwarded -eq 1) "Las capturas históricas no se encolaron."

        Wait-Condition -FailureMessage "Los precios no aparecieron en la API central." -Condition {
            $response = Invoke-JsonPost "$CentralApiUrl/prices/query" $priceQuery
            return [int]$response.count -eq 2
        }
        Wait-Condition -FailureMessage "El historial no apareció en la API central." -Condition {
            $response = Invoke-JsonPost "$CentralApiUrl/history/query" $historyQuery
            return [int]$response.count -eq 2 -and [int]$response.bucketCount -eq 2
        }

        $rawPrices = [int](Invoke-Psql "select count(*) from market_ingest_raw;")
        $currentPrices = [int](Invoke-Psql "select count(*) from current_market_prices;")
        $rawHistory = [int](Invoke-Psql "select count(*) from market_history_ingest_raw;")
        $historyBuckets = [int](Invoke-Psql "select count(*) from market_history_buckets;")
        Assert-True ($rawPrices -eq 2) "market_ingest_raw debía contener 2 filas y contiene $rawPrices."
        Assert-True ($currentPrices -eq 2) "current_market_prices debía contener 2 filas y contiene $currentPrices."
        Assert-True ($rawHistory -eq 2) "market_history_ingest_raw debía contener 2 filas y contiene $rawHistory."
        Assert-True ($historyBuckets -eq 2) "market_history_buckets debía contener 2 filas y contiene $historyBuckets."
    }

    Invoke-Scenario "E2E-05" "Frontend usa API central y batching múltiple" {
        Invoke-FrontendLiveStage "online"
        $publicPrices = Invoke-JsonPost "$CentralApiUrl/prices/query" $priceQuery
        $publicHistory = Invoke-JsonPost "$CentralApiUrl/history/query" $historyQuery
        $serialized = @($publicPrices, $publicHistory) | ConvertTo-Json -Depth 20 -Compress
        Assert-True ($serialized -notmatch 'location_id|locationId') "El contrato público filtró un identificador numérico de ubicación."

        $productionFiles = Get-ChildItem (Join-Path $CraftRepo "src") -Recurse -File -Include *.ts,*.tsx |
            Where-Object { $_.Name -notmatch '\.test\.' }
        $forbidden = $productionFiles | Select-String -Pattern 'location_id|locationId' -CaseSensitive:$false
        Assert-True (@($forbidden).Count -eq 0) "El código productivo React contiene location_id/locationId."
    }

    Invoke-Scenario "E2E-06" "Idempotencia de batches centrales" {
        $observedAt = (Get-Date).ToUniversalTime().ToString("o")
        $priceRequestId = "11111111-1111-4111-8111-111111111111"
        $priceBatch = @{
            request_id = $priceRequestId
            server = "west"
            entries = @(@{
                observed_at = $observedAt
                location_id = 4002
                item_key = "T4_BAG"
                quality = 1
                sell_price_min = 13000
                sell_price_min_at = $observedAt
                buy_price_max = 11500
                buy_price_max_at = $observedAt
            })
        }
        $firstPrice = Invoke-JsonPost "$CentralApiUrl/ingest/prices" $priceBatch $authHeaders
        $secondPrice = Invoke-JsonPost "$CentralApiUrl/ingest/prices" $priceBatch $authHeaders
        Assert-True (-not [bool]$firstPrice.duplicate) "El primer batch de precio fue marcado como duplicado."
        Assert-True ([bool]$secondPrice.duplicate) "El segundo batch de precio no fue marcado como duplicado."
        $priceRawCount = [int](Invoke-Psql "select count(*) from market_ingest_raw where request_id = '$priceRequestId';")
        Assert-True ($priceRawCount -eq 1) "El batch de precio duplicó filas raw."
        $logicalPrices = [int](Invoke-Psql "select count(*) from current_market_prices where server=1 and location_id=4002 and item_key='T4_BAG' and quality=1;")
        Assert-True ($logicalPrices -eq 1) "El precio lógico quedó duplicado en current_market_prices."

        $historyRequestId = "22222222-2222-4222-8222-222222222222"
        $historyBatch = @{
            request_id = $historyRequestId
            server = "west"
            entries = @(@{
                observed_at = $observedAt
                location_id = 4002
                item_key = "T4_BAG"
                quality = 1
                history = @(@{
                    timestamp = $bucketDate.ToString("o")
                    item_count = 12
                    average_unit_price = 4500
                })
            })
        }
        $firstHistory = Invoke-JsonPost "$CentralApiUrl/ingest/history" $historyBatch $authHeaders
        $secondHistory = Invoke-JsonPost "$CentralApiUrl/ingest/history" $historyBatch $authHeaders
        Assert-True (-not [bool]$firstHistory.duplicate) "El primer batch histórico fue marcado como duplicado."
        Assert-True ([bool]$secondHistory.duplicate) "El segundo batch histórico no fue marcado como duplicado."
        $historyRawCount = [int](Invoke-Psql "select count(*) from market_history_ingest_raw where request_id = '$historyRequestId';")
        Assert-True ($historyRawCount -eq 1) "El batch histórico duplicó filas raw."
        $logicalBuckets = [int](Invoke-Psql "select count(*) from market_history_buckets where location_id=4002 and item_key='T4_BAG' and quality=1 and bucket_at='$($bucketDate.ToString('o'))';")
        Assert-True ($logicalBuckets -eq 1) "El bucket lógico quedó duplicado."
    }

    Invoke-Scenario "E2E-07" "Una captura más nueva corrige un bucket" {
        $newObservedAt = (Get-Date).ToUniversalTime().AddMinutes(1).ToString("o")
        $correction = @{
            request_id = "33333333-3333-4333-8333-333333333333"
            server = "west"
            entries = @(@{
                observed_at = $newObservedAt
                location_id = 4002
                item_key = "T4_BAG"
                quality = 1
                history = @(@{
                    timestamp = $bucketDate.ToString("o")
                    item_count = 17
                    average_unit_price = 4999
                })
            })
        }
        $result = Invoke-JsonPost "$CentralApiUrl/ingest/history" $correction $authHeaders
        Assert-True ([int]$result.history_rows_touched -eq 1) "La corrección no tocó el bucket esperado."
        $corrected = Invoke-JsonPost "$CentralApiUrl/history/query" $historyQuery
        $series = @($corrected.data | Where-Object itemIdentifier -eq "T4_BAG")[0]
        $point = @($series.history)[0]
        Assert-True ([int]$point.itemCount -eq 17) "La cantidad corregida no se publicó."
        Assert-True ([int]$point.averageUnitPrice -eq 4999) "El precio corregido no se publicó."
    }

    Invoke-Scenario "E2E-08" "Fallback real del frontend al receiver local" {
        Stop-Api
        Assert-ApiUnavailable
        Invoke-FrontendLiveStage "local"
    }

    Invoke-Scenario "E2E-09" "Outbox persiste durante caída y reinicio" {
        $outageOrders = @{
            Orders = @(
                @{ Id = 920000001; ItemTypeId = "T4_BAG"; ItemGroupTypeId = "T4_BAG"; LocationId = "4002"; QualityLevel = 1; EnchantmentLevel = 0; UnitPriceSilver = 124000000; Amount = 5; AuctionType = "offer"; Expires = $expires },
                @{ Id = 920000002; ItemTypeId = "T4_BAG"; ItemGroupTypeId = "T4_BAG"; LocationId = "4002"; QualityLevel = 1; EnchantmentLevel = 0; UnitPriceSilver = 112000000; Amount = 5; AuctionType = "request"; Expires = $expires }
            )
        }
        $outageHistory = @{
            AlbionId = 2837
            LocationId = "4002"
            QualityLevel = 1
            Timescale = 2
            MarketHistories = @(@{ ItemAmount = 15; SilverAmount = [uint64](8700L * 15L * 10000L); Timestamp = $bucketTicks })
        }
        Invoke-JsonPost "$ReceiverBaseUrl/marketorders.ingest" $outageOrders | Out-Null
        Invoke-JsonPost "$ReceiverBaseUrl/markethistories.ingest" $outageHistory | Out-Null

        Wait-Condition -FailureMessage "No aparecieron pendientes de precio e historial en la outbox." -Condition {
            $status = Wait-HttpJson "$LocalApiUrl/status" $ReceiverProcess 2 100
            return (Get-OutboxActivity $status.price_forwarder) -gt 0 -and (Get-OutboxActivity $status.history_forwarder) -gt 0
        }
        $beforeRestart = Wait-HttpJson "$LocalApiUrl/status" $ReceiverProcess
        $beforePrice = Get-OutboxActivity $beforeRestart.price_forwarder
        $beforeHistory = Get-OutboxActivity $beforeRestart.history_forwarder
        Stop-Receiver
        Start-Receiver
        $afterRestart = Wait-HttpJson "$LocalApiUrl/status" $ReceiverProcess
        Assert-True ((Get-OutboxActivity $afterRestart.price_forwarder) -gt 0) "El pendiente de precios no sobrevivió al reinicio."
        Assert-True ((Get-OutboxActivity $afterRestart.history_forwarder) -gt 0) "El pendiente histórico no sobrevivió al reinicio."
        Assert-True ($beforePrice -gt 0 -and $beforeHistory -gt 0) "El estado previo no tenía pendientes válidos."

        Start-Api
        Wait-Condition -Attempts 120 -DelayMilliseconds 250 -FailureMessage "La outbox no se vació al recuperar la API central." -Condition {
            $status = Wait-HttpJson "$LocalApiUrl/status" $ReceiverProcess 2 100
            return (Get-OutboxActivity $status.price_forwarder) -eq 0 -and (Get-OutboxActivity $status.history_forwarder) -eq 0
        }
        $recovered = Wait-HttpJson "$LocalApiUrl/status" $ReceiverProcess
        Assert-True ([int]$recovered.price_forwarder.totals.batches_sent -ge 1) "El batch pendiente de precios no se envió después de recuperar la API."
        Assert-True ([int]$recovered.history_forwarder.totals.batches_sent -ge 1) "El batch histórico pendiente no se envió después de recuperar la API."
        Assert-True ([int]$recovered.price_forwarder.outbox.rescheduled_batches_total -ge 1) "La outbox no registró la reprogramación de precios durante la caída."
        Assert-True ([int]$recovered.history_forwarder.outbox.rescheduled_batches_total -ge 1) "La outbox no registró la reprogramación histórica durante la caída."
    }

    Invoke-Scenario "E2E-10" "Fallback a caché con ambas fuentes apagadas" {
        Stop-Api
        Stop-Receiver
        Assert-ApiUnavailable
        Invoke-FrontendLiveStage "cache"
    }

    Invoke-Scenario "E2E-11" "Errores permanentes terminan en dead_letter" {
        Start-Api
        Start-Receiver -Token "token-e2e-invalido" -MaxDeliveryAttempts 2
        $deadLetterOrders = @{
            Orders = @(
                @{ Id = 930000001; ItemTypeId = "T5_BAG"; ItemGroupTypeId = "T5_BAG"; LocationId = "4002"; QualityLevel = 1; EnchantmentLevel = 0; UnitPriceSilver = 230000000; Amount = 3; AuctionType = "offer"; Expires = $expires },
                @{ Id = 930000002; ItemTypeId = "T5_BAG"; ItemGroupTypeId = "T5_BAG"; LocationId = "4002"; QualityLevel = 1; EnchantmentLevel = 0; UnitPriceSilver = 200000000; Amount = 3; AuctionType = "request"; Expires = $expires }
            )
        }
        $deadLetterHistory = @{
            AlbionId = 2832
            LocationId = "4002"
            QualityLevel = 1
            Timescale = 2
            MarketHistories = @(@{ ItemAmount = 19; SilverAmount = [uint64](5200L * 19L * 10000L); Timestamp = $bucketTicks })
        }
        Invoke-JsonPost "$ReceiverBaseUrl/marketorders.ingest" $deadLetterOrders | Out-Null
        Invoke-JsonPost "$ReceiverBaseUrl/markethistories.ingest" $deadLetterHistory | Out-Null
        Wait-Condition -FailureMessage "Los errores 401 no llegaron a dead_letter." -Condition {
            $status = Wait-HttpJson "$LocalApiUrl/status" $ReceiverProcess 2 100
            return [int]$status.price_forwarder.outbox.dead_letter_batches -ge 1 -and [int]$status.history_forwarder.outbox.dead_letter_batches -ge 1
        }
    }

    Invoke-Scenario "E2E-12" "Estado distingue precios, historial, outbox, reintentos y errores" {
        $apiStatus = Wait-HttpJson "$CentralApiUrl/status" $ApiProcess
        $receiverStatus = Wait-HttpJson "$LocalApiUrl/status" $ReceiverProcess
        Assert-True ($null -ne $apiStatus.database) "El status central no contiene database."
        Assert-True ($null -ne $apiStatus.ingest) "El status central no contiene ingest de precios."
        Assert-True ($null -ne $apiStatus.history_ingest) "El status central no contiene history_ingest."
        Assert-True ($null -ne $receiverStatus.price_forwarder.outbox) "El status local no contiene outbox de precios."
        Assert-True ($null -ne $receiverStatus.history_forwarder.outbox) "El status local no contiene outbox histórica."
        Assert-True ([int]$receiverStatus.price_forwarder.outbox.dead_letter_batches -ge 1) "No se refleja dead_letter de precios."
        Assert-True ([int]$receiverStatus.history_forwarder.outbox.dead_letter_batches -ge 1) "No se refleja dead_letter histórica."
        Assert-True ($null -ne $receiverStatus.price_forwarder.totals.retries) "No se refleja la métrica de reintentos de precios."
        Assert-True ($null -ne $receiverStatus.history_forwarder.totals.retries) "No se refleja la métrica de reintentos históricos."
        Assert-True (-not [string]::IsNullOrWhiteSpace([string]$receiverStatus.price_forwarder.last_error)) "No se refleja el último error del forwarder de precios."
        Assert-True (-not [string]::IsNullOrWhiteSpace([string]$receiverStatus.history_forwarder.last_error)) "No se refleja el último error del forwarder histórico."
    }
} finally {
    Stop-Receiver
    Stop-Api
    Write-Evidence
    Write-Host "`nEvidencia: $ArtifactRoot" -ForegroundColor Yellow
}

$failedCount = @($Results | Where-Object status -eq "failed").Count
if ($failedCount -gt 0) {
    throw "La prueba E2E terminó con $failedCount escenario(s) fallido(s)."
}
Write-Host "`nPrueba end-to-end formal aprobada: $($Results.Count) escenarios." -ForegroundColor Green
