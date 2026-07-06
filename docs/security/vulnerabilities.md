# Respuesta a vulnerabilidades

## Alcance

Aplica a vulnerabilidades en:

- receiver local;
- tools de outbox, backfill, storage, reprocess y rebuild;
- workflows de release;
- dependencias Go y Node usadas para documentación;
- manejo de tokens y archivos locales.

## Clasificación

- **Crítica:** exposición de token, ejecución remota, corrupción de datos o pérdida silenciosa.
- **Alta:** bypass de autenticación upstream, escritura fuera de `data/`, filtrado de secretos en logs.
- **Media:** denegación de servicio local, métricas incorrectas, degradación sin alerta.
- **Baja:** documentación incorrecta o hardening menor.

## Respuesta

1. Confirmar impacto y versión afectada.
2. Evitar publicar detalles explotables antes del fix.
3. Crear rama de corrección.
4. Ejecutar validaciones.
5. Publicar PATCH SemVer.
6. Marcar versiones afectadas en release notes.
7. Recomendar actualización o mitigación.

## Validación mínima del fix

```powershell
.\scripts\check.ps1
```

Y para cierre completo:

```powershell
.\scripts\e2e-three-projects.ps1 -DatabaseUrl "postgres://.../albion_market_e2e?sslmode=disable"
```

## Secretos

Si un token pudo exponerse:

1. Rotar token en API y plataforma.
2. Reiniciar API.
3. Reiniciar receiver.
4. Retirar token anterior cuando los logs confirmen uso del nuevo `auth_key_id`.
