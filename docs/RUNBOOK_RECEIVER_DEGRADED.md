# Runbook: receiver degradado

## Objetivo

Este runbook define cómo diagnosticar y recuperar el receiver local cuando
`GET /api/v1/status` muestra `degraded`, cuando las métricas Prometheus disparan
alertas o cuando el forwarder deja de enviar datos a la API central.

## Alcance

Aplica al proceso local de `albion-market-data-platform` que recibe capturas de
Albion Data Client, persiste auditoría local, actualiza la base caliente y envía
precios e historial a `albion-market-api`.

## Señales principales

Revisa primero:

```powershell
Invoke-RestMethod http://127.0.0.1:8787/api/v1/status |
    ConvertTo-Json -Depth 10

Invoke-WebRequest http://127.0.0.1:8787/metrics |
    Select-Object -ExpandProperty Content
```

El estado se considera degradado cuando ocurre una o más de estas condiciones:

- `price_forwarder.status` o `history_forwarder.status` es `degraded`;
- `price_forwarder.status` o `history_forwarder.status` es `stopped` estando habilitado;
- la outbox tiene profundidad cercana a su capacidad;
- existen batches en dead-letter;
- el último error upstream es reciente;
- el almacenamiento local no puede medirse o escribir;
- no se observan capturas nuevas tras visitar mercados.

## Triage rápido

### 1. Confirmar que el receiver está vivo

```powershell
$response = Invoke-WebRequest http://127.0.0.1:8787/healthz -Headers @{
    "X-Request-ID" = "runbook-check-0001"
}

$response.StatusCode
$response.Headers["X-Request-ID"]
```

Esperado: `200` y el mismo `X-Request-ID` de entrada.

### 2. Confirmar que `/metrics` responde Prometheus

```powershell
$metrics = Invoke-WebRequest http://127.0.0.1:8787/metrics |
    Select-Object -ExpandProperty Content

$metrics -match "albion_receiver_build_info"
$metrics -match "albion_receiver_outbox_depth"
```

Ambos valores deben ser `True`.

### 3. Revisar forwarders

```powershell
$status = Invoke-RestMethod http://127.0.0.1:8787/api/v1/status
$status.price_forwarder | ConvertTo-Json -Depth 10
$status.history_forwarder | ConvertTo-Json -Depth 10
```

Campos más importantes:

- `enabled`
- `running`
- `status`
- `queue.depth`
- `queue.capacity`
- `outbox.pending_batches`
- `outbox.dead_letter_batches`
- `last_success_at`
- `last_error_at`
- `last_error`

## Recuperación por causa

### Receiver no responde

1. Verifica que no haya otro proceso usando el puerto.
2. Reinicia el receiver.
3. Si el puerto sigue ocupado, cierra la consola vieja o finaliza el proceso que mantiene `127.0.0.1:8787`.

```powershell
Get-NetTCPConnection -LocalPort 8787 -ErrorAction SilentlyContinue |
    Select-Object LocalAddress, LocalPort, State, OwningProcess
```

### `/metrics` no responde

1. Confirma que estás ejecutando una versión actualizada de `main`.
2. Reinicia el receiver.
3. Ejecuta el verificador:

```powershell
.\scripts\verify-observability.ps1
```

### Forwarder degradado por API central apagada

1. Confirma que `albion-market-api` esté arriba.
2. Revisa su readiness.
3. Mantén el receiver encendido; la outbox debe reintentar cuando la API central vuelva.

```powershell
Invoke-RestMethod http://127.0.0.1:8080/readyz
```

### Outbox cercana al límite

1. Revisa si la API central está caída o lenta.
2. Aumenta temporalmente la capacidad si necesitas seguir capturando.
3. Evita borrar la outbox manualmente; contiene datos pendientes.

Variables útiles:

```env
UPSTREAM_QUEUE_SIZE=5000
UPSTREAM_HISTORY_QUEUE_SIZE=1000
UPSTREAM_MAX_DELIVERY_ATTEMPTS=12
UPSTREAM_MAX_RETRY_DELAY=5m
```

### Dead-letter mayor que cero

1. Guarda evidencia de `/api/v1/status` y `/metrics`.
2. Revisa `last_error` y timestamps.
3. Corrige la causa upstream antes de reintentar.
4. Usa la guía de outbox y backfill cuando corresponda.

Documento relacionado: `docs/OUTBOX_Y_BACKFILL.md`.

### Storage degradado

1. Revisa espacio disponible en disco.
2. Verifica permisos de `data/`.
3. Ejecuta backup antes de limpiar datos antiguos.
4. No elimines `data/outbox` si hay pendientes.

```powershell
Get-PSDrive -PSProvider FileSystem
Get-ChildItem .\data -Force
```

## Validación después de recuperar

```powershell
.\scripts\verify-observability.ps1

$status = Invoke-RestMethod http://127.0.0.1:8787/api/v1/status
$status.status
$status.price_forwarder.status
$status.history_forwarder.status
```

Criterio de salida:

- `/healthz` responde `200`;
- `/metrics` expone métricas obligatorias;
- `X-Request-ID` se conserva o genera;
- el receiver responde `/api/v1/status`;
- no crecen los errores de storage;
- la outbox baja o se estabiliza;
- los forwarders habilitados vuelven a `ok` o `idle` sin errores recientes.

## Cuándo escalar

Escala si:

- los dead-letter aumentan después de recuperar la API central;
- el almacenamiento local reporta errores repetidos;
- los batches se reintentan pero nunca se aceptan;
- la API central responde errores persistentes;
- el receiver se detiene al iniciar.

Incluye siempre:

```powershell
Invoke-RestMethod http://127.0.0.1:8787/api/v1/status |
    ConvertTo-Json -Depth 10

Invoke-WebRequest http://127.0.0.1:8787/metrics |
    Select-Object -ExpandProperty Content
```
