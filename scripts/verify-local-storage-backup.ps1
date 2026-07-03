[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$BackupPath
)

$ErrorActionPreference = "Stop"

$repositoryRoot = Split-Path -Parent $PSScriptRoot
Push-Location $repositoryRoot
try {
    go run .\apps\collector\cmd\storagectl verify `
        -backup $BackupPath
}
finally {
    Pop-Location
}
