# Conexión con albion-market-api

El receiver reenvía precios actuales e historial normalizado a la API central. En
producción, la API pública es:

```text
https://albion-market-api.onrender.com
```

`UPSTREAM_BASE_URL` debe contener la raíz del servicio, **sin** `/api/v1`. El
receiver agrega internamente las rutas de ingesta correspondientes.

## Configuración automática recomendada

Desde la raíz de `albion-market-data-platform`:

```powershell
Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass
.\scripts\configure-render-production.ps1
```

El script:

1. localiza el token de ingesta del checkout hermano de `albion-market-api`;
2. comprueba `/healthz` y `/readyz` en Render;
3. copia el token a `secrets/upstream-current.token` sin imprimirlo;
4. crea o actualiza `.env` y respalda el anterior;
5. habilita precios e historial con HTTPS obligatorio;
6. configura reintentos y timeout adecuados para una API remota.

También puede recibir una ruta explícita sin revelar el contenido:

```powershell
.\scripts\configure-render-production.ps1 `
    -TokenSourcePath "C:\ruta\segura\ingest-current.token"
```

No pongas el token directamente en `.env`. El perfil mantiene
`UPSTREAM_TOKEN=` vacío y usa solamente `UPSTREAM_TOKEN_FILE`.

## Perfil resultante

Las opciones críticas quedan así:

```env
APP_ENV=production
LOAD_DOTENV=true
UPSTREAM_ENABLED=true
UPSTREAM_HISTORY_ENABLED=true
UPSTREAM_BASE_URL=https://albion-market-api.onrender.com
UPSTREAM_TOKEN=
UPSTREAM_TOKEN_FILE=./secrets/upstream-current.token
UPSTREAM_MIN_TOKEN_LENGTH=32
UPSTREAM_REQUIRE_HTTPS=true
UPSTREAM_RETRY_COUNT=5
UPSTREAM_RETRY_DELAY=1s
UPSTREAM_MAX_DELIVERY_ATTEMPTS=12
UPSTREAM_MAX_RETRY_DELAY=5m
UPSTREAM_TIMEOUT=30s
```

El archivo `docs/examples/render-production.env.example` contiene el perfil
completo sin secretos y se incluye en la distribución nativa.

## Comprobación rápida

```powershell
Invoke-RestMethod https://albion-market-api.onrender.com/healthz
Invoke-RestMethod https://albion-market-api.onrender.com/readyz
Invoke-RestMethod http://127.0.0.1:8787/api/v1/status |
    ConvertTo-Json -Depth 20
```

Señales saludables:

- `price_forwarder.enabled=true`;
- `history_forwarder.enabled=true`;
- estado `idle` antes de capturas o `ok` después de enviar;
- `last_success_at` se actualiza;
- la profundidad de outbox vuelve a cero;
- no aparecen nuevos dead-letter.

## Validación real con Albion Data Client

```powershell
.\scripts\verify-render-pipeline.ps1
```

El verificador aplica la configuración, reinicia el receiver para cargarla,
inicia Albion Data Client con AODP y el destino local, y espera una captura real.
Durante la ejecución abre Albion Online y visita un mercado.

La evidencia se guarda en:

```text
.e2e/artifacts/render-pipeline-AAAAmmdd-HHMMSS/
```

El cierre exige capturas y entradas nuevas, un batch aceptado por Render, una
marca de último éxito actualizada y ningún dead-letter nuevo.

## Fallos comunes

- `401` o `403`: el token local no coincide con el configurado en Render.
- `5xx`: Render o PostgreSQL están degradados.
- `timeout`: la API no respondió dentro de `UPSTREAM_TIMEOUT`.
- outbox creciendo: la API no acepta batches o la conexión está interrumpida.
- estado `idle`: aún no se recibió una captura válida desde Albion Data Client.
