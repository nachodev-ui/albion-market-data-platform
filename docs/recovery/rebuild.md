# Reconstrucción

La reconstrucción regenera la proyección local desde los archivos normalizados o desde los crudos.

## Cuándo reconstruir

Reconstruye cuando:

- cambia `catalog/items.txt`;
- cambia `catalog/markets.json`;
- cambia la normalización;
- se corrige una asociación de ubicación;
- se actualiza el formato local;
- necesitas recuperar `data/database/market-state.json`.

## Rebuild de base local

Desde paquete release:

```powershell
.\tools\albion-market-rebuilddb.exe `
    -normalized-dir .\data\normalized `
    -database .\data\database\market-state.json `
    -reset
```

Desde checkout de desarrollo:

```powershell
.\scripts\rebuild-database.ps1
```

## Reprocesar desde raw

Desde paquete release:

```powershell
.\tools\albion-market-reprocess.exe `
    -input-dir .\data\raw `
    -output-dir .\data\normalized `
    -catalog-dir .\catalog

.\tools\albion-market-rebuilddb.exe `
    -normalized-dir .\data\normalized `
    -database .\data\database\market-state.json `
    -reset
```

Desde checkout de desarrollo:

```powershell
.\scripts\reprocess.ps1 -Rebuild
```

## Precaución

Antes de `-Rebuild`, crea backup. No borres `data/outbox` como parte de una reconstrucción de precios; la outbox representa envíos pendientes hacia la API central.
