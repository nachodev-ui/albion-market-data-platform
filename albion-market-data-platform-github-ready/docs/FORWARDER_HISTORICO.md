# Forwarder de historial hacia la API central

Este cambio conecta las capturas `markethistories.ingest` del receiver local
con el contrato autenticado de `albion-market-api`:

```text
Albion Data Client
        ↓
POST /markethistories.ingest
        ↓
normalización + auditoría local + base local
        ↓
outbox persistente + history_forwarder
        ↓
POST /api/v1/ingest/history
        ↓
PostgreSQL central
```

La persistencia local ocurre antes de encolar el envío. Un fallo de la API
central no elimina el historial del receiver ni impide que el frontend use el
fallback local.

## Contrato enviado

Cada captura normalizada se transforma en una entrada como esta:

```json
{
  "request_id": "00112233-4455-4677-8899-aabbccddeeff",
  "server": "west",
  "entries": [
    {
      "observed_at": "2026-06-26T20:00:00Z",
      "location_id": 4002,
      "item_key": "T5_LEATHER_LEVEL4@4",
      "quality": 1,
      "history": [
        {
          "timestamp": "2026-06-25T00:00:00Z",
          "item_count": 42,
          "average_unit_price": 18750
        }
      ]
    }
  ]
}
```

`location_id` aparece solamente en esta comunicación interna y autenticada.
El contrato público de lectura continúa usando `marketKey`.

El mismo `request_id` se conserva durante todos los reintentos de un batch. De
esta manera, si la API central alcanzó a guardar la solicitud pero la respuesta
se perdió, el reintento se reconoce como duplicado y no crea buckets dobles.

## Conversión de buckets

- `observed_at` corresponde a `capturedAt` de la captura normalizada.
- `timestamp` se envía en UTC.
- `item_count` conserva la cantidad vendida del bucket.
- `average_unit_price` se calcula como `totalSilver / itemCount` usando plata
  ya normalizada.
- Un bucket sin ventas o sin precio significativo envía
  `average_unit_price: null`.

## Configuración

Las credenciales y transporte se comparten con el forwarder de precios:

```env
UPSTREAM_BASE_URL=http://127.0.0.1:8080
UPSTREAM_TOKEN=CHANGE_ME_STRONG_RANDOM_TOKEN
UPSTREAM_TIMEOUT=5s
UPSTREAM_RETRY_COUNT=3
UPSTREAM_RETRY_DELAY=500ms
UPSTREAM_GZIP=false
```

Configuración específica de historial:

```env
UPSTREAM_HISTORY_ENABLED=true
UPSTREAM_HISTORY_BATCH_SIZE=100
UPSTREAM_HISTORY_MAX_BATCH_BUCKETS=100000
UPSTREAM_HISTORY_FLUSH_INTERVAL=500ms
UPSTREAM_HISTORY_QUEUE_SIZE=1000
```

Si `UPSTREAM_HISTORY_ENABLED` no está definido, hereda el valor de
`UPSTREAM_ENABLED`. Por lo tanto, una configuración antigua con
`UPSTREAM_ENABLED=true` activa ambos forwarders después de actualizar.

Los límites de batch respetan el contrato de la API central:

- máximo 2000 capturas por request;
- máximo 100000 buckets por request;
- además del límite de bytes configurado en la API central.

El valor predeterminado de 100 capturas mantiene los requests habituales muy
por debajo de esos máximos.

## Reintentos e idempotencia

Cada ciclo intenta el batch hasta `UPSTREAM_RETRY_COUNT` veces. El retraso
dentro del ciclo es lineal:

```text
intento 1 falla → espera 1 × UPSTREAM_RETRY_DELAY
intento 2 falla → espera 2 × UPSTREAM_RETRY_DELAY
```

Los reintentos reutilizan exactamente el mismo cuerpo y `request_id`.

Si el ciclo inmediato falla por red, HTTP 408, 425, 429 o 5xx, el batch queda
en la outbox y se reprograma con backoff exponencial. El total acumulado se
limita con `UPSTREAM_MAX_DELIVERY_ATTEMPTS`. Los demás HTTP 4xx y los batches
que agotan ese máximo pasan a `dead_letter`, donde permanecen hasta reencolarlos
o eliminarlos manualmente.

```env
UPSTREAM_OUTBOX_PATH=./data/outbox/state.json
UPSTREAM_MAX_DELIVERY_ATTEMPTS=12
UPSTREAM_MAX_RETRY_DELAY=5m
```

## Estado

```http
GET /api/v1/status
```

Ahora diferencia explícitamente:

```json
{
  "price_forwarder": {},
  "history_forwarder": {}
}
```

`forwarder` se mantiene como alias temporal de `price_forwarder` para no romper
herramientas anteriores.

La sección histórica informa, entre otros:

- capturas y buckets encolados;
- capturas y buckets rechazados por capacidad de outbox;
- entradas pendientes, batches reintentando y dead-letter;
- antigüedad del pendiente más antiguo;
- batches enviados, recuperados, reprogramados o fallidos;
- capturas y buckets enviados;
- reintentos;
- latencia;
- último éxito, último error y último código HTTP.

## Logs

Eventos principales:

```text
upstream.history_forwarder_started
upstream.history_queue_drop
upstream.history_retry_scheduled
upstream.history_batch_sent
upstream.history_batch_recovered
upstream.history_batch_persisted_for_retry
upstream.history_batch_dead_lettered
upstream.history_forwarder_stopped
```

La normalización inicial también informa:

```text
ingest.history_completed ... forwarded=1 forwarded_buckets=68 dropped=0
```

## Verificación end-to-end

Con la API central y el receiver activos:

```powershell
Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass
.\scripts\verify-history-forwarder.ps1
```

El script:

1. comprueba que `history_forwarder` esté habilitado;
2. envía una captura controlada al receiver;
3. espera el flush del batch;
4. consulta la API central por `marketKey`;
5. confirma que el bucket apareció en PostgreSQL.

También puedes inspeccionar las métricas manualmente:

```powershell
$status = Invoke-RestMethod http://127.0.0.1:8787/api/v1/status
$status.history_forwarder | ConvertTo-Json -Depth 10
```

## Backfill de capturas anteriores

El forwarder opera sobre capturas nuevas. Para cargar los JSONL existentes:

```powershell
.\scripts\backfill-history.ps1 -DryRun
.\scripts\backfill-history.ps1
```

Para todo lo disponible, usa `-All`. Los `request_id` del backfill son
deterministas, por lo que repetir el mismo rango es idempotente.
