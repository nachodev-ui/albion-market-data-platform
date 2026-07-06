# Limpieza segura

## No borrar sin revisar

No elimines manualmente:

- `data/outbox/state.json`
- `data/outbox/state.json.bak`
- `secrets/`
- `.env`
- backups recientes

## Limpieza recomendada

1. Detén el receiver.
2. Crea backup.
3. Verifica que no haya pendientes.
4. Limpia solo datos antiguos o respaldados.
5. Reinicia y valida `/api/v1/status`.

```powershell
$status = Invoke-RestMethod http://127.0.0.1:8787/api/v1/status
$status.price_forwarder.outbox
$status.history_forwarder.outbox
```

## Limpieza de normalizados antiguos

El receiver aplica retención según:

```env
STORAGE_RAW_RETENTION_DAYS=30
STORAGE_NORMALIZED_RETENTION_DAYS=365
```

Para limpieza manual, primero mueve a una carpeta temporal en vez de borrar definitivamente.

```powershell
$stamp = Get-Date -Format yyyyMMdd-HHmmss
Move-Item .\data\normalized .\data\normalized-manual-backup-$stamp
New-Item -ItemType Directory -Force .\data\normalized | Out-Null
```

Después reconstruye desde raw si corresponde.

## Backups antiguos

Mantén al menos `STORAGE_MINIMUM_BACKUPS`. Borra backups viejos solo si ya verificaste uno más reciente.
