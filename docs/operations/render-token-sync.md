# Sincronización del token de ingesta con Render

Este procedimiento alinea el token local usado por el receiver con la variable `INGEST_BEARER_TOKEN` del servicio `albion-market-api` en Render.

## Cuándo usarlo

Úsalo cuando el receiver capture datos correctamente, pero el forwarder registre respuestas `401 unauthorized` y envíe batches a dead-letter.

## Requisitos

- un API key de Render con acceso al servicio;
- el token local en `secrets/upstream-current.token`, o una ruta indicada mediante `-TokenSourcePath`;
- PowerShell 5.1 o PowerShell 7.

El API key de Render se solicita como valor oculto. El script no imprime el API key ni el token de ingesta.

## Ejecución

```powershell
Set-Location "C:\Users\mitsf\Desktop\albion-market-data-platform"

Set-ExecutionPolicy `
    -Scope Process `
    -ExecutionPolicy Bypass `
    -Force

.\scripts\sync-render-ingest-token.ps1
```

El script:

1. localiza el servicio `albion-market-api`;
2. actualiza solamente `INGEST_BEARER_TOKEN`;
3. no reemplaza las demás variables del servicio;
4. ejecuta un deploy de configuración;
5. espera hasta que el deploy quede `live`;
6. comprueba `/readyz`;
7. realiza una solicitud inválida pero autenticada a `/api/v1/prices` para confirmar que el token ya no recibe `401`.

Resultado esperado:

```text
Token de ingesta sincronizado y aceptado por Render.
```

Después ejecuta nuevamente:

```powershell
.\scripts\verify-render-pipeline.ps1 -TimeoutSeconds 900
```

## Seguridad

- no pegues el API key ni el token en tickets, PR, logs o chat;
- no agregues `.env`, `*.token` ni el directorio `secrets` a Git;
- revoca el API key de Render cuando deje de ser necesario;
- la sonda autenticada no inserta precios: envía un cuerpo vacío que debe fallar en validación después de superar autenticación.
