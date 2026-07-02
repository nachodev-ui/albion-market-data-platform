# Observabilidad del receiver y forwarders

## Objetivo

El receiver local expone el estado del pipeline y registra eventos fáciles de
leer sin revelar el token upstream ni otras credenciales.

## Colores

Configura `LOG_COLOR` en `.env`:

```env
LOG_COLOR=auto
```

Valores disponibles:

- `auto`: usa colores solo cuando la consola los admite.
- `always`: fuerza secuencias ANSI.
- `never`: desactiva colores.

En Windows, `auto` intenta activar Virtual Terminal Processing. Si la consola
no lo permite, los niveles siguen siendo legibles, pero sin colores.

## Niveles

- `OK`: operación completada.
- `INFO`: inicio, configuración o apagado.
- `RETRY`: fallo temporal con un nuevo intento programado.
- `WARN`: entrada inválida o normalización pendiente.
- `DROP`: entrada rechazada por outbox llena o batch enviado a dead-letter.
- `ERROR`: error interno no recuperable.

## Eventos principales

### Envío correcto

```text
[OK   ] upstream.batch_sent request_id="..." server="west" entries=500 accepted=500 current_rows_touched=480 duplicate=false http_status=202 attempts=1 duration_ms=91.52 queue_depth=0 queue_capacity=5000
```

### Reintento

```text
[RETRY] upstream.retry_scheduled request_id="..." attempt=1 next_attempt=2 max_attempts_total=12 entries=500 http_status=503 attempt_duration_ms=32.11 retry_in_ms=500 queue_depth=320 queue_capacity=5000 error="upstream returned 503"
```

### Recuperación

```text
[OK   ] upstream.batch_recovered request_id="..." entries=500 attempts=2 duration_ms=640.72
```

### Rechazo por capacidad de outbox

```text
[DROP ] upstream.queue_drop reason="outbox_full" dropped_total=1 item_key="T4_PLANKS" location_id=4002 quality=1 outbox_depth=5000 outbox_capacity=5000
```

### Batch enviado a dead-letter

```text
[DROP ] upstream.batch_dead_lettered request_id="..." entries=500 attempts_total=12 http_status=503 duration_ms=1703.4 error="upstream returned 503"
```

### Historial enviado

```text
[OK   ] upstream.history_batch_sent request_id="..." server="west" entries=1 buckets=68 accepted_entries=1 accepted_buckets=68 history_rows_touched=68 duplicate=false http_status=202 attempts=1
```

### Reintento histórico

```text
[RETRY] upstream.history_retry_scheduled request_id="..." attempt=1 next_attempt=2 entries=1 buckets=68 http_status=503 retry_in_ms=500
```

### Drop histórico

```text
[DROP ] upstream.history_queue_drop reason="outbox_full" dropped_total=1 item_key="T5_LEATHER_LEVEL4@4" location_id=4002 quality=1 buckets=68 outbox_depth=1000 outbox_capacity=1000
```

## Endpoint de estado

```http
GET /api/v1/status
```

Ejemplo simplificado:

```json
{
  "status": "ok",
  "service": "albion-market-data-platform",
  "environment": "development",
  "uptime_seconds": 1850,
  "repository": {
    "historySnapshots": 20,
    "orderSnapshots": 4500,
    "storage": "local-json-database"
  },
  "price_forwarder": {
    "enabled": true,
    "running": true,
    "status": "ok",
    "in_flight_batches": 0,
    "queue": {
      "depth": 0,
      "capacity": 5000,
      "utilization_percent": 0,
      "high_watermark": 620
    },
    "outbox": {
      "path": "data/outbox/state.json",
      "pending_entries": 0,
      "retrying_batches": 0,
      "processing_batches": 0,
      "dead_letter_batches": 0,
      "oldest_pending_age_seconds": 0
    },
    "totals": {
      "enqueued_entries": 25000,
      "queue_dropped_entries": 0,
      "batches_started": 52,
      "batches_sent": 52,
      "entries_sent": 25000,
      "send_attempts": 54,
      "retries": 2,
      "recovered_batches": 2,
      "failed_batches": 0,
      "entries_dropped_after_retries": 0
    },
    "latency_ms": {
      "last_batch_ms": 84.2,
      "average_batch_ms": 93.4,
      "max_batch_ms": 684.1,
      "last_attempt_ms": 82.8
    },
    "last_status_code": 202,
    "last_success_at": "2026-06-26T00:30:00Z"
  },
  "history_forwarder": {
    "enabled": true,
    "running": true,
    "status": "ok",
    "queue": {
      "depth": 0,
      "capacity": 1000,
      "utilization_percent": 0,
      "high_watermark": 12
    },
    "outbox": {
      "path": "data/outbox/state.json",
      "pending_entries": 0,
      "retrying_batches": 0,
      "processing_batches": 0,
      "dead_letter_batches": 0,
      "oldest_pending_age_seconds": 0
    },
    "totals": {
      "enqueued_entries": 20,
      "enqueued_buckets": 1360,
      "queue_dropped_entries": 0,
      "queue_dropped_buckets": 0,
      "batches_sent": 20,
      "entries_sent": 20,
      "buckets_sent": 1360,
      "retries": 1,
      "failed_batches": 0,
      "buckets_dropped_after_retries": 0
    }
  }
}
```

`forwarder` continúa disponible como alias de `price_forwarder`.

### Estados de cada forwarder

- `disabled`: forwarding desactivado.
- `idle`: iniciado, todavía sin envíos completados.
- `ok`: el envío más reciente fue correcto.
- `degraded`: último intento fallido sin recuperación, outbox al 90 % o más, o al menos un dead-letter.
- `stopped`: forwarder detenido.

El estado superior del servicio cambia a `degraded` cuando cualquiera de los
forwarders habilitados queda degradado o detenido. El endpoint sigue respondiendo HTTP
200 para que pueda inspeccionarse desde herramientas locales.

## Seguridad

El endpoint y los logs no muestran:

- `UPSTREAM_TOKEN`;
- contenido de `DATABASE_URL` u otras credenciales;
- cuerpos completos de solicitudes de mercado.

El último error se normaliza en una sola línea y se limita a 512 caracteres.

## Validación

```powershell
.\scripts\check.ps1

Invoke-RestMethod http://127.0.0.1:8787/api/v1/status |
    ConvertTo-Json -Depth 10
```

La guía operativa de recuperación está en `OUTBOX_Y_BACKFILL.md`.


La configuración del forwarder registra `credential_source=file|environment` y `require_https`, nunca el token.
