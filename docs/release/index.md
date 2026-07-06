# Política de releases

## SemVer

El proyecto usa tags SemVer con prefijo `v`:

```text
vMAJOR.MINOR.PATCH
vMAJOR.MINOR.PATCH-prerelease
```

Ejemplos:

```text
v0.1.0
v0.2.0
v1.0.0-rc.1
```

## Criterio de cambio

- **PATCH:** correcciones compatibles, documentación y mejoras de scripts sin cambio de contrato.
- **MINOR:** nuevas capacidades compatibles, nuevos endpoints, nuevos tools o métricas.
- **MAJOR:** cambios incompatibles de formato persistido, contrato HTTP, flags o variables obligatorias.

Antes de `v1.0.0`, los cambios incompatibles deben quedar documentados claramente en release notes.

## Versiones soportadas

Se soportan la última versión estable y la versión inmediatamente anterior durante una ventana corta de migración.

## Retención de releases

- Mantener todos los tags SemVer.
- Mantener los assets de GitHub Release de versiones estables.
- No reemplazar assets de un tag ya publicado; crear un PATCH nuevo.

## Evidencia obligatoria

Cada release debe incluir zip Windows amd64, `SHA256SUMS.txt`, SBOM SPDX JSON, attestations y notas de instalación.
