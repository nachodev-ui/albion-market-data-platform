[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$BackupPath,
    [string]$TargetPath = ".\data",
    [switch]$Force
)

$ErrorActionPreference = "Stop"

$repositoryRoot = Split-Path -Parent $PSScriptRoot
Push-Location $repositoryRoot
try {
    $arguments = @(
        "run",
        ".\apps\collector\cmd\storagectl",
        "restore",
        "-backup", $BackupPath,
        "-target", $TargetPath
    )
    if ($Force) {
        $arguments += "-force"
    }
    & go @arguments
    if ($LASTEXITCODE -ne 0) {
        throw "storage restore failed with exit code $LASTEXITCODE"
    }
}
finally {
    Pop-Location
}
