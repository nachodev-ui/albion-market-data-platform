# Hito 4 — Persistencia y consumo desde la calculadora

## Entregado

- Proyección persistente local en `data/database/market-state.json`.
- Importación automática de los JSONL normalizados al iniciar.
- Escritura simultánea de auditoría JSONL y base local.
- Endpoint batch `/api/v1/prices`.
- Endpoint `/api/v1/status` con estadísticas del repositorio.
- CORS para el frontend local.
- Cliente React de precios e historial apuntando a `127.0.0.1:8787`.
- Variable `VITE_MARKET_API_URL` para cambiar la URL sin tocar código.
- Invalidación de las cachés antiguas provenientes de la API pública.
- Scripts para reconstruir y verificar la base local.

## Validación incluida

Con las capturas reales del proyecto, la base local contiene historiales y
órdenes de Bridgewatch y Brecilien. El endpoint batch devuelve precios de
compra y venta para los objetos presentes, y el historial de cuatro semanas se
puede consumir directamente desde la calculadora.

## Límite conocido

El servicio solo puede responder con combinaciones que hayan sido capturadas
por Albion Data Client y normalizadas con una ubicación conocida. La interfaz
muestra ausencia de datos en vez de recurrir silenciosamente a la API pública.

## Catálogo único de mercados

El servicio expone `GET /api/v1/markets` y resuelve las consultas mediante
`marketKey`. Los siete mercados regulares usan los códigos verificados en el
juego. React consume este endpoint y ya no mantiene un mapa propio de ciudades.
Black Market permanece declarado pero deshabilitado.
