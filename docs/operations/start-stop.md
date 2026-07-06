# Inicio y detención

## Instalación release

```powershell
Set-Location C:\AlbionMarketData
.\albion-market-receiver.exe
```

Para cambiar configuración temporalmente:

```powershell
$env:LOG_FORMAT = "json"
.\albion-market-receiver.exe
```

## Checkout de desarrollo

```powershell
Set-Location C:\Users\mitsf\Desktop\albion-market-data-platform
.\scripts\receiver.ps1
```

El script usa el binario `albion-market-receiver.exe` si existe en la raíz del paquete; si no existe, cae a `go run` para desarrollo.

## Detención normal

Presiona `Ctrl + C` en la consola del receiver y espera a que termine.

## Detención de emergencia

Solo si el proceso no responde:

```powershell
Get-Process albion-market-receiver -ErrorAction SilentlyContinue | Stop-Process -Force
```

Después revisa la outbox:

```powershell
.\scripts\outbox-dead-letter.ps1 -Action list
```
