$ErrorActionPreference = "Stop"

New-Item -ItemType Directory -Force -Path ./data/test/normalized | Out-Null

go run ./apps/collector/cmd/collector `
    -input ./samples/market-history.json `
    -data-dir ./data/test/normalized `
    -catalog-dir ./catalog
