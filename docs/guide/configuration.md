# Configuración

La configuración activa vive en `.env`. El repositorio contiene dos referencias:

- `.env.example`: desarrollo local con API central en `127.0.0.1:8080`;
- `docs/examples/render-production.env.example`: receiver local conectado a la API pública de Render.

Los archivos `.env`, `secrets/`, `*.token` y `*.secret` están ignorados por Git.

## Configuración local básica

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

`COLLECTOR_LISTEN` debe mantenerse en loopback, salvo que conscientemente
habilites acceso remoto con `COLLECTOR_ALLOW_REMOTE=true`.

## Perfil de producción con Render

La forma recomendada de crear el perfil es:

```powershell
Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass
.\scripts\configure-render-production.ps1
```

El configurador es idempotente. Si `.env` ya existe, conserva las opciones no
relacionadas y crea una copia `*.backup-AAAAMMDD-HHMMSS` antes de reemplazar los
valores críticos.

El resultado usa:

```env
APP_ENV=production
LOAD_DOTENV=true
LOG_FORMAT=json
LOG_COLOR=never
UPSTREAM_ENABLED=true
UPSTREAM_HISTORY_ENABLED=true
UPSTREAM_BASE_URL=https://albion-market-api.onrender.com
UPSTREAM_TOKEN=
UPSTREAM_TOKEN_FILE=./secrets/upstream-current.token
UPSTREAM_REQUIRE_HTTPS=true
UPSTREAM_TIMEOUT=30s
```

`UPSTREAM_BASE_URL` es la raíz de Render y no debe terminar en `/api/v1`.

## Token de ingesta

Usa `UPSTREAM_TOKEN_FILE`; no copies el token a `UPSTREAM_TOKEN`.

El configurador busca automáticamente, en este orden:

1. `secrets/upstream-current.token` dentro del receiver;
2. `../albion-market-api/secrets/deployment/ingest-current.token`;
3. el mismo archivo dentro del checkout de API ubicado en el Escritorio.

También puedes indicar una ruta explícita:

```powershell
.\scripts\configure-render-production.ps1 `
    -TokenSourcePath "C:\ruta\segura\ingest-current.token"
```

El valor se valida, se escribe en UTF-8 sin BOM ni salto final y nunca se muestra
en la consola. Para rotar el token, actualiza primero Render y vuelve a ejecutar
el configurador con el nuevo archivo.

## API local

```env
LOCAL_API_ALLOWED_ORIGINS=http://127.0.0.1:5173,http://localhost:5173
LOCAL_API_URL=http://127.0.0.1:8787/api/v1
```

La calculadora pública usa Render. La API local queda disponible para operación,
diagnóstico y fallback de desarrollo.

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

## Outbox y red remota

```env
UPSTREAM_OUTBOX_PATH=./data/outbox/state.json
UPSTREAM_RETRY_COUNT=5
UPSTREAM_RETRY_DELAY=1s
UPSTREAM_MAX_DELIVERY_ATTEMPTS=12
UPSTREAM_MAX_RETRY_DELAY=5m
UPSTREAM_TIMEOUT=30s
```

La outbox permite seguir capturando aunque Render esté temporalmente dormido,
reiniciándose o sin conexión. Los batches pendientes se recuperan al volver a
iniciar el receiver.

## Inicio con el perfil cargado

```powershell
.\scripts\receiver.ps1
```

`receiver.ps1` habilita `LOAD_DOTENV=true` durante el arranque cuando detecta un
`.env`, de modo que el perfil de producción se aplica incluso con el hardening de
`APP_ENV=production`.
