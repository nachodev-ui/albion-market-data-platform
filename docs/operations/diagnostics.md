# Diagnóstico

## Salud HTTP

```powershell
Invoke-WebRequest http://127.0.0.1:8787/healthz
Invoke-RestMethod http://127.0.0.1:8787/api/v1/status |
    ConvertTo-Json -Depth 20

Invoke-RestMethod https://albion-market-api.onrender.com/healthz
Invoke-RestMethod https://albion-market-api.onrender.com/readyz
```

## Métricas

```powershell
$metrics = Invoke-WebRequest http://127.0.0.1:8787/metrics |
    Select-Object -ExpandProperty Content

$metrics
```

Revisa especialmente:

- `albion_receiver_captures_received_total`;
- `albion_receiver_entries_received_total`;
- `albion_receiver_forwarder_batches_sent_total`;
- `albion_receiver_upstream_last_success_timestamp_seconds`;
- `albion_receiver_outbox_depth`;
- `albion_receiver_dead_letter_batches_total`;
- `albion_receiver_storage_errors_total`;
- `albion_receiver_normalization_errors_total`.

Después de una visita real a un mercado deben aumentar capturas, entradas y
batches enviados. La profundidad de outbox debería regresar a cero.

## Verificación integral de producción

```powershell
Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass
.\scripts\verify-render-pipeline.ps1
```

Este comando reinicia el receiver con la configuración de Render, inicia Albion
Data Client y compara métricas y estado antes/después. La evidencia queda en
`.e2e/artifacts/render-pipeline-*`.

Un `summary.json` saludable contiene:

```json
{
  "success": true,
  "captures_delta": 1,
  "entries_delta": 1,
  "batches_sent_delta": 1,
  "dead_letter_delta": 0
}
```

Los valores reales pueden ser mayores porque una visita al mercado genera varias
capturas.

## Logs JSON

El perfil de producción usa:

```env
LOG_FORMAT=json
LOG_COLOR=never
```

Cada access log incluye `request_id`, método, ruta, estado y duración. Los eventos
`upstream.batch_sent` y `upstream.history_batch_sent` confirman aceptación por la
API central. Nunca publiques `.env`, archivos de `secrets/` ni cabeceras completas.

## Puerto ocupado

```powershell
Get-NetTCPConnection -LocalPort 8787 -ErrorAction SilentlyContinue |
    Select-Object LocalAddress, LocalPort, State, OwningProcess
```

El verificador puede reiniciar el proceso que escucha en 8787 después de confirmar
que el endpoint local corresponde al receiver.

## Verificador operativo local

Para probar únicamente la API local, sin Render ni Albion Data Client:

```powershell
.\scripts\verify-api.ps1
```

## Errores frecuentes

- `401` o `403`: token distinto al de Render;
- `upstream_transport`: conectividad o DNS;
- `timeout`: despertar de Render o red lenta;
- `outbox_depth` creciente: la API no está aceptando batches;
- `dead_letter` creciente: se agotaron los intentos acumulados;
- capturas en cero: Albion Data Client no está enviando al puerto 8787.

## Evidencia para reportar problemas

Incluye:

- versión del receiver: `.\albion-market-receiver.exe --version`;
- `summary.json` del verificador;
- copia de `/api/v1/status`;
- métricas relevantes;
- últimos logs del receiver;
- si aplica, estado de outbox sin secretos.
