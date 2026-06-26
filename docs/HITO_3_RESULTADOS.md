# Hito 3 — resultados de validación

Fecha de validación: 2026-06-22.

## Capturas preservadas

Los archivos originales del Hito 2 siguen intactos en:

- `data/raw-ingest-2026-06-22.jsonl`
- `data/market-history-2026-06-22.jsonl`

Se generaron copias separadas para el nuevo flujo:

- 9 eventos reales en `data/raw/raw-ingest-2026-06-22.jsonl`.
- 1 evento simulado en `data/test/raw/raw-ingest-2026-06-22.jsonl`.

## Historial real normalizado

Dimensiones:

```text
server: west
AlbionId: 6826
itemId: T4_MAIN_CURSEDSTAFF_CRYSTAL@4
itemName: Adept's Rotcaller Staff
locationId: 1301
locationName: Bridgewatch
quality: 2 / Bueno
```

Resultados:

| Periodo | Buckets | Unidades | Total plata | Promedio ponderado |
|---|---:|---:|---:|---:|
| 7-days | 5 | 8 | 7.233.089 | 904.136,125 |
| 4-weeks | 12 | 15 | 13.586.764 | 905.784,266667 |

La primera observación de siete días queda normalizada como:

```json
{
  "timestamp": "2026-06-21T00:00:00Z",
  "itemCount": 1,
  "totalSilver": 904439,
  "averageUnitPrice": 904439
}
```

## Órdenes

Las 7 cargas reales contenían 226 apariciones de órdenes. La deduplicación produjo:

```text
128 snapshots únicos
98 repeticiones descartadas durante la primera reconstrucción
```

Para el objeto, ubicación y calidad validados, la orden de venta mínima es:

```text
orderId: 15082648328
unitPrice: 909997
amount: 1
side: sell
```

## Prueba de idempotencia

Una segunda ejecución del reprocesador reportó:

```text
History: 0 stored, 2 duplicates
Orders: 0 stored snapshots, 226 duplicates
```

Los archivos conservaron exactamente:

```text
2 líneas de historial
128 líneas de órdenes
```

## Validaciones ejecutadas

```text
gofmt: sin archivos pendientes
go test ./apps/collector/...: correcto
go vet ./apps/collector/...: correcto
API /healthz: correcto
API /api/v1/history: correcto
API /api/v1/orders: correcto
```
