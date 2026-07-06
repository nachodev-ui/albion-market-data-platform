# Instalación inicial

## Instalación desde GitHub Release

Descarga el archivo:

```text
albion-market-data-platform-vX.Y.Z-windows-amd64.zip
```

Instala en una carpeta estable:

```powershell
Expand-Archive .\albion-market-data-platform-vX.Y.Z-windows-amd64.zip `
    C:\AlbionMarketData

Set-Location C:\AlbionMarketData
Copy-Item .env.example .env
.\albion-market-receiver.exe --version
```

El comando `--version` debe imprimir versión, commit, fecha de build y versión de Go. Esto no requiere Go instalado.

## Instalación desde código fuente

Usa esta opción para desarrollo o cambios internos:

```bash
git clone https://github.com/nachodev-ui/albion-market-data-platform.git
cd albion-market-data-platform
git switch main
```

Luego en PowerShell:

```powershell
Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass
.\scripts\check.ps1
Copy-Item .env.example .env
.\scripts\receiver.ps1
```

## Requisitos externos

- Windows 10/11 o runner compatible.
- Albion Data Client instalado.
- `albion-market-api` disponible si se habilita forwarding.
- PostgreSQL solo es necesario para la API central, no para el receiver local.
