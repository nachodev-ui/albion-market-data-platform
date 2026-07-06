# Verificación de release

## Assets esperados

Para `vX.Y.Z`, el GitHub Release debe incluir:

```text
albion-market-data-platform-vX.Y.Z-windows-amd64.zip
albion-market-data-platform-vX.Y.Z-windows-amd64.spdx.json
albion-market-receiver-vX.Y.Z-linux-amd64
SHA256SUMS.txt
```

## Verificar checksums

En PowerShell:

```powershell
Get-FileHash .\albion-market-data-platform-vX.Y.Z-windows-amd64.zip -Algorithm SHA256
Get-Content .\SHA256SUMS.txt
```

Compara el hash del zip con la línea correspondiente.

## Verificar instalación

```powershell
Expand-Archive .\albion-market-data-platform-vX.Y.Z-windows-amd64.zip `
    C:\AlbionMarketDataTest

Set-Location C:\AlbionMarketDataTest
Copy-Item .env.example .env
.\albion-market-receiver.exe --version
```

La versión debe coincidir con el tag.

## Verificar SBOM

El archivo `.spdx.json` debe abrir como JSON válido:

```powershell
Get-Content .\albion-market-data-platform-vX.Y.Z-windows-amd64.spdx.json -Raw |
    ConvertFrom-Json | Out-Null
```

## Verificar attestations

En GitHub, revisa la sección de attestations del release o usa GitHub CLI cuando esté disponible:

```bash
gh attestation verify albion-market-data-platform-vX.Y.Z-windows-amd64.zip \
  --repo nachodev-ui/albion-market-data-platform
```

Las attestations son evidencia keyless de build; no sustituyen una firma Authenticode del `.exe`.
