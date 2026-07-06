# Operación diaria

## Arranque normal

Desde una instalación de release:

```powershell
Set-Location C:\AlbionMarketData
.\albion-market-receiver.exe
```

Desde un checkout de desarrollo:

```powershell
Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass
.\scripts\receiver.ps1
```

## Detención

Usa `Ctrl + C` en la consola del receiver. No cierres Windows a la fuerza mientras haya escrituras intensas si puedes evitarlo.

## Comprobaciones mínimas

```powershell
Invoke-WebRequest http://127.0.0.1:8787/healthz
Invoke-RestMethod http://127.0.0.1:8787/api/v1/status |
    ConvertTo-Json -Depth 10
Invoke-WebRequest http://127.0.0.1:8787/metrics |
    Select-Object -ExpandProperty Content
```

## Rutina recomendada

1. Verifica que la API central esté arriba si usas forwarding.
2. Inicia el receiver.
3. Inicia Albion Data Client con el destino local.
4. Visita mercados.
5. Revisa `/api/v1/status` si algo no aparece.
6. Ejecuta backup antes de limpiezas o restauraciones.

## Archivos importantes

```text
.env
catalog/
data/raw/
data/normalized/
data/database/market-state.json
data/outbox/state.json
backups/
```
