[CmdletBinding()]
param(
    [string]$DataPath = ".\data",
    [string]$OutputPath = ".\backups"
)

$ErrorActionPreference = "Stop"

$repositoryRoot = Split-Path -Parent $PSScriptRoot
Push-Location $repositoryRoot
try {
    go run .\apps\collector\cmd\storagectl backup `
        -data $DataPath `
        -output $OutputPath
}
finally {
    Pop-Location
}
