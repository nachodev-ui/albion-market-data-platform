# Conexión con Albion Data Client

Albion Data Client captura tráfico del juego y envía las capturas al receiver
local. El receiver conserva auditoría, normaliza, actualiza su base local y
reenvía precios e historial a Render.

## Flujo de producción

```text
Albion Online
    ↓
Albion Data Client
    ↓
http://127.0.0.1:8787
    ↓
receiver + almacenamiento + outbox
    ↓
https://albion-market-api.onrender.com
    ↓
Neon PostgreSQL
```

## Arranque y verificación recomendados

Desde la raíz del receiver:

```powershell
Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass
.\scripts\verify-render-pipeline.ps1
```

El script realiza automáticamente:

1. configuración segura del token y `.env`;
2. comprobación de health y readiness de Render;
3. reinicio del receiver para cargar el perfil de producción;
4. inicio de Albion Data Client con AODP y el receiver local;
5. captura del estado y métricas anteriores;
6. espera de una captura real y un batch aceptado por Render;
7. generación de evidencia antes/después.

Durante la espera, abre Albion Online, cambia de zona si el cliente aún no conoce
la ubicación y visita un mercado. Revisa ofertas u órdenes para provocar la
captura.

La validación puede esperar diez minutos por defecto. Para ampliar la ventana:

```powershell
.\scripts\verify-render-pipeline.ps1 -TimeoutSeconds 900
```

## Inicio manual de Albion Data Client

El destino recomendado sigue enviando a AODP y añade el receiver local:

```powershell
& "C:\Program Files\Albion Data Client\albiondata-client.exe" `
    -i "https+pow://albion-online-data.com,http://127.0.0.1:8787"
```

El verificador reinicia por defecto una instancia existente para garantizar que
use esos destinos. Usa `-KeepExistingAlbionDataClient` únicamente cuando ya
confirmaste su configuración.

## Receiver existente

El verificador reinicia por defecto el proceso que escucha en el puerto 8787 para
aplicar el `.env` recién escrito. Para conservar deliberadamente una instancia ya
configurada:

```powershell
.\scripts\verify-render-pipeline.ps1 -KeepExistingReceiver
```

## Evidencia

Cada ejecución crea:

```text
.e2e/artifacts/render-pipeline-AAAAmmdd-HHMMSS/
```

Incluye:

- `summary.json`;
- estado del receiver antes y después;
- estado de Render antes y después;
- métricas Prometheus antes y después;
- contadores de ingesta observables;
- logs de procesos iniciados por el script.

El cierre exige:

- aumento de capturas y entradas;
- aumento de batches enviados;
- actualización del último envío exitoso;
- ausencia de nuevos dead-letter;
- aumento de contadores de Render cuando el endpoint los expone.

## Validación rápida manual

```powershell
$status = Invoke-RestMethod http://127.0.0.1:8787/api/v1/status
$status.price_forwarder
$status.history_forwarder

Invoke-WebRequest http://127.0.0.1:8787/metrics |
    Select-Object -ExpandProperty Content
```

Los contadores deben aumentar después de visitar mercados y la outbox debe volver
a cero cuando Render acepta los batches.
