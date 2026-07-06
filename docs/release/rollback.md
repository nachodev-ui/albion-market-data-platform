# Rollback

## Cuándo hacer rollback

Haz rollback si una actualización rompe arranque, captura, lectura local, outbox o forwarding central.

## Procedimiento recomendado

1. Detén el receiver actual.
2. Conserva evidencia de error.
3. Vuelve a la carpeta de la versión anterior.
4. Usa el mismo `.env`, `secrets/` y `data/`.
5. Ejecuta `--version`.
6. Inicia y valida.

```powershell
Set-Location C:\AlbionMarketData-vANTERIOR
.\albion-market-receiver.exe --version
.\albion-market-receiver.exe
```

En otra consola:

```powershell
Invoke-RestMethod http://127.0.0.1:8787/api/v1/status |
    ConvertTo-Json -Depth 10
```

## Rollback de datos

Si la versión nueva modificó datos y necesitas volver atrás:

```powershell
.\tools\albion-market-storagectl.exe restore `
    -backup .\backups\NOMBRE_DEL_BACKUP.zip `
    -target .\data `
    -force
```

No hagas rollback de `data/outbox` sin entender si hay batches ya enviados o pendientes.
