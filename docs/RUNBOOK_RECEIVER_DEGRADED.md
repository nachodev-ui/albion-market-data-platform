# Runbook: receiver degradado

## Objetivo

Diagnosticar y recuperar el receiver local cuando `GET /api/v1/status` muestra
`degraded`, cuando las métricas Prometheus indican problemas o cuando el envío a
la API central deja de avanzar.

## Comandos iniciales

```powershell
Invoke-RestMethod http://127.0.0.1:8787/api/v1/status |
    ConvertTo-Json -Depth 10

Invoke-WebRequest http://127.0.0.1:8787/metrics |
    Select-Object -ExpandProperty Content
```

## Señales de degradación

- `price_forwarder.status` o `history_forwarder.status` es `degraded`.
- Un forwarder habilitado aparece como `stopped`.
- La outbox se acerca a su capacidad.
- Hay batches en dead-letter.
- Hay errores recientes de upstream o storage.
- No se observan capturas nuevas después de visitar mercados.

## Triage rápido

### 1. Health y request id

```powershell
$response = Invoke-WebRequest http://127.0.0.1:8787/healthz -Headers @{
    "X-Request-ID" = "runbook-check-0001"
}

$response.StatusCode
$response.Headers["X-Request-ID"]
```

Esperado: `200` y el mismo `X-Request-ID`.

### 2. Métricas

```powershell
$metrics = Invoke-WebRequest http://127.0.0.1:8787/metrics |
    Select-Object -ExpandProperty Content

$metrics -match "albion_receiver_build_info"
$metrics -match "albion_receiver_outbox_depth"
```

### 3. Forwarders

```powershell
$status = Invoke-RestMethod http://127.0.0.1:8787/api/v1/status
$status.price_forwarder | ConvertTo-Json -Depth 10
$status.history_forwarder | ConvertTo-Json -Depth 10
```

Revisa `enabled`, `running`, `status`, `queue.depth`, `queue.capacity`, outbox,
`last_success_at`, `last_error_at` y `last_error`.

## Recuperación

### Receiver no responde

Verifica el puerto y reinicia el proceso.

```powershell
Get-NetTCPConnection -LocalPort 8787 -ErrorAction SilentlyContinue |
    Select-Object LocalAddress, LocalPort, State, OwningProcess
```

### Métricas no responden

Actualiza `main`, reinicia el receiver y ejecuta:

```powershell
.\scripts\verify-api.ps1
```

### API central apagada

Confirma readiness de `albion-market-api`:

```powershell
Invoke-RestMethod http://127.0.0.1:8080/readyz
```

Mantén el receiver encendido para que la outbox reintente cuando la API vuelva.

### Outbox cerca del límite

Revisa disponibilidad de la API central. Aumenta temporalmente la capacidad si
necesitas seguir capturando.

```env
UPSTREAM_QUEUE_SIZE=5000
UPSTREAM_HISTORY_QUEUE_SIZE=1000
UPSTREAM_MAX_DELIVERY_ATTEMPTS=12
UPSTREAM_MAX_RETRY_DELAY=5m
```

### Dead-letter mayor que cero

Guarda evidencia de `/api/v1/status` y `/metrics`, corrige la causa upstream y
usa `docs/OUTBOX_Y_BACKFILL.md` para recuperación manual.

### Storage degradado

Revisa espacio disponible y permisos de `data/`.

```powershell
Get-PSDrive -PSProvider FileSystem
Get-ChildItem .\data -Force
```

## Validación final

```powershell
.\scripts\verify-api.ps1

$status = Invoke-RestMethod http://127.0.0.1:8787/api/v1/status
$status.status
$status.price_forwarder.status
$status.history_forwarder.status
```

Criterio de salida:

- `/healthz` responde `200`;
- `/metrics` expone métricas obligatorias;
- `X-Request-ID` se conserva o genera;
- `/api/v1/status` responde;
- no crecen los errores de storage;
- la outbox baja o se estabiliza;
- los forwarders habilitados vuelven a `ok` o `idle`.
