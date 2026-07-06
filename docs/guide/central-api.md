# Conexión con albion-market-api

El receiver puede reenviar precios actuales e historial normalizado a `albion-market-api`.

## Configuración mínima

```env
UPSTREAM_ENABLED=true
UPSTREAM_HISTORY_ENABLED=true
UPSTREAM_BASE_URL=http://127.0.0.1:8080
UPSTREAM_TOKEN=
UPSTREAM_TOKEN_FILE=./secrets/upstream-current.token
UPSTREAM_MIN_TOKEN_LENGTH=32
UPSTREAM_REQUIRE_HTTPS=false
```

En producción, usa HTTPS:

```env
APP_ENV=production
UPSTREAM_BASE_URL=https://TU_API
UPSTREAM_REQUIRE_HTTPS=true
```

## Orden de arranque

1. Inicia PostgreSQL.
2. Inicia `albion-market-api`.
3. Verifica readiness.
4. Inicia el receiver.
5. Visita mercados con Albion Data Client.

```powershell
Invoke-RestMethod http://127.0.0.1:8080/readyz
Invoke-RestMethod http://127.0.0.1:8787/api/v1/status |
    ConvertTo-Json -Depth 10
```

## Señales saludables

- `price_forwarder.enabled=true`
- `history_forwarder.enabled=true`
- `status=ok` o `idle`
- `last_success_at` se actualiza después de capturas
- `outbox.pending_entries` baja tras enviar

## Fallos comunes

- `401` o `403`: token incorrecto o no rotado en ambos proyectos.
- `5xx`: API central o PostgreSQL degradados.
- `timeout`: API central no responde dentro de `UPSTREAM_TIMEOUT`.
- outbox creciendo: la API central no está aceptando batches.
