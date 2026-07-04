# API local y contrato OpenAPI

El receiver expone una API HTTP de solo lectura para la calculadora y para
operación local. El contrato verificable vive en:

```text
openapi/openapi.json
```

La API escucha en loopback de forma predeterminada:

```text
http://127.0.0.1:8787
```

## Endpoints

| Método | Ruta | Propósito |
|---|---|---|
| `GET` | `/healthz` | Liveness del proceso HTTP |
| `GET` | `/metrics` | Métricas operativas en formato Prometheus |
| `GET` | `/readyz` | Readiness del catálogo y repositorio local |
| `GET` | `/api/v1/status` | Estado del receiver, repositorio y forwarders |
| `GET` | `/api/v1/markets` | Mercados configurados |
| `GET` | `/api/v1/prices` | Precios actuales calculados desde órdenes activas |
| `GET` | `/api/v1/history` | Capturas históricas normalizadas |
| `GET` | `/api/v1/orders` | Órdenes normalizadas capturadas |

Las rutas JSON documentadas por OpenAPI aceptan `GET` y `OPTIONS`. Las rutas
desconocidas responden `404` y los métodos no admitidos responden `405` con el
header `Allow: GET, OPTIONS`. `/metrics` es un endpoint operacional separado:
acepta únicamente `GET`, devuelve `text/plain; version=0.0.4` y no forma parte
del contrato JSON consumido por la calculadora.

## Métricas Prometheus

```powershell
Invoke-WebRequest http://127.0.0.1:8787/metrics |
    Select-Object -ExpandProperty Content
```

El exporter reutiliza los snapshots internos de precios e historial y no lee ni
parsea manualmente `data/outbox/state.json`. Expone, entre otras:

- `albion_receiver_captures_received_total`;
- `albion_receiver_entries_received_total`;
- `albion_receiver_entries_stored_total`;
- `albion_receiver_duplicates_total`;
- `albion_receiver_normalization_errors_total`;
- `albion_receiver_storage_errors_total`;
- `albion_receiver_outbox_depth` y `albion_receiver_outbox_capacity`;
- `albion_receiver_outbox_oldest_pending_age_seconds`;
- `albion_receiver_dead_letter_batches_total`;
- `albion_receiver_upstream_latency_seconds`;
- `albion_receiver_upstream_last_success_timestamp_seconds`;
- `albion_receiver_storage_bytes`;
- `albion_receiver_uptime_seconds`;
- `albion_receiver_build_info`.

Los tamaños de almacenamiento se cachean brevemente para que un scraper no
recorra los directorios en cada solicitud consecutiva.

## Liveness y readiness

```powershell
Invoke-RestMethod http://127.0.0.1:8787/healthz
Invoke-RestMethod http://127.0.0.1:8787/readyz
```

`/healthz` indica que el proceso HTTP está vivo. `/readyz` devuelve `200`
cuando el catálogo contiene mercados habilitados y el repositorio local está
disponible. Si una comprobación falla, devuelve `503` y señala el componente
`unavailable`.

## CORS por allowlist

La API ya no responde con `Access-Control-Allow-Origin: *`. Los navegadores
solo reciben autorización cuando el header `Origin` coincide exactamente con
`LOCAL_API_ALLOWED_ORIGINS`:

```env
LOCAL_API_ALLOWED_ORIGINS=http://127.0.0.1:5173,http://localhost:5173
```

Las solicitudes locales sin header `Origin`, como PowerShell, curl o los smoke
tests, siguen funcionando normalmente. No se admite el comodín `*`.

## Exposición de red

El valor predeterminado permanece limitado a loopback:

```env
COLLECTOR_LISTEN=127.0.0.1:8787
COLLECTOR_ALLOW_REMOTE=false
```

El receiver rechaza `0.0.0.0`, direcciones LAN y hostnames no loopback salvo
que `COLLECTOR_ALLOW_REMOTE=true` se configure explícitamente. Esa excepción
solo debe utilizarse detrás de un firewall confiable; la API local no incorpora
autenticación para consumidores de lectura.

## Límites HTTP

```env
COLLECTOR_MAX_HEADER_BYTES=65536
```

Además del límite de headers del servidor, la API aplica:

- máximo de 16 KiB para la query string;
- máximo de 256 valores de consulta;
- máximo de 64 bytes por nombre de parámetro;
- máximo de 4096 bytes por valor;
- máximo de 200 identificadores en `/api/v1/prices`;
- límites específicos de `limit` para historial y órdenes.

Las respuestas JSON incluyen `Cache-Control: no-store`,
`X-Content-Type-Options: nosniff` y `Referrer-Policy: no-referrer`.

## Validación contractual

Los tests comparan:

- todas las rutas OpenAPI contra las rutas GET reales;
- el schema de respuesta `200` de cada operación;
- las propiedades OpenAPI contra los tags JSON de los DTO Go;
- la respuesta `503` de readiness.

Validación local:

```powershell
go test ./apps/collector/internal/httpapi -run TestOpenAPI -count=1
.\scripts\check.ps1
```

El workflow `.github/workflows/contracts.yml` repite el contrato en cada pull
request que modifica la API, los DTO o el documento OpenAPI.
