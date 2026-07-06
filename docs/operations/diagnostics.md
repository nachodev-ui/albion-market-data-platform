# Diagnóstico

## Salud HTTP

```powershell
Invoke-WebRequest http://127.0.0.1:8787/healthz
Invoke-RestMethod http://127.0.0.1:8787/api/v1/status |
    ConvertTo-Json -Depth 10
```

## Métricas

```powershell
Invoke-WebRequest http://127.0.0.1:8787/metrics |
    Select-Object -ExpandProperty Content
```

Revisa especialmente:

- `albion_receiver_storage_errors_total`
- `albion_receiver_normalization_errors_total`
- `albion_receiver_outbox_depth`
- `albion_receiver_forwarder_status`
- `albion_receiver_upstream_last_error_timestamp_seconds`

## Logs JSON

```powershell
$env:LOG_FORMAT = "json"
.\albion-market-receiver.exe
```

Cada access log incluye `request_id`, método, ruta, estado y duración.

## Puerto ocupado

```powershell
Get-NetTCPConnection -LocalPort 8787 -ErrorAction SilentlyContinue |
    Select-Object LocalAddress, LocalPort, State, OwningProcess
```

## Verificador operativo

Con el receiver corriendo:

```powershell
.\scripts\verify-api.ps1
```

## Evidencia para reportar problemas

Incluye:

- versión del receiver: `.\albion-market-receiver.exe --version`;
- copia de `/api/v1/status`;
- salida relevante de `/metrics`;
- últimos logs del receiver;
- si aplica, `data/outbox/state.json` sin secretos.
