# Paso 6: distribución nativa reproducible

Este cambio agrega distribución nativa para Windows sin Docker como mecanismo
primario.

## Incluye

- `albion-market-receiver.exe --version`
- build con metadata por ldflags
- paquete Windows amd64 en zip
- binarios de outbox y backfill dentro de `tools/`
- scripts PowerShell, `.env.example`, catálogo y documentación mínima
- checksums internos y checksums de release
- SBOM SPDX JSON
- GitHub artifact attestations para provenance y SBOM en tags SemVer
- publicación de GitHub Release desde tags `vX.Y.Z`

## Nota

El ejecutable no se anuncia como firmado con Authenticode. La evidencia de build
se entrega mediante checksums, SBOM y attestations keyless.
