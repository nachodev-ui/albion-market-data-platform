# Rendimiento, batching y backpressure

Este bloque protege al receiver frente a capturas grandes y frente a una API upstream lenta o temporalmente fuera de servicio, sin relajar las garantías de durabilidad local.

## Flujo protegido

```text
HTTP ingest
  -> límite de concurrencia
  -> raw JSONL durable
  -> normalización
  -> normalized JSONL por lote
  -> snapshot local atómico
  -> outbox persistente acotada
  -> batches upstream con retry y jitter
```

## Backpressure HTTP

El ingreso `POST` usa un límite de concurrencia igual a `max(GOMAXPROCS, 4)`. Cuando todos los slots están ocupados, responde `429 Too Many Requests` con `Retry-After: 1` antes de leer el cuerpo. Los endpoints de salud, readiness y consulta local no consumen slots de ingestión.

## Escritura por lotes

Las órdenes normalizadas se agrupan por archivo diario. Cada lote usa una sola apertura, escritura y sincronización por archivo, en lugar de un `fsync` por orden. La base embebida mantiene snapshots atómicos con JSON compacto.

## Outbox y caída upstream

La capacidad configurada incluye entradas pendientes y batches `processing` o `retrying`.

- con utilización igual o superior al 90 %, el estado pasa a `degraded`;
- al 100 %, la outbox no crece sin límite y las nuevas entradas se rechazan;
- los rechazos se contabilizan en `queue_dropped_entries`;
- raw, normalización y almacenamiento local ocurren antes del enqueue upstream;
- un batch solo pasa a dead-letter al agotar intentos o recibir un error permanente;
- los reintentos usan backoff con jitter estable de ±20 %.

## Línea base completa

Ejecuta desde la raíz:

```powershell
.\scripts\benchmark-receiver.ps1
```

El valor predeterminado es `-Count 20`, suficiente para calcular p50 y p95 por escenario. La matriz cubre:

- normalización de 1.000 y 10.000 órdenes;
- historial de 68 buckets;
- escritura JSONL y actualización de base local;
- lectura de precios y lectura histórica;
- serialización de payloads;
- enqueue y reinicio de outbox con 1.000 y 10.000 entradas;
- memoria y allocations por operación;
- tamaño de archivos reales bajo `data/`;
- high-watermark de cola;
- recuperación después de error upstream.

Se generan tres archivos:

```text
artifacts/receiver-benchmarks/receiver-benchmarks-*.txt
artifacts/receiver-benchmarks/receiver-baseline-*.json
artifacts/receiver-benchmarks/receiver-baseline-*.csv
```

El TXT conserva la salida cruda. JSON y CSV contienen p50, p95, máximo, bytes/op y allocations/op.

## Latencia hasta PostgreSQL

La medición se hace contra la API central porque una respuesta exitosa confirma el flujo HTTP y el commit del backend. Esta prueba escribe filas sintéticas y requiere confirmación explícita:

```powershell
.\scripts\benchmark-receiver.ps1 `
  -UpstreamUrl http://127.0.0.1:8788 `
  -UpstreamToken $env:UPSTREAM_TOKEN `
  -UpstreamSamples 20 `
  -ConfirmUpstreamWrite
```

Sin `-UpstreamUrl`, el informe deja `upstream_postgresql` en `null` y muestra una advertencia; no inventa una cifra.

## Presupuestos numéricos

Los presupuestos se fijan solo después de obtener la primera línea base completa en la máquina objetivo. Deben documentarse usando p95, memoria, allocations, tamaño de archivos, high-watermark, recuperación y latencia hacia PostgreSQL.

## Validación

```powershell
.\scripts\check.ps1
.\scripts\benchmark-receiver.ps1
```

CI ejecuta una iteración corta de todos los escenarios y verifica también recuperación upstream. Las comparaciones de rendimiento usan la ejecución local de 20 muestras.
