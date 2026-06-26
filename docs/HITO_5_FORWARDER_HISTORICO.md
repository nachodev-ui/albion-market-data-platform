# Hito 5 — Forwarder histórico

## Resultado

El receiver local ahora envía las capturas históricas normalizadas a
`albion-market-api` mediante:

```http
POST /api/v1/ingest/history
```

El flujo local de precios actuales no se modifica.

## Componentes agregados

- contrato Go de ingesta histórica;
- método HTTP autenticado `Client.SendHistory`;
- cola y worker `HistoryForwarder` independientes;
- batching por cantidad de capturas y buckets;
- reintentos conservando el mismo `request_id` UUID;
- métricas separadas de entradas y buckets;
- transformación desde `NormalizedHistory` al contrato central;
- sección `history_forwarder` en `/api/v1/status`;
- variables de entorno específicas;
- script de verificación end-to-end.

## Compatibilidad

- `UPSTREAM_HISTORY_ENABLED` hereda `UPSTREAM_ENABLED` si no se define.
- `forwarder` continúa siendo alias de `price_forwarder` en el endpoint de
  estado.
- el receiver persiste localmente antes de encolar el envío central.
- React y las rutas públicas continúan trabajando con `marketKey`.

## Validaciones ejecutadas

```text
gofmt: correcto
go test ./apps/collector/...: correcto
go vet ./apps/collector/...: correcto
go test -race ./apps/collector/...: correcto
go build ./apps/collector/cmd/receiver: correcto
```

También se ejecutó una integración real receiver → servidor HTTP de prueba:

- captura AoData recibida;
- normalización correcta;
- batch histórico generado;
- token Bearer enviado;
- respuesta 202 procesada;
- métricas `entries_sent=1` y `buckets_sent=1`.

## Evolución posterior

La limitación original de colas en memoria fue resuelta en el siguiente cambio:
ambos pipelines usan una outbox persistente compartida, recuperación automática
al reiniciar y dead-letter. Consulta `OUTBOX_Y_BACKFILL.md`.
