<div align="center">

# Albion Market Data Platform

**Receiver local, normalización y entrega confiable de datos de mercado para Albion Online.**

[Documentación](https://nachodev-ui.github.io/albion-market-data-platform/) · [Releases](https://github.com/nachodev-ui/albion-market-data-platform/releases) · [API central](https://albion-market-api.onrender.com)

</div>

---

Albion Market Data Platform recibe paquetes desde Albion Data Client, conserva evidencia local, normaliza precios e historial y los entrega a la API central mediante una outbox persistente. Está orientado a colaboradores y máquinas recolectoras; los usuarios de la aplicación web no necesitan instalarlo.

> [!NOTE]
> Este repositorio es la capa de captura de la plataforma. El frontend público consume datos desde `albion-market-api` y funciona independientemente del receiver local.

## Papel dentro de la plataforma

| Área | Responsabilidad |
|---|---|
| Captura | Recibir órdenes e historial desde Albion Data Client |
| Normalización | Convertir IDs, precios, timestamps, mercados y calidad a un modelo estable |
| Persistencia local | Conservar raw, JSONL normalizado y una proyección de consulta |
| Entrega | Reenviar precios e historial a la API central con autenticación |
| Recuperación | Mantener outbox durable, reintentos, reinicio seguro y dead-letter |
| Diagnóstico | Exponer API local, métricas Prometheus y estado detallado |

## Arquitectura del pipeline

```mermaid
flowchart LR
    ADC[Albion Data Client]
    Receiver[Receiver local\n127.0.0.1:8787]
    Raw[(data/raw)]
    Normalized[(data/normalized)]
    LocalDB[(market-state.json)]
    Outbox[(Outbox persistente)]
    API[albion-market-api\nRender]
    Neon[(Neon PostgreSQL)]
    LocalAPI[API local de consulta]

    ADC -->|órdenes e historial| Receiver
    Receiver --> Raw
    Receiver --> Normalized
    Receiver --> LocalDB
    Receiver --> Outbox
    Receiver --> LocalAPI
    Outbox -->|HTTPS + Bearer| API
    API --> Neon
```

## Garantías operativas

### Persistencia antes de entrega

Cada captura aceptada se conserva localmente antes de considerarse lista para reenvío. La API central puede estar temporalmente caída sin perder los batches pendientes.

### Recuperación automática

```mermaid
flowchart LR
    Capture[Captura normalizada]
    Persist[Escritura durable]
    Pending[Pending]
    Send[Intento de entrega]
    Success[Confirmado]
    Retry[Backoff y reintento]
    DLQ[Dead-letter]

    Capture --> Persist --> Pending --> Send
    Send -->|2xx| Success
    Send -->|error transitorio| Retry --> Pending
    Send -->|error permanente o límite agotado| DLQ
```

El mismo `request_id` se conserva durante reintentos y recuperación, permitiendo que la API central aplique idempotencia sin duplicar información.

### Separación de forwarders

Precios e historial mantienen estado y métricas independientes. Esto permite detectar si una corriente está saludable aunque la otra esté degradada.

## Inicio rápido recomendado

### Requisitos

- Windows con PowerShell;
- Albion Data Client;
- Go 1.23 o posterior para desarrollo desde código fuente.

La distribución nativa publicada en GitHub Releases no requiere Go para ejecutar el receiver.

### Sesión completa de desarrollo

Desde la raíz del repositorio:

```powershell
Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass
.\scripts\start-session.ps1
```

El script puede iniciar en cadena:

1. la API central local en `127.0.0.1:8080`;
2. el receiver en `127.0.0.1:8787`;
3. Albion Data Client con el receiver como destino;
4. la calculadora React mediante `pnpm dev`.

### Receiver solamente

```powershell
.\scripts\check.ps1
.\scripts\rebuild-database.ps1
.\scripts\receiver.ps1
```

En otra consola, inicia Albion Data Client con ambos destinos:

```powershell
& "C:\Program Files\Albion Data Client\albiondata-client.exe" `
  -i "https+pow://albion-online-data.com,http://127.0.0.1:8787"
```

Las consolas deben permanecer abiertas durante la captura. `Ctrl + C` detiene los procesos sin eliminar los datos guardados.

## Operación diaria

```text
1. Iniciar el receiver o ejecutar start-session.ps1
2. Abrir Albion Online
3. Cambiar de zona para confirmar detección de ubicación
4. Visitar los mercados y objetos necesarios
5. Revisar ofertas de venta y órdenes de compra
6. Consultar el estado local o actualizar la calculadora
```

Las capturas nuevas actualizan automáticamente:

```text
data/raw
data/normalized
data/database/market-state.json
data/outbox/state.json
```

No es necesario ejecutar una reconstrucción completa después de una sesión normal.

## Configuración del upstream

Configuración mínima para entregar datos a la API central:

```dotenv
UPSTREAM_ENABLED=true
UPSTREAM_HISTORY_ENABLED=true
UPSTREAM_BASE_URL=https://albion-market-api.onrender.com
UPSTREAM_TOKEN=
UPSTREAM_TOKEN_FILE=./secrets/upstream-current.token
UPSTREAM_MIN_TOKEN_LENGTH=32
UPSTREAM_OUTBOX_PATH=./data/outbox/state.json
UPSTREAM_MAX_DELIVERY_ATTEMPTS=12
UPSTREAM_MAX_RETRY_DELAY=5m
```

> [!IMPORTANT]
> Usa el archivo de token o la variable, nunca ambos. No confirmes credenciales, archivos `.env.local` ni contenido de `secrets/`.

La configuración completa está en [Guía de configuración](https://nachodev-ui.github.io/albion-market-data-platform/guide/configuration).

## API local

| Método y ruta | Propósito |
|---|---|
| `GET /healthz` | Estado básico del receiver |
| `GET /metrics` | Métricas Prometheus de captura, almacenamiento y forwarders |
| `GET /api/v1/status` | Estado operativo, outbox, reintentos y dead-letter |
| `GET /api/v1/markets` | Catálogo canónico de mercados |
| `GET /api/v1/prices` | Precio mínimo de venta y máximo de compra |
| `GET /api/v1/history` | Historial normalizado de 7 días o 4 semanas |
| `GET /api/v1/orders` | Última versión conocida de las órdenes capturadas |

Ejemplo de precios:

```text
http://127.0.0.1:8787/api/v1/prices?server=west&marketKey=brecilien&itemIds=T4_MAIN_CURSEDSTAFF_CRYSTAL%404&quality=4
```

Ejemplo de historial:

```text
http://127.0.0.1:8787/api/v1/history?server=west&marketKey=brecilien&itemId=T4_MAIN_CURSEDSTAFF_CRYSTAL%404&quality=4&period=4-weeks
```

Validación rápida:

```powershell
.\scripts\verify-api.ps1

Invoke-RestMethod http://127.0.0.1:8787/api/v1/status |
    ConvertTo-Json -Depth 10
```

## Outbox y dead-letter

La outbox durable vive en:

```text
data/outbox/state.json
```

Operaciones disponibles:

```powershell
.\scripts\outbox-dead-letter.ps1 -Action list
.\scripts\outbox-dead-letter.ps1 -Action requeue -RequestId <UUID>
.\scripts\outbox-dead-letter.ps1 -Action purge -RequestId <UUID>
```

Prueba formal de caída, reinicio y recuperación:

```powershell
.\scripts\verify-outbox-recovery.ps1
```

Consulta [Outbox y backfill](https://nachodev-ui.github.io/albion-market-data-platform/OUTBOX_Y_BACKFILL) antes de purgar o reencolar elementos.

## Backfill histórico

Revisa primero el contenido que se enviará:

```powershell
.\scripts\backfill-history.ps1 -DryRun
```

Envía los últimos 28 días:

```powershell
.\scripts\backfill-history.ps1
```

Para todo el historial normalizado disponible:

```powershell
.\scripts\backfill-history.ps1 -All -DryRun
.\scripts\backfill-history.ps1 -All
```

Los `request_id` son deterministas por contenido; repetir el mismo backfill no duplica buckets en la API central.

## Reconstrucción y recuperación local

Una reconstrucción completa se reserva para cambios de catálogo, reglas de normalización, asociaciones de ubicación o formatos persistidos.

```powershell
# Detener primero el receiver con Ctrl + C
.\scripts\reprocess.ps1 -Rebuild
.\scripts\receiver.ps1
```

Para reconstruir solo la proyección desde los normalizados:

```powershell
.\scripts\rebuild-database.ps1
```

Guías relacionadas:

- [Backup y restauración](https://nachodev-ui.github.io/albion-market-data-platform/recovery/backup-restore)
- [Reconstrucción](https://nachodev-ui.github.io/albion-market-data-platform/recovery/rebuild)
- [Runbook degradado](https://nachodev-ui.github.io/albion-market-data-platform/RUNBOOK_RECEIVER_DEGRADED)

## Prueba end-to-end de los tres proyectos

El arnés formal valida receiver, outbox, API central, PostgreSQL y frontend usando puertos y base de datos aislados:

```powershell
.\scripts\e2e-three-projects.ps1 `
  -DatabaseUrl "postgres://postgres:TU_CLAVE@localhost:5432/albion_market_e2e?sslmode=disable"
```

La prueba cubre:

- ingesta de precios e historial;
- idempotencia;
- corrección de buckets;
- fallbacks del frontend;
- recuperación tras reinicio;
- dead-letter y requeue;
- evidencia reproducible en `.e2e/artifacts`.

Documentación: [E2E de los tres proyectos](https://nachodev-ui.github.io/albion-market-data-platform/E2E_TRES_PROYECTOS).

## Estructura del repositorio

```text
.
├─ apps/collector/
│  ├─ cmd/receiver/          proceso principal
│  ├─ cmd/reprocess/         normalización desde raw
│  ├─ cmd/rebuilddb/         reconstrucción de la proyección
│  ├─ cmd/backfillhistory/   backfill idempotente
│  ├─ cmd/outboxctl/         operación de dead-letter
│  └─ internal/
│     ├─ httpapi/            API local
│     ├─ httpingest/         recepción desde Albion Data Client
│     ├─ normalization/      transformación de paquetes
│     ├─ observability/      logs y métricas
│     ├─ upstream/           outbox, reintentos y forwarders
│     └─ storage/            raw, JSONL y proyección local
├─ catalog/
│  ├─ items.txt
│  └─ markets.json
├─ data/
│  ├─ raw/
│  ├─ normalized/
│  ├─ database/
│  ├─ outbox/
│  └─ test/
├─ docs/                    portal VitePress
└─ scripts/                 operación y validación en Windows
```

## Normalización

- resolución de `AlbionId` a identificadores estables;
- conversión de valores monetarios desde paquetes reales;
- conversión de ticks .NET a UTC;
- asignación canónica de mercado y calidad;
- deduplicación de snapshots de órdenes e historial;
- exclusión de órdenes expiradas en consultas de precios;
- conservación de ubicaciones desconocidas sin mezclarlas con mercados verificados.

## Calidad y observabilidad

```powershell
.\scripts\check.ps1
.\scripts\verify-api.ps1
.\scripts\verify-history-forwarder.ps1
.\scripts\verify-outbox-recovery.ps1
```

`/metrics` expone contadores, latencias, profundidad de outbox, estado de forwarders, bytes persistidos, uptime y metadatos del build. `/api/v1/status` entrega una vista detallada para diagnóstico humano.

## Documentación

El portal operativo se publica en [GitHub Pages](https://nachodev-ui.github.io/albion-market-data-platform/).

| Tema | Enlace |
|---|---|
| Instalación | [Guía inicial](https://nachodev-ui.github.io/albion-market-data-platform/guide/installation) |
| Albion Data Client | [Configuración del cliente](https://nachodev-ui.github.io/albion-market-data-platform/guide/albion-data-client) |
| Operación | [Inicio, detención y diagnóstico](https://nachodev-ui.github.io/albion-market-data-platform/operations/) |
| Recuperación | [Backup, rebuild y limpieza](https://nachodev-ui.github.io/albion-market-data-platform/recovery/) |
| Seguridad | [Secretos y rotación](https://nachodev-ui.github.io/albion-market-data-platform/SEGURIDAD_SECRETOS) |
| Releases | [Actualización y rollback](https://nachodev-ui.github.io/albion-market-data-platform/release/) |

## Repositorios relacionados

| Repositorio | Función |
|---|---|
| [`albion-market-api`](https://github.com/nachodev-ui/albion-market-api) | API central y persistencia PostgreSQL |
| [`albion-production-calculator`](https://github.com/nachodev-ui/albion-production-calculator) | Frontend público de cálculo y análisis |

## Estado del proyecto

El receiver y sus mecanismos de recuperación están completos para el alcance actual. La fase operativa se centra en ejecución prolongada, revisión de dead-letter, backup y restauración, y publicación de releases estables.