# Actualización de versión

## Actualizar desde GitHub Release

1. Detén el receiver.
2. Crea backup.
3. Descarga el nuevo zip.
4. Extrae en una carpeta nueva.
5. Copia `.env`, `secrets/` y `data/` desde la instalación anterior.
6. Ejecuta `--version`.
7. Inicia el receiver.
8. Valida `/api/v1/status`.

```powershell
Expand-Archive .\albion-market-data-platform-vX.Y.Z-windows-amd64.zip `
    C:\AlbionMarketData-vX.Y.Z

Copy-Item C:\AlbionMarketData\.env C:\AlbionMarketData-vX.Y.Z\.env
Copy-Item C:\AlbionMarketData\secrets C:\AlbionMarketData-vX.Y.Z\secrets -Recurse
Copy-Item C:\AlbionMarketData\data C:\AlbionMarketData-vX.Y.Z\data -Recurse

Set-Location C:\AlbionMarketData-vX.Y.Z
.\albion-market-receiver.exe --version
```

## Validación post-actualización

```powershell
Invoke-WebRequest http://127.0.0.1:8787/healthz
Invoke-RestMethod http://127.0.0.1:8787/api/v1/status |
    ConvertTo-Json -Depth 10
.\scripts\verify-api.ps1
```

## Actualizar checkout local

```bash
git fetch --all --prune
git switch main
git pull --ff-only origin main
```
