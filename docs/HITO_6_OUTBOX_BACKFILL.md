# Hito 6 — Outbox persistente y backfill

## Resultado

Se reemplazó la dependencia de colas exclusivamente en memoria por una outbox
persistente compartida por precios e historial.

Incluye:

- persistencia atómica antes de confirmar `Enqueue`;
- recuperación automática de batches después de reiniciar;
- conservación del mismo `request_id` en todos los reintentos;
- reintentos inmediatos y reprogramación persistente con backoff;
- clasificación de errores transitorios y permanentes;
- dead-letter durable;
- métricas de pendientes, retries, dead-letter y antigüedad;
- operación `list`, `requeue` y `purge`;
- backfill histórico por rango o de todo el archivo disponible;
- UUID determinista para hacer el backfill repetible e idempotente.

## Validaciones ejecutadas

### Calidad de código

```text
gofmt: correcto
go test ./apps/collector/...: correcto
go vet ./apps/collector/...: correcto
go test -race ./apps/collector/...: correcto
go build receiver: correcto
go build backfillhistory: correcto
go build outboxctl: correcto
```

### Recuperación real de proceso

Se ejecutó una integración con un receiver real y un servidor central de prueba:

1. API central apagada;
2. captura histórica recibida y normalizada;
3. batch persistido como `retrying`;
4. receiver detenido;
5. receiver iniciado de nuevo sobre el mismo directorio;
6. batch todavía presente con el mismo `request_id`;
7. API central iniciada;
8. batch enviado automáticamente;
9. outbox vacía y `completed_batches_total=1`.

### Dead-letter

Se verificó que un batch que agotó el límite acumulado terminó en
`dead_letter`, conservando payload, número de intentos y último error.

### Backfill repetido

Se procesaron 48 capturas normalizadas reales, con 4206 buckets, en tres
batches. La segunda ejecución produjo exactamente los mismos tres UUID:

```text
primera ejecución: duplicate=false, originalRowsTouched=4206, currentRowsTouched=4206
segunda ejecución: duplicate=true, originalRowsTouched=4206, currentRowsTouched=0
```

Esto valida que repetir el mismo rango y configuración no crea buckets nuevos.

## Acción local pendiente

El backfill real de PostgreSQL debe ejecutarse en la máquina donde están el
receiver, la API central y el `.env`:

```powershell
.\scripts\backfill-history.ps1 -DryRun
.\scripts\backfill-history.ps1
```

No se ejecutó sobre la base PostgreSQL del usuario porque esa instancia local no
es accesible desde el entorno de construcción.
