[CmdletBinding()]
param(
    [ValidateSet("list", "requeue", "purge")]
    [string]$Action = "list",
    [string]$Pipeline = "",
    [string]$RequestId = "",
    [string]$Path = ".\data\outbox\state.json"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
$nativeTool = Join-Path $root "tools\albion-market-outboxctl.exe"
$sourceTool = Join-Path $root "apps\collector\cmd\outboxctl"

$argsList = @(
    "--path", $Path,
    "--action", $Action
)
if ($Pipeline) { $argsList += @("--pipeline", $Pipeline) }
if ($RequestId) { $argsList += @("--request-id", $RequestId) }

Push-Location $root
try {
    if (Test-Path $nativeTool) {
        & $nativeTool @argsList
    } elseif (Test-Path $sourceTool) {
        $goArgs = @("run", "./apps/collector/cmd/outboxctl") + $argsList
        & go @goArgs
    } else {
        throw "No se encontró albion-market-outboxctl.exe ni el código fuente de outboxctl."
    }
    if ($LASTEXITCODE -ne 0) {
        throw "outboxctl terminó con código $LASTEXITCODE."
    }
}
finally {
    Pop-Location
}
