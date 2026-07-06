# Conexión con Albion Data Client

El receiver escucha capturas HTTP locales. Albion Data Client debe reenviar a AODP y al receiver.

## Comando recomendado

```powershell
& "C:\Program Files\Albion Data Client\albiondata-client.exe" `
  -i "https+pow://albion-online-data.com,http://127.0.0.1:8787"
```

Mantén abiertas la consola del receiver y Albion Data Client mientras visitas mercados.

## Flujo diario

1. Inicia el receiver.
2. Inicia Albion Data Client con el destino local.
3. Abre Albion Online.
4. Cambia de zona si el cliente aún no detecta ubicación.
5. Visita mercados y revisa ofertas/órdenes.
6. Consulta `GET /api/v1/status` o pulsa actualizar precios en la calculadora.

## Validación rápida

```powershell
Invoke-RestMethod http://127.0.0.1:8787/api/v1/status |
    ConvertTo-Json -Depth 10
```

Los contadores de capturas y entradas deben aumentar después de visitar mercados.
