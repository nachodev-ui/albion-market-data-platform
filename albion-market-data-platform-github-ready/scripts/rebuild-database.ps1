param(
    [switch]$IncludeTest
)

$ErrorActionPreference = "Stop"

New-Item -ItemType Directory -Force -Path ./data/database | Out-Null

go run ./apps/collector/cmd/rebuilddb `
    -normalized-dir ./data/normalized `
    -database ./data/database/market-state.json `
    -reset

if ($IncludeTest) {
    New-Item -ItemType Directory -Force -Path ./data/test/database | Out-Null

    go run ./apps/collector/cmd/rebuilddb `
        -normalized-dir ./data/test/normalized `
        -database ./data/test/database/market-state.json `
        -reset
}
