# Outbox persistente y backfill histórico

## Objetivo

Evitar que un batch de precios o historial desaparezca cuando:

- la API central está caída;
- se agotan los reintentos inmediatos;
- el receiver se reinicia;
- Windows se apaga;
- existe un error permanente de autenticación o contrato.

La solución usa una outbox común y durable para ambos pipelines.

```text
captura normalizada
        ↓
outbox persistente
        ↓
worker de precios o historial
        ↓
API central
        ↓
confirmar y eliminar de la outbox
```

## Archivo de estado

Por defecto:

```text
data/outbox/state.json
```

Se escribe mediante archivo temporal, `fsync`, respaldo y renombre. El respaldo
`state.json.bak` permite recuperar el estado si la escritura principal queda
interrumpida.

No edites estos archivos manualmente mientras el receiver está activo.

## Ciclo de vida de un batch

Estados persistentes:

- `processing`: reclamado por un worker;
- `retrying`: esperando su siguiente intento;
- `dead_letter`: error permanente o máximo de intentos agotado.

Las entradas todavía no agrupadas permanecen en `items`. Al crear un batch, la
selección y el `request_id` se guardan juntos antes de enviar HTTP.

Si el proceso cae después de que la API central aceptó el batch, pero antes de
marcarlo como completado, el receiver lo reenvía con el mismo `request_id`. La
idempotencia central responde como duplicado y evita una segunda inserción.

## Reintentos

Configuración:

```env
UPSTREAM_RETRY_COUNT=3
UPSTREAM_RETRY_DELAY=500ms
UPSTREAM_MAX_DELIVERY_ATTEMPTS=12
UPSTREAM_MAX_RETRY_DELAY=5m
```

`UPSTREAM_RETRY_COUNT` controla los intentos inmediatos de un ciclo. Si todos
fallan por un error transitorio, el batch vuelve a disco con backoff exponencial.

Son transitorios:

- errores de red;
- HTTP 408, 425 y 429;
- HTTP 5xx.

Los demás HTTP 4xx se consideran permanentes y pasan directamente a
`dead_letter`.

## Estado y observabilidad

`GET /api/v1/status` expone dentro de cada forwarder:

```json
{
  "outbox": {
    "path": "data/outbox/state.json",
    "pending_entries": 2,
    "pending_batches": 0,
    "retrying_batches": 1,
    "processing_batches": 0,
    "dead_letter_batches": 0,
    "oldest_pending_at": "2026-06-26T10:00:00Z",
    "oldest_pending_age_seconds": 42
  }
}
```

Si existe al menos un dead-letter, el forwarder aparece como `degraded`.

## Dead-letter

Listar:

```powershell
.\scripts\outbox-dead-letter.ps1 -Action list
```

Filtrar por historial:

```powershell
.\scripts\outbox-dead-letter.ps1 -Action list -Pipeline history
```

Reencolar después de corregir la causa:

```powershell
.\scripts\outbox-dead-letter.ps1 `
  -Action requeue `
  -RequestId 00000000-0000-0000-0000-000000000000
```

Eliminar definitivamente:

```powershell
.\scripts\outbox-dead-letter.ps1 `
  -Action purge `
  -RequestId 00000000-0000-0000-0000-000000000000
```

Detén el receiver antes de usar `requeue` o `purge`; el archivo no implementa
bloqueo entre procesos distintos. `list` también es más seguro con el receiver
detenido.

## Validación de recuperación

Ejecuta:

```powershell
.\scripts\verify-outbox-recovery.ps1
```

La prueba guía estos pasos:

1. apagar la API central;
2. enviar una captura al receiver;
3. comprobar que sigue pendiente en disco;
4. reiniciar el receiver;
5. comprobar que el pendiente continúa;
6. iniciar la API central;
7. comprobar que la outbox se vacía automáticamente.

## Backfill de historial

El backfill lee:

```text
data/normalized/market-history-*.jsonl
```

El rango se filtra por `capturedAt`.

### Últimos 28 días

```powershell
.\scripts\backfill-history.ps1 -DryRun
.\scripts\backfill-history.ps1
```

### Todo lo disponible

```powershell
.\scripts\backfill-history.ps1 -All -DryRun
.\scripts\backfill-history.ps1 -All
```

### Rango específico

```powershell
.\scripts\backfill-history.ps1 `
  -From 2026-06-01 `
  -To 2026-06-26 `
  -Server west
```

Cada batch genera un UUID determinista a partir de su contenido. Si el comando
se interrumpe, puede ejecutarse de nuevo con los mismos parámetros. La API
central reconocerá los batches ya aceptados.

La estabilidad del UUID depende de mantener iguales:

- rango;
- filtro de servidor;
- orden de los archivos;
- `BatchSize`;
- `MaxBuckets`;
- contenido de los JSONL.

Aunque cambie el batching, la clave primaria de `market_history_buckets` sigue
evitando buckets duplicados.

## Comprobación SQL

Antes y después del primer backfill:

```sql
select count(*) as buckets
from market_history_buckets;
```

Ejecuta el mismo backfill una segunda vez y repite la consulta. El conteo no
debe aumentar por duplicados. En la segunda ejecución, la salida debe mostrar
`duplicate=true` para batches idénticos, `originalRowsTouched` conserva el resultado de la primera ingesta y `currentRowsTouched=0` confirma que el reenvío no modificó filas.
