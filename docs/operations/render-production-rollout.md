# Rollout del receiver hacia Render

Fecha de preparación: 2026-07-10.

## Topología

```text
Albion Online
  → Albion Data Client
  → receiver local http://127.0.0.1:8787
  → https://albion-market-api.onrender.com
  → Neon PostgreSQL
```

## Controles aplicados

- receiver expuesto únicamente en loopback;
- `APP_ENV=production` y carga explícita de `.env`;
- HTTPS obligatorio para el upstream;
- precios e historial habilitados;
- token leído desde `secrets/upstream-current.token`;
- token ausente de plantillas, documentación y variables inline;
- outbox persistente, reintentos acumulados y dead-letter;
- logs JSON y evidencia antes/después;
- verificador real para Albion Data Client → receiver → Render;
- validación del perfil con PowerShell en Windows CI.

## Validación automatizada previa al release

- Go quality checks;
- contratos de la API local;
- performance smoke;
- baseline local con 20 muestras y presupuestos sin ampliar;
- round trip de PostgreSQL;
- durabilidad en Ubuntu y Windows;
- distribución nativa de Windows;
- documentación;
- perfil de producción de Render.

## Línea base de Neon

Antes de la primera captura productiva se registraron cero filas en:

- `market_ingest_raw`;
- `market_ingest_requests`;
- `current_market_prices`;
- `market_history_ingest_raw`;
- `market_history_ingest_requests`;
- `market_history_buckets`.

La primera ejecución de `scripts/verify-render-pipeline.ps1` debe generar una
variación observable sobre esta línea base.

## Criterio de cierre operativo

La validación en el equipo que ejecuta Albion Online debe confirmar:

1. nuevas capturas y entradas en el receiver;
2. al menos un batch enviado con éxito;
3. actualización del timestamp del último envío exitoso;
4. ninguna entrada nueva en dead-letter;
5. filas nuevas en Neon;
6. lectura de los datos mediante la API pública.

La evidencia local queda en `.e2e/artifacts/render-pipeline-*` y no debe incluir
credenciales.
