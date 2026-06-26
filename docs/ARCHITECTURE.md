# Arquitectura del servicio local

## Flujo de escritura local

1. Albion Data Client envía un POST compatible con AoData al receptor.
2. `rawjsonl` conserva el evento exacto en `data/raw/`.
3. El normalizador resuelve catálogo, plata, fechas, ubicación y calidad.
4. `composite.Store` escribe la misma entidad en dos destinos:
   - `normalizedjsonl`, auditoría reconstruible;
   - `localdb`, proyección persistente para lectura rápida.
5. La API local consulta solamente `localdb`.

La persistencia local ocurre antes de cualquier envío upstream. Por eso una
caída de la API central no impide conservar ni consultar la captura desde el
receiver.

## Forwarding central

Después de normalizar, el receiver alimenta dos pipelines independientes:

```text
marketorders.ingest
  → snapshot de precio actual
  → price_forwarder
  → POST /api/v1/ingest/prices

markethistories.ingest
  → captura histórica normalizada
  → history_forwarder
  → POST /api/v1/ingest/history
```

Cada pipeline tiene su propio worker, batching, métricas y estado. Ambos
comparten una outbox persistente, además de la URL, token, timeout, compresión
y política de reintentos.

Los reintentos de un mismo batch conservan el `request_id`, permitiendo que
`albion-market-api` aplique idempotencia incluso cuando la respuesta de un envío
exitoso se pierde.

## Persistencia elegida

La proyección se guarda en `data/database/market-state.json`. No requiere un
servidor PostgreSQL ni servicios adicionales para ejecutar el proyecto local.

El archivo contiene un `schemaVersion`, fecha de actualización, historiales y
órdenes. Las escrituras usan un archivo temporal y reemplazo atómico. La clave
de deduplicación de cada entidad funciona como índice persistente.

Los JSONL no se sustituyen: siguen siendo la auditoría y permiten reconstruir la
proyección con `cmd/rebuilddb`.

La outbox upstream se guarda en `data/outbox/state.json`. Las entradas quedan
en disco antes de que el handler confirme que fueron encoladas. Un reinicio
recupera batches pendientes con el mismo `request_id`; los errores permanentes
o intentos acumulados agotados quedan en `dead_letter` para inspección manual.

## Lecturas para la calculadora

`GET /api/v1/prices` recibe hasta 200 identificadores y devuelve una fila por
objeto. Para cada combinación servidor, ciudad y calidad calcula:

- `sellPriceMin`: menor orden de venta vigente conocida;
- `buyPriceMax`: mayor orden de compra vigente conocida;
- fecha de captura de ambos valores.

`GET /api/v1/history` entrega los eventos normalizados más recientes por
periodo. La calculadora solicita `4-weeks` y construye vistas de 7 y 28 días en
el navegador.

La API central ofrece contratos equivalentes por `marketKey`; los
`location_id` solo existen dentro del salto autenticado receiver → API central.

## Recuperación

- Base ausente: el receptor importa automáticamente `data/normalized/`.
- Base dañada o catálogo modificado: ejecutar `scripts/rebuild-database.ps1`.
- Normalizados obsoletos o catálogo modificado: ejecutar `scripts/reprocess.ps1 -Rebuild`.
- Fallo central temporal: la outbox conserva los batches y el frontend puede seguir consultando el receiver local.
- Dead-letter: revisar y reencolar con `scripts/outbox-dead-letter.ps1`.
- Historial anterior: ejecutar `scripts/backfill-history.ps1`.
