# Prueba end-to-end formal de los tres proyectos

Este hito valida el flujo completo:

```text
Albion Data Client / payload compatible
→ albion-market-data-platform
→ outbox persistente
→ albion-market-api
→ PostgreSQL
→ albion-craft-calculator
```

## Preparación

Los repositorios deben ser carpetas hermanas:

```text
workspace/
├─ albion-craft-calculator/
├─ albion-market-api/
└─ albion-market-data-platform/
```

Crea una base PostgreSQL exclusiva para la prueba, por ejemplo
`albion_market_e2e`. El script rechaza por defecto una base cuyo nombre no
contenga `e2e` o `test`, porque aplica migraciones y vacía las tablas de mercado.

Requisitos en `PATH`:

- Go 1.23 o posterior;
- pnpm;
- `psql`;
- PostgreSQL activo.

## Ejecución

Desde `albion-market-data-platform`:

```powershell
Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass
.\scripts\e2e-three-projects.ps1 `
  -DatabaseUrl "postgres://postgres:TU_CLAVE@localhost:5432/albion_market_e2e?sslmode=disable"
```

Para omitir temporalmente `test/lint/build/docs:build` y ejecutar solo la integración:

```powershell
.\scripts\e2e-three-projects.ps1 `
  -DatabaseUrl $env:E2E_DATABASE_URL `
  -SkipQuality
```

## Alcance del Albion Data Client

La parte automatizada reproduce los contratos HTTP reales que Albion Data
Client envía a `/marketorders.ingest` y `/markethistories.ingest`. Esto permite
repetir los mismos datos, comprobar idempotencia y provocar caídas de forma
determinista sin depender de que el juego genere tráfico durante la prueba.

La aceptación del ejecutable real sigue siendo un smoke test operativo: inicia
una sesión con `scripts/start-session.ps1`, cambia de zona o abre un mercado y
confirma en `/api/v1/status` que aumenten los contadores de precios e historial.
Ese paso valida la captura externa; el arnés valida de forma reproducible todo
el flujo desde el contrato recibido hasta React.

## Aislamiento

El arnés usa:

- API central: `127.0.0.1:18080`;
- receiver local: `127.0.0.1:18787`;
- binarios temporales y directorio de trabajo: `.e2e/runtime`;
- datos y outbox: `.e2e/runtime/receiver-data`;
- token de ingesta exclusivo de la ejecución;
- base PostgreSQL dedicada indicada por el usuario.

No reutiliza `data/` ni los puertos diarios `8080/8787`. La API se ejecuta
desde un directorio temporal, por lo que un `.env.local` de desarrollo no puede
sobrescribir la base, el token ni los puertos definidos por el arnés.

## Escenarios automatizados

1. Puertas de calidad de los tres repositorios.
2. Aplicación ordenada de migraciones en una base aislada.
3. Build y arranque de binarios aislados de API y receiver.
4. Captura de precios e historial desde el receiver hasta PostgreSQL.
5. Lectura del frontend desde la API central.
6. Contrato público por `marketKey`, sin `location_id` ni `locationId`.
7. Batching histórico de varios candidatos en una solicitud central.
8. Idempotencia de batches de precios e historial.
9. Corrección de un bucket por una captura más nueva.
10. Fallback real del frontend al receiver con la API apagada.
11. Persistencia de pendientes al detener y reiniciar el receiver.
12. Envío automático al recuperar la API central.
13. Fallback de catálogo, precios e historial a caché con ambas fuentes apagadas.
14. Movimiento a `dead_letter` ante un token permanentemente inválido.
15. Separación de precios, historial, outbox, reintentos y errores en status.

## Evidencia

Cada ejecución crea:

```text
.e2e/artifacts/<fecha-hora>/
├─ RESULTS.md
├─ results.json
├─ api-01.stdout.log … api-03.stdout.log
├─ api-01.stderr.log … api-03.stderr.log
├─ receiver-01.stdout.log … receiver-03.stdout.log
└─ receiver-01.stderr.log … receiver-03.stderr.log
```

`RESULTS.md` es el acta legible del hito. Los logs permiten investigar un fallo
sin mezclarlo con la sesión diaria del receiver.
