# Observabilidad del receiver y forwarders

## Objetivo

El receiver local expone estado, métricas Prometheus y eventos de operación para
seguir el pipeline de captura, almacenamiento, outbox y envío upstream.

## Formato de logs

Configura el formato con `LOG_FORMAT`:

```env
LOG_FORMAT=text
```

Valores disponibles:

- `text`: formato humano para consola.
- `json`: una línea JSON por evento, recomendado para recolectores de logs.

`LOG_COLOR` solo aplica en modo `text`:

```env
LOG_COLOR=auto
```

- `auto`: usa colores solo cuando la consola los admite.
- `always`: fuerza secuencias ANSI.
- `never`: desactiva colores.

## Request ID

Todas las rutas HTTP pasan por middleware de correlación:

- si llega `X-Request-ID` válido, se conserva;
- si falta o no cumple el formato permitido, el receiver genera uno nuevo;
- la respuesta siempre incluye `X-Request-ID`;
- los access logs incluyen `request_id`;
- los batches enviados a la API central propagan el identificador lógico del batch en `X-Request-ID`.

Formato aceptado: 8 a 128 caracteres alfanuméricos con `-`, `_`, `.`, o `:`.

```powershell
Invoke-WebRequest http://127.0.0.1:8787/healthz -Headers @{
    "X-Request-ID" = "manual-check-0001"
}
```

## Access logs HTTP

Cada petición produce `http.request_completed`.

Formato texto:

```text
2026-07-06T01:02:03Z [INFO ] http.request_completed request_id="manual-check-0001" method="GET" path="/healthz" status=200 duration_ms=1.2 response_bytes=42
```

Formato JSON:

```json
{"ts":"2026-07-06T01:02:03Z","level":"WARN","event":"http.request_completed","request_id":"manual-check-0001","method":"POST","path":"/marketorders.ingest","status":400,"duration_ms":1.2,"response_bytes":37,"error_category":"invalid_request"}
```

Los estados HTTP 4xx se registran como `WARN` y los 5xx como `ERROR`.

## Categorías de error

Los logs agregan `error_category` cuando corresponde.

| Categoría | Uso |
|---|---|
| `invalid_request` | método, ruta, query o cuerpo inválido a nivel HTTP |
| `payload_decode` | JSON inválido o payload no decodificable |
| `normalization` | payload válido pero no normalizable |
| `storage` | error escribiendo raw, normalized, base local u outbox |
| `backpressure` | rechazo por capacidad temporal del receiver |
| `forwarder_queue` | entrada no encolada hacia forwarder |
| `upstream_payload` | error construyendo payload upstream |
| `upstream_http` | respuesta HTTP upstream fallida |
| `upstream_transport` | fallo de transporte hacia upstream |
| `upstream_response` | respuesta upstream ilegible o no decodificable |
| `timeout` | timeout local o de red |
| `canceled` | contexto cancelado |
| `internal` | error interno no clasificado |

## Niveles

- `OK`: operación completada.
- `INFO`: inicio, configuración, petición HTTP normal o apagado.
- `RETRY`: fallo temporal con un nuevo intento programado.
- `WARN`: entrada inválida o normalización pendiente.
- `DROP`: entrada rechazada por outbox llena o batch enviado a dead-letter.
- `ERROR`: error interno no recuperable.

## Eventos principales

### Envío correcto

```text
[OK   ] upstream.batch_sent request_id="..." server="west" entries=500 accepted=500 current_rows_touched=480 duplicate=false http_status=202 attempts_this_cycle=1 duration_ms=91.52 outbox_depth=0
```

### Reintento

```text
[RETRY] upstream.retry_scheduled request_id="..." attempt=1 http_status=503 attempt_duration_ms=32.11 retry_in_ms=500 error="upstream returned 503" error_category="upstream_http"
```

### Historial enviado

```text
[OK   ] upstream.history_batch_sent request_id="..." server="west" entries=1 buckets=68 accepted_entries=1 accepted_buckets=68 history_rows_touched=68 duplicate=false http_status=202 attempts_this_cycle=1
```

### Batch enviado a dead-letter

```text
[DROP ] upstream.batch_dead_lettered request_id="..." entries=500 attempts_total=12 http_status=503 error="upstream returned 503" error_category="upstream_http"
```

## Endpoint de estado

```http
GET /api/v1/status
```

`forwarder` continúa disponible como alias de `price_forwarder`.

Estados de cada forwarder:

- `disabled`: forwarding desactivado.
- `idle`: iniciado, todavía sin envíos completados.
- `ok`: el envío más reciente fue correcto.
- `degraded`: último intento fallido sin recuperación, outbox al 90 % o más, o al menos un dead-letter.
- `stopped`: forwarder detenido.

El estado superior del servicio cambia a `degraded` cuando cualquiera de los
forwarders habilitados queda degradado o detenido. El endpoint sigue respondiendo
HTTP 200 para que pueda inspeccionarse desde herramientas locales.

## Endpoint de métricas

```http
GET /metrics
```

La respuesta usa el formato de exposición de Prometheus y combina:

1. Registro concurrente del receiver.
2. Snapshots de forwarders y outbox persistente.
3. Medición cacheada de bytes en `raw`, `normalized`, base local y outbox.

Consulta rápida:

```powershell
Invoke-WebRequest http://127.0.0.1:8787/metrics |
    Select-Object -ExpandProperty Content
```

Métricas principales:

| Métrica | Tipo | Interpretación |
|---|---|---|
| `albion_receiver_captures_received_total` | counter | Peticiones de captura recibidas por topic |
| `albion_receiver_entries_received_total` | counter | Entradas decodificadas por pipeline |
| `albion_receiver_entries_stored_total` | counter | Entradas nuevas persistidas |
| `albion_receiver_duplicates_total` | counter | Entradas deduplicadas |
| `albion_receiver_normalization_errors_total` | counter | Fallos de decode o normalización |
| `albion_receiver_storage_errors_total` | counter | Errores de escritura por área |
| `albion_receiver_forwarder_batches_sent_total` | counter | Batches aceptados por la API central |
| `albion_receiver_forwarder_retries_total` | counter | Reintentos upstream |
| `albion_receiver_outbox_depth` | gauge | Entradas pendientes por pipeline |
| `albion_receiver_outbox_capacity` | gauge | Capacidad configurada por pipeline |
| `albion_receiver_outbox_oldest_pending_age_seconds` | gauge | Edad del pendiente más antiguo |
| `albion_receiver_dead_letter_batches_total` | counter | Batches enviados a dead-letter |
| `albion_receiver_upstream_latency_seconds` | gauge | Última, media, máxima y último intento |
| `albion_receiver_upstream_last_success_timestamp_seconds` | gauge | Último envío exitoso |
| `albion_receiver_storage_bytes` | gauge | Bytes por área y total observable |
| `albion_receiver_uptime_seconds` | gauge | Tiempo activo del proceso |
| `albion_receiver_build_info` | gauge | Versión, commit, build time y Go |

Los timestamps sin observaciones se exponen como `0`. Las métricas son acumuladas
desde el inicio del proceso, excepto los totales persistentes de outbox, que
sobreviven reinicios.

## Redacción de campos

La redacción aplica sobre campos simples y estructuras anidadas, como cabeceras,
mapas y arreglos. Los errores se normalizan en una sola línea y se limitan a 512
caracteres.

## Validación

```powershell
.\scripts\check.ps1

Invoke-RestMethod http://127.0.0.1:8787/api/v1/status |
    ConvertTo-Json -Depth 10

Invoke-WebRequest http://127.0.0.1:8787/metrics |
    Select-Object -ExpandProperty Content
```

La guía operativa de recuperación está en `OUTBOX_Y_BACKFILL.md`.
