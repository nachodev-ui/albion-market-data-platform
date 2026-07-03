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

El ingreso `POST` usa un límite de concurrencia igual a `max(GOMAXPROCS, 4)`.

Cuando todos los slots están ocupados, el receiver responde antes de leer o persistir el cuerpo:

```http
HTTP/1.1 429 Too Many Requests
Retry-After: 1
```

El cliente debe reintentar esa captura. Los endpoints de salud, readiness y consulta local no consumen slots de ingestión.

## Escritura por lotes

Las órdenes normalizadas se agrupan por archivo diario. Cada lote usa una sola apertura, escritura y sincronización por archivo, en lugar de un `fsync` por orden.

La base embebida sigue instalando snapshots de forma atómica, pero usa JSON compacto para reducir bytes escritos y asignaciones. Los JSONL normalizados continúan siendo la auditoría durable y la fuente de reconstrucción.

## Outbox y caída upstream

La capacidad configurada incluye entradas pendientes y batches en estado `processing` o `retrying`.

- con utilización igual o superior al 90 %, el estado pasa a `degraded`;
- al 100 %, la outbox no crece sin límite y las nuevas entradas se rechazan;
- los rechazos se contabilizan en `queue_dropped_entries`;
- raw, normalización y almacenamiento local ocurren antes del enqueue upstream;
- los datos locales pueden reenviarse mediante las herramientas de reproceso y backfill;
- un batch solo pasa a dead-letter al agotar `UPSTREAM_MAX_DELIVERY_ATTEMPTS` o recibir un error permanente.

Los reintentos aplican jitter estable de ±20 % según `request_id` e intento. Esto evita que varias instancias reintenten en sincronía después de una caída compartida.

## Benchmarks reproducibles

Desde la raíz del repositorio:

```powershell
.\scripts\benchmark-receiver.ps1 -Count 3
```

La batería cubre 1.000 y 10.000 órdenes en:

- normalización;
- escritura JSONL normalizada;
- actualización del snapshot local;
- enqueue de la outbox persistente.

Los resultados se guardan en:

```text
artifacts/receiver-benchmarks/
```

Ese directorio está ignorado por Git. Compara resultados en la misma máquina, con la misma versión de Go y sin carga externa relevante.

CI ejecuta una iteración corta para detectar regresiones funcionales o benchmarks rotos; las comparaciones de rendimiento deben usar varias repeticiones locales.

## Validación

```powershell
.\scripts\check.ps1
.\scripts\benchmark-receiver.ps1 -Count 3
```
