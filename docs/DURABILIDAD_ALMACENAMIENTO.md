# Durabilidad del almacenamiento local

El receiver mantiene eventos raw, auditoría normalizada, una base embebida de lectura y el outbox persistente. Los JSONL normalizados son la fuente durable desde la que puede reconstruirse el modelo local.

## Arranque protegido

Antes de abrir el servidor HTTP, el proceso:

- obtiene una exclusión asociada al directorio `data`;
- aplica la política de retención;
- verifica el presupuesto configurado;
- revisa la última línea de los JSONL;
- recupera la base local o el outbox desde una copia `.bak` válida cuando corresponde.

Una segunda instancia que use el mismo directorio se rechaza. El archivo `data/.receiver.lock` conserva PID, host, fecha de inicio y el puerto loopback de coordinación.

## Recuperación JSONL

Cada append se sincroniza antes del cierre. Durante el siguiente inicio:

- una última línea JSON válida sin salto final se completa;
- un fragmento final incompleto se conserva con sufijo `.truncated-<timestamp>` y se retira del archivo activo;
- una corrupción interior detiene el inicio para no ocultar daño histórico.

## Estado JSON

Las escrituras atómicas usan un temporal en el mismo directorio, sincronización, rotación del estado previo a `.bak` y rename final. Al arrancar se revisan:

```text
data/database/market-state.json
data/database/market-state.json.bak
data/outbox/state.json
data/outbox/state.json.bak
```

Si el primario no es JSON válido y la copia sí lo es, el primario queda en cuarentena con sufijo `.corrupt-<timestamp>` y se restaura la copia. La base embebida vuelve a importar la auditoría normalizada durante el arranque.

## Retención y límite

```env
STORAGE_MAX_BYTES=10737418240
STORAGE_RAW_RETENTION_DAYS=30
STORAGE_NORMALIZED_RETENTION_DAYS=365
STORAGE_BACKUP_DIR=./backups
STORAGE_BACKUP_RETENTION_DAYS=30
STORAGE_MINIMUM_BACKUPS=3
```

La retención se aplica a archivos diarios raw, normalizados y backups antiguos, conservando siempre el mínimo configurado de backups. El outbox y los dead-letter no se descartan automáticamente; requieren una decisión operativa explícita.

Si el directorio ya supera `STORAGE_MAX_BYTES`, el receiver no arranca. Si un append JSONL proyecta superar el límite, la escritura se rechaza sin dañar el contenido existente.

## Backup

Detén el receiver con `Ctrl+C` y ejecuta:

```powershell
.\scripts\backup-local-storage.ps1
```

La herramienta usa la misma exclusión del receiver, por lo que no crea un backup mientras exista una instancia activa. El resultado contiene un ZIP y su archivo `.sha256`. Dentro del ZIP, `manifest.json` registra ruta, tamaño y SHA-256 de cada archivo.

## Verificación

```powershell
.\scripts\verify-local-storage-backup.ps1 `
    -BackupPath .\backups\albion-market-data-YYYYMMDD-HHMMSS.zip
```

Se verifican el checksum del archivo, el manifiesto, las rutas y el contenido de cada entrada.

## Restauración de prueba

Restaura primero en una ubicación nueva:

```powershell
.\scripts\restore-local-storage.ps1 `
    -BackupPath .\backups\albion-market-data-YYYYMMDD-HHMMSS.zip `
    -TargetPath .\data-restored
```

Luego inicia una instancia aislada:

```powershell
go run .\apps\collector\cmd\receiver `
    -data-dir .\data-restored `
    -listen 127.0.0.1:18787 `
    -upstream-enabled=false `
    -upstream-history-enabled=false
```

Comprueba `/healthz`, `/readyz` y `/api/v1/status`. La restauración verifica primero el backup completo, extrae en staging y solo instala el resultado cuando todos los archivos fueron escritos correctamente.

## Validación

```powershell
.\scripts\check.ps1
```

El workflow `Storage durability` repite las pruebas de corrupción, reinicio, exclusión, backup y restauración tanto en Windows como en Linux.
