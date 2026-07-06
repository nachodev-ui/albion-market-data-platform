# Albion Market Data Platform

Documentación operativa del receiver local de Albion Online.

El receiver corre de forma nativa en Windows, junto a Albion Data Client, conserva auditoría local, actualiza una base caliente de lectura y reenvía precios e historial hacia `albion-market-api`.

## Rutas principales

- [Instalación inicial](./guide/installation.md)
- [Configuración](./guide/configuration.md)
- [Operación diaria](./operations/index.md)
- [Backup y restauración](./recovery/backup-restore.md)
- [Releases y mantenimiento](./release/index.md)
- [Cierre de proyecto y validación](./testing/index.md)

## Distribución recomendada

Para usuarios finales en Windows, usa el `.zip` publicado en GitHub Releases. No requiere Go instalado para ejecutar el receiver.

Para desarrollo, usa el checkout del repositorio y los scripts de `scripts/`.
