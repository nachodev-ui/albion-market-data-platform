# Cierre de proyecto y validación

## Validación local obligatoria

Desde `apps/collector`:

```powershell
go test -race ./...
go vet ./...
go build ./cmd/receiver
```

Desde la raíz del repositorio:

```powershell
.\scripts\check.ps1
```

## Validación E2E de tres proyectos

Usa una base dedicada con `e2e` o `test` en el nombre:

```powershell
.\scripts\e2e-three-projects.ps1 `
    -DatabaseUrl "postgres://postgres:TU_CLAVE@localhost:5432/albion_market_e2e?sslmode=disable"
```

El script valida integración entre:

- `albion-market-data-platform`;
- `albion-market-api`;
- `albion-craft-calculator`;
- PostgreSQL.

## Flujo de cierre

1. Rama final hacia `develop`.
2. CI completo.
3. Merge a `develop`.
4. PR `develop → main`.
5. CI completo.
6. Merge a `main`.
7. Tag SemVer.
8. GitHub Release.
9. Verificación de ejecutable, SBOM y checksums.
10. Verificación de documentación publicada en GitHub Pages.

## Evidencia esperada

- checks de GitHub Actions en success;
- salida de `go test -race ./...`;
- salida de `go vet ./...`;
- salida de `go build ./cmd/receiver`;
- artefactos de `.e2e/artifacts`;
- assets del release con checksums y SBOM.
