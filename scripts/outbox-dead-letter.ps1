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

$argsList = @(
    "run", "./apps/collector/cmd/outboxctl",
    "--path", $Path,
    "--action", $Action
)
if ($Pipeline) { $argsList += @("--pipeline", $Pipeline) }
if ($RequestId) { $argsList += @("--request-id", $RequestId) }

& go @argsList
if ($LASTEXITCODE -ne 0) {
    throw "outboxctl terminó con código $LASTEXITCODE."
}
