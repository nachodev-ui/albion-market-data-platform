# API local

La API se ejecuta dentro del binario Go `receiver`, en el mismo proceso que
recibe y normaliza las capturas. Las lecturas se resuelven desde la proyección
persistente `data/database/market-state.json`.

Endpoints:

- `GET /healthz`
- `GET /api/v1/status` (repositorio, cola, reintentos, latencia y estado upstream)
- `GET /api/v1/prices`
- `GET /api/v1/history`
- `GET /api/v1/orders`

El frontend React usa `/prices` para consultas batch y `/history` para el
producto terminado. Los endpoints incluyen CORS para desarrollo local.
