# Catálogo único de mercados

## Contrato

El catálogo canónico vive en `catalog/markets.json`. El frontend no mantiene
una lista duplicada; la obtiene mediante:

```text
GET /api/v1/markets
```

Cada mercado posee una clave estable (`marketKey`) y dos ubicaciones
observadas:

- `cityLocationId`: zona central informada por Albion Data Client;
- `marketLocationId`: identificador incluido en los paquetes de órdenes e
  historial y utilizado para consultar precios.

Las consultas nuevas deben usar `marketKey`:

```text
GET /api/v1/prices?server=west&marketKey=lymhurst&itemIds=T3_CLOTH&quality=1
GET /api/v1/history?server=west&marketKey=brecilien&itemId=T6_MAIN_CURSEDSTAFF_CRYSTAL%403&quality=4&period=4-weeks&limit=1
```

El backend resuelve la clave al `marketLocationId` exacto. `location` y
`locationId` se conservan únicamente por compatibilidad y depuración.

## Ubicaciones desconocidas

Un código no incluido en `markets.json`, como una zona portal, se conserva sin
nombre ni `marketKey`. No se mezcla automáticamente con un mercado regular.
