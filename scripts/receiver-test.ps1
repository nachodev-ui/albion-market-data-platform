$ErrorActionPreference = "Stop"

New-Item -ItemType Directory -Force -Path ./data/test/raw | Out-Null
New-Item -ItemType Directory -Force -Path ./data/test/normalized | Out-Null
New-Item -ItemType Directory -Force -Path ./data/test/database | Out-Null

go run ./apps/collector/cmd/receiver `
    -listen 127.0.0.1:8788 `
    -data-dir ./data/test `
    -database ./data/test/database/market-state.json `
    -catalog-dir ./catalog `
    -server west
