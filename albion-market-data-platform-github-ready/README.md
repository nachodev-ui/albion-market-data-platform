# Albion Market Data Platform

Servicio local en Go para capturar los paquetes de mercado enviados por Albion
Data Client, conservarlos como auditoría, normalizarlos y servirlos a la
calculadora React sin consultar directamente la API pública de AODP.

## Inicio rápido recomendado

Desde la raíz de `albion-market-data-platform`:

```powershell
Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass
.\scripts\start-session.ps1
```

Este script inicia en cadena:

1. La API central en `127.0.0.1:8080`, si todavía no está activa.
2. El receptor local en `127.0.0.1:8787`.
3. Albion Data Client conectado a AODP y al receptor local.
4. La calculadora React mediante `pnpm dev`.

### Flujo diario

```text
1. Ejecutar start-session.ps1
2. Abrir o iniciar sesión en Albion Online
3. Cambiar de zona si Albion Data Client aún no detectó la ubicación
4. Visitar los mercados y objetos necesarios
5. Revisar ofertas de venta y órdenes de compra
6. Pulsar “Actualizar precios” en la calculadora
```

No es necesario ejecutar `reprocess.ps1 -Rebuild` después de visitar mercados.
Las capturas nuevas actualizan automáticamente:

```text
data/raw
data/normalized
data/database/market-state.json
```

## Scripts sin firma

Cuando Windows bloquee un archivo `.ps1`, ejecuta primero:

```powershell
Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass
```

Luego ejecuta el script en un comando separado:

```powershell
.\scripts\verify-api.ps1
```

La excepción solo afecta a la consola actual y desaparece al cerrarla.

También puedes desbloquear una vez todos los archivos:

```powershell
Get-ChildItem . -Recurse -File | Unblock-File
```

## Cuándo usar una reconstrucción completa

Detén primero el receptor con `Ctrl + C` y ejecuta:

```powershell
.\scripts\reprocess.ps1 -Rebuild
```

Úsalo únicamente cuando:

* cambia `catalog/markets.json`;
* cambia `catalog/items.txt`;
* se modifica la normalización;
* se corrige una asociación de ubicación;
* se actualiza el formato de los datos;
* es necesario recuperar la base desde `data/raw`.

Después vuelve a iniciar:

```powershell
.\scripts\receiver.ps1
```

::: 


## Requisitos

- Go 1.23 o posterior.
- PowerShell en Windows.
- Albion Data Client.

## Primera ejecución

Desde la raíz del proyecto:

```powershell
./scripts/check.ps1
./scripts/rebuild-database.ps1
./scripts/receiver.ps1
```

El receptor queda disponible en `http://127.0.0.1:8787`.

En otra consola inicia Albion Data Client con el receptor local como segundo
destino:

```powershell
& "C:\Program Files\Albion Data Client\albiondata-client.exe" `
  -i "https+pow://albion-online-data.com,http://127.0.0.1:8787"
```

La consola del receptor y la de Albion Data Client deben permanecer abiertas
mientras capturas mercados. Se detienen con `Ctrl + C`; los datos guardados no
se eliminan.

## API local

| Endpoint | Uso |
|---|---|
| `GET /healthz` | Estado básico del receptor |
| `GET /api/v1/status` | Estado del receptor, repositorio y forwarders separados de precios e historial |
| `GET /api/v1/markets` | Catálogo canónico de mercados habilitados |
| `GET /api/v1/prices` | Precio de venta mínimo y compra máxima para varios objetos |
| `GET /api/v1/history` | Historial normalizado de 7 días o 4 semanas |
| `GET /api/v1/orders` | Última versión conocida de las órdenes capturadas |

Ejemplo batch utilizado por la calculadora:

```text
/api/v1/prices?server=west&marketKey=brecilien&itemIds=T4_MAIN_CURSEDSTAFF_CRYSTAL%404,T5_MAIN_CURSEDSTAFF_CRYSTAL%404&quality=4
```

Ejemplo de historial:

```text
/api/v1/history?server=west&marketKey=brecilien&itemId=T4_MAIN_CURSEDSTAFF_CRYSTAL%404&quality=4&period=4-weeks&limit=1
```

Puedes validar una captura incluida con:

```powershell
./scripts/verify-api.ps1
```



## Prueba end-to-end formal

La validación integrada de receiver, outbox, API central, PostgreSQL y frontend
se ejecuta con una base dedicada:

```powershell
.\scripts\e2e-three-projects.ps1 `
  -DatabaseUrl "postgres://postgres:TU_CLAVE@localhost:5432/albion_market_e2e?sslmode=disable"
```

El arnés usa puertos aislados, prueba fallbacks, idempotencia, corrección de
buckets, recuperación de outbox y dead-letter, y genera evidencia en
`.e2e/artifacts`. Consulta `docs/E2E_TRES_PROYECTOS.md`.

## Observabilidad del receiver y forwarder

La consola usa eventos estructurados y niveles visuales:

```text
[OK   ] ingest.orders_completed received=50 stored=50 duplicates=0 forwarded=12 dropped=0
[OK   ] ingest.history_completed item_key="..." buckets=68 forwarded=1 forwarded_buckets=68 dropped=0
[RETRY] upstream.retry_scheduled request_id="..." attempt=1 next_attempt=2 http_status=503 retry_in_ms=500
[RETRY] upstream.history_retry_scheduled request_id="..." attempt=1 next_attempt=2 buckets=68 http_status=503
[OK   ] upstream.history_batch_recovered request_id="..." entries=1 buckets=68 attempts=2
[DROP ] upstream.history_queue_drop reason="outbox_full" dropped_total=1 item_key="..." buckets=68
[RETRY] upstream.history_batch_persisted_for_retry request_id="..." attempts_total=3
[DROP ] upstream.history_batch_dead_lettered request_id="..." attempts_total=12
```

Los colores se controlan con `LOG_COLOR=auto`, `always` o `never`. En Windows,
el modo `auto` habilita Virtual Terminal Processing cuando la consola lo admite;
si no lo admite, muestra etiquetas limpias sin imprimir secuencias ANSI.

`GET /api/v1/status` incluye:

- tiempo activo del servicio;
- cantidad de snapshots del repositorio local;
- `price_forwarder` y `history_forwarder` por separado;
- profundidad, capacidad y máximo observado de cada outbox;
- pendientes, batches en retry, batches en procesamiento y dead-letter;
- antigüedad del pendiente más antiguo;
- batches, capturas, entradas y buckets enviados;
- reintentos, recuperaciones, reprogramaciones y dead-letter;
- latencia del último batch, promedio y máxima;
- último envío exitoso y último error upstream.

`forwarder` se conserva como alias de `price_forwarder` para compatibilidad.

Consulta rápida:

```powershell
Invoke-RestMethod http://127.0.0.1:8787/api/v1/status |
    ConvertTo-Json -Depth 10
```

La documentación detallada está en `docs/OBSERVABILIDAD.md` y `docs/FORWARDER_HISTORICO.md`.


## Forwarder histórico central

Las capturas `markethistories.ingest` se guardan primero en el receiver y luego
se encolan hacia:

```http
POST http://127.0.0.1:8080/api/v1/ingest/history
```

Configuración mínima:

```env
UPSTREAM_ENABLED=true
UPSTREAM_HISTORY_ENABLED=true
UPSTREAM_BASE_URL=http://127.0.0.1:8080
UPSTREAM_TOKEN=EL_MISMO_TOKEN_DE_ALBION_MARKET_API
```

Prueba end-to-end:

```powershell
.\scripts\verify-history-forwarder.ps1
```

La guía completa está en `docs/FORWARDER_HISTORICO.md`.

## Outbox persistente y recuperación

Los forwarders de precios e historial comparten una outbox durable en:

```text
data/outbox/state.json
```

Cada captura se escribe en disco antes de considerarse encolada. Si la API
central está caída, el receiver se cierra o Windows se reinicia, los batches
pendientes se recuperan automáticamente con el mismo `request_id`.

Configuración recomendada:

```env
UPSTREAM_OUTBOX_PATH=./data/outbox/state.json
UPSTREAM_MAX_DELIVERY_ATTEMPTS=12
UPSTREAM_MAX_RETRY_DELAY=5m
```

Errores transitorios se reprograman con backoff. Errores permanentes, o batches
que agotan el máximo acumulado, terminan en `dead_letter` y nunca desaparecen
silenciosamente.

Consulta y operación manual:

```powershell
.\scripts\outbox-dead-letter.ps1 -Action list
.\scripts\outbox-dead-letter.ps1 -Action requeue -RequestId <UUID>
.\scripts\outbox-dead-letter.ps1 -Action purge -RequestId <UUID>
```

Para validar una caída, reinicio y recuperación:

```powershell
.\scripts\verify-outbox-recovery.ps1
```

La documentación detallada está en `docs/OUTBOX_Y_BACKFILL.md`.

## Backfill histórico

Primero revisa lo que se enviará durante los últimos 28 días:

```powershell
.\scripts\backfill-history.ps1 -DryRun
```

Después realiza el envío:

```powershell
.\scripts\backfill-history.ps1
```

Para cargar todo el historial normalizado disponible:

```powershell
.\scripts\backfill-history.ps1 -All -DryRun
.\scripts\backfill-history.ps1 -All
```

El comando produce `request_id` deterministas según el contenido de cada batch.
Repetir exactamente el mismo backfill devuelve batches duplicados desde la API
central, sin duplicar buckets en `market_history_buckets`.

## Persistencia

`data/database/market-state.json` es una proyección local versionada y escrita
de forma atómica. Contiene snapshots históricos y de órdenes deduplicados.

Para reconstruirla desde `data/normalized/`:

```powershell
./scripts/rebuild-database.ps1
```

Para volver a normalizar `data/raw/` y sincronizar después la base local:

```powershell
./scripts/reprocess.ps1
```

Modificar `catalog/items.txt`, `catalog/markets.json` o las reglas de
normalización requiere una reconstrucción completa:

```powershell
./scripts/reprocess.ps1 -Rebuild
```

El script aparta los normalizados anteriores en una carpeta con timestamp,
regenera los JSONL desde `data/raw/` y reconstruye la base local.

## Carpetas

```text
apps/collector/
├─ cmd/receiver/       receptor, normalización y API
├─ cmd/reprocess/      reconstrucción desde datos crudos
├─ cmd/rebuilddb/      reconstrucción de la proyección persistente
├─ cmd/backfillhistory/ backfill idempotente a la API central
├─ cmd/outboxctl/      inspección y recuperación de dead-letter
└─ internal/
   ├─ httpapi/
   ├─ httpingest/
   ├─ normalization/
   ├─ observability/  logs estructurados y colores multiplataforma
   ├─ upstream/       outbox persistente, reintentos y métricas
   └─ storage/
      ├─ localdb/      base local persistente
      ├─ composite/    escritura JSONL + base local
      └─ ...

catalog/
├─ items.txt
└─ markets.json

data/
├─ raw/
├─ normalized/
├─ database/
├─ outbox/
└─ test/
```

## Normalización

- `AlbionId` se resuelve a identificador de objeto.
- Plata de paquetes reales se divide por `10.000`.
- Ticks de .NET se convierten a UTC.
- Ubicación y calidad reciben dimensiones legibles.
- Órdenes e historiales se deduplican por snapshot.
- Una orden que cambia conserva una versión nueva.
- Las órdenes expiradas no participan en `/prices`.

`catalog/markets.json` contiene los siete mercados regulares verificados y sus
identificadores exactos. La API y React utilizan claves estables (`marketKey`).
Una ubicación desconocida se conserva por ID, pero no se mezcla con una ciudad
hasta incorporarla explícitamente al catálogo.

## Pruebas aisladas

```powershell
# Terminal 1
./scripts/receiver-test.ps1

# Terminal 2
./scripts/test-receiver.ps1
```

La prueba usa el puerto `8788` y escribe únicamente dentro de `data/test/`.
