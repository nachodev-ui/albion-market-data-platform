# Backup y restauración

## Regla principal

Detén el receiver antes de crear o restaurar backups. El storage usa lock para evitar copiar datos mientras el proceso escribe.

## Backup desde paquete release

```powershell
Set-Location C:\AlbionMarketData
.\tools\albion-market-storagectl.exe backup `
    -data .\data `
    -output .\backups
```

## Backup desde checkout de desarrollo

```powershell
go run .\apps\collector\cmd\storagectl backup `
    -data .\data `
    -output .\backups
```

## Verificar backup

```powershell
.\tools\albion-market-storagectl.exe verify `
    -backup .\backups\NOMBRE_DEL_BACKUP.zip
```

## Restaurar

Restaurar sobrescribe el destino. Hazlo con el receiver detenido.

```powershell
.\tools\albion-market-storagectl.exe restore `
    -backup .\backups\NOMBRE_DEL_BACKUP.zip `
    -target .\data `
    -force
```

Después valida:

```powershell
.\albion-market-receiver.exe --version
Invoke-RestMethod http://127.0.0.1:8787/api/v1/status |
    ConvertTo-Json -Depth 10
```

## Qué respaldar

- `data/raw/`
- `data/normalized/`
- `data/database/`
- `data/outbox/`
- `catalog/` si hubo cambios locales
- `.env` y `secrets/` por un canal seguro separado

No subas backups, `.env` ni tokens al repositorio.
