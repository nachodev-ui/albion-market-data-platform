# Guía

Esta guía cubre instalación inicial, configuración y conexión con los otros componentes del flujo Albion.

## Flujo esperado

```text
Albion Data Client
        ↓
albion-market-receiver.exe
        ↓
data/raw + data/normalized + data/database + data/outbox
        ↓
albion-market-api + PostgreSQL
        ↓
albion-craft-calculator
```

## Modos de uso

- **Instalación nativa Windows:** descarga el release `.zip`, copia `.env.example` a `.env` y ejecuta `albion-market-receiver.exe`.
- **Desarrollo:** usa el repositorio completo, Go y scripts PowerShell.

La distribución primaria es Windows nativo; Docker no es el mecanismo principal para el receiver local.
