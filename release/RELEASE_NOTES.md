# Albion Market Data Platform v0.1.1

Release de mantenimiento para el receiver local de Windows.

## Corrección principal

- conserva `LocationId = 3003` como **Black Market**;
- mantiene `LocationId = 3005` como **mercado regular de Caerleon**;
- evita que las órdenes de compra capturadas en el Black Market sean reclasificadas como Caerleon;
- mantiene la canonicalización existente para las demás ciudades.

## Validación

La versión fue validada mediante:

- pruebas unitarias y de regresión de catálogo y normalización;
- contratos de la API local;
- baseline y smoke tests de rendimiento;
- round trip con PostgreSQL;
- pruebas de durabilidad;
- instalación real del paquete Windows generado por CI.

Los datos productivos afectados también fueron reparados en Neon: 11.440 observaciones crudas y 10.417 snapshots actuales quedaron asociados correctamente a `3003`, sin discrepancias respecto del audit trail.

## Instalación en Windows

Descarga `albion-market-data-platform-v0.1.1-windows-amd64.zip`, descomprímelo en una carpeta nueva y ejecuta:

```powershell
Copy-Item .env.example .env
.\albion-market-receiver.exe --version
.\scripts\receiver.ps1
```

Para actualizar una instalación anterior, conserva antes tus carpetas `data` y `secrets`, además de tu archivo `.env`, y cópialos a la nueva carpeta. No copies el ejecutable antiguo sobre esta versión.

El paquete no requiere Go instalado. Incluye checksums SHA-256, SBOM SPDX y attestations de GitHub para verificar el build.
