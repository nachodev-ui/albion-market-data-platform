# Configuración

La configuración vive en `.env`. Parte copiando el ejemplo:

```powershell
Copy-Item .env.example .env
```

## Básico

```env
ALBION_SERVER=west
APP_ENV=development
COLLECTOR_LISTEN=127.0.0.1:8787
COLLECTOR_DATA_DIR=./data
COLLECTOR_CATALOG_DIR=./catalog
LOCAL_DATABASE_PATH=./data/database/market-state.json
LOG_FORMAT=text
LOG_COLOR=auto
```

`COLLECTOR_LISTEN` debe mantenerse en loopback, salvo que conscientemente habilites acceso remoto con `COLLECTOR_ALLOW_REMOTE=true`.

## API local

```env
LOCAL_API_ALLOWED_ORIGINS=http://127.0.0.1:5173,http://localhost:5173
LOCAL_API_URL=http://127.0.0.1:8787/api/v1
```

La calculadora debe apuntar a `LOCAL_API_URL` para leer precios e historial locales.

## Durabilidad y retención

```env
STORAGE_MAX_BYTES=10737418240
STORAGE_RAW_RETENTION_DAYS=30
STORAGE_NORMALIZED_RETENTION_DAYS=365
STORAGE_BACKUP_DIR=./backups
STORAGE_BACKUP_RETENTION_DAYS=30
STORAGE_MINIMUM_BACKUPS=3
```

No borres manualmente `data/outbox` si hay pendientes hacia la API central.

## Forwarding hacia albion-market-api

```env
UPSTREAM_ENABLED=true
UPSTREAM_HISTORY_ENABLED=true
UPSTREAM_BASE_URL=http://127.0.0.1:8080
UPSTREAM_TOKEN=
UPSTREAM_TOKEN_FILE=./secrets/upstream-current.token
UPSTREAM_MIN_TOKEN_LENGTH=32
UPSTREAM_REQUIRE_HTTPS=false
```

Usa `UPSTREAM_TOKEN_FILE` en vez de poner el token en `UPSTREAM_TOKEN`.

## Outbox

```env
UPSTREAM_OUTBOX_PATH=./data/outbox/state.json
UPSTREAM_RETRY_COUNT=3
UPSTREAM_RETRY_DELAY=500ms
UPSTREAM_MAX_DELIVERY_ATTEMPTS=12
UPSTREAM_MAX_RETRY_DELAY=5m
```

La outbox permite seguir capturando aunque la API central esté caída.
