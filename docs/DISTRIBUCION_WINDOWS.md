# Distribución nativa reproducible para Windows

## Objetivo

El receiver se distribuye como paquete nativo para Windows. No requiere Docker ni
Go instalado en la máquina de destino.

## Artefacto principal

Cada tag SemVer `vX.Y.Z` publica un release con:

```text
albion-market-data-platform-vX.Y.Z-windows-amd64.zip
SHA256SUMS.txt
albion-market-data-platform-vX.Y.Z-windows-amd64.spdx.json
albion-market-receiver-vX.Y.Z-linux-amd64
```

El `.zip` de Windows contiene:

```text
albion-market-receiver.exe
tools/albion-market-outboxctl.exe
tools/albion-market-backfill-history.exe
tools/albion-market-storagectl.exe
tools/albion-market-reprocess.exe
tools/albion-market-rebuilddb.exe
scripts/
.env.example
catalog/
docs/
INSTALL.md
CHECKSUMS.sha256
SBOM.spdx.json
```

## Instalación limpia

```powershell
Expand-Archive .\albion-market-data-platform-vX.Y.Z-windows-amd64.zip `
    C:\AlbionMarketData

Set-Location C:\AlbionMarketData
Copy-Item .env.example .env
.\albion-market-receiver.exe --version
```

El comando `--version` imprime versión, commit, fecha de build, versión de Go y
si el árbol usado por el build estaba modificado.

## Scripts incluidos

Los scripts PowerShell incluidos en `scripts/` usan los binarios de `tools/` cuando están presentes. En un checkout de desarrollo, caen a `go run`.

## Build reproducible

El workflow de release usa:

- `CGO_ENABLED=0`;
- `-trimpath`;
- `-buildvcs=false`;
- `-ldflags "-s -w -buildid= ..."`;
- metadata fija desde el tag, commit y fecha del commit;
- `GOOS=windows`, `GOARCH=amd64` para el paquete principal.

La fecha de build se inyecta por `ldflags` desde el commit, no desde el reloj del
runner, para evitar variación innecesaria.

## Checksums y SBOM

El paquete incluye `CHECKSUMS.sha256` para los archivos internos. El release
incluye `SHA256SUMS.txt` para los artefactos publicados y un SBOM SPDX JSON.

## Attestations

En tags SemVer se generan attestations de GitHub para:

- provenance de los artefactos del release;
- SBOM del paquete Windows.

La evidencia se firma de forma keyless mediante GitHub artifact attestations y
Sigstore. Esto no equivale a una firma Authenticode del `.exe`.

## Firma Authenticode

El `.exe` no debe anunciarse como firmado con Authenticode hasta que exista un
certificado real de firma de código y un paso explícito de firma Windows.

## Publicación

Desde una rama `main` actualizada:

```bash
git tag vX.Y.Z
git push origin vX.Y.Z
```

El workflow `Native release distribution` compila, valida el paquete, genera
checksums, SBOM, attestations y publica el GitHub Release.
