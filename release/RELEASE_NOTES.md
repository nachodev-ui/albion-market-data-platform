# Albion Market Data Platform v0.1.2

Release de corrección para evitar pérdidas de precios e historial durante las
ráfagas simultáneas generadas por Albion Data Client.

## Corrección principal

- elimina el rechazo inmediato `429 Too Many Requests` cuando se ocupan todos los
  slots de normalización;
- lee, valida y persiste cada payload en el almacenamiento raw antes de aplicar
  backpressure;
- mantiene las solicitudes en espera hasta que exista capacidad de procesamiento;
- continúa normalizando aunque el cliente HTTP desconecte después de entregar el
  body;
- conserva el límite concurrente para proteger CPU y disco;
- permite configurar ese límite mediante
  `COLLECTOR_INGEST_MAX_CONCURRENT` o `--ingest-max-concurrent`;
- amplía el `WriteTimeout` del servidor local a 60 segundos para absorber ráfagas
  válidas sin cortar la respuesta;
- añade logs de espera y la cabecera `X-Ingest-Queue-Wait-Ms`.

## Incidente corregido

La versión `v0.1.1` respondía `429` antes de leer el body cuando cuatro solicitudes
ya estaban en curso. Albion Data Client no garantizaba el reintento de esos
payloads, por lo que algunas órdenes y capturas históricas no llegaban al
almacenamiento raw, al forwarder, a Render ni a Neon.

`stored=false` continúa significando duplicado local; no es un fallo de historial.
Las capturas aceptadas siguen reenviándose normalmente cuando
`forwarded=1` y `dropped=0`.

## Validación

La versión incorpora una prueba concurrente de regresión que demuestra que:

1. la primera petición puede ocupar el único slot disponible;
2. la segunda petición se persiste en raw;
3. la segunda espera en vez de recibir `429`;
4. ambas terminan correctamente después de liberar el slot.

Además, la matriz protegida valida calidad Go, contratos, rendimiento,
PostgreSQL, durabilidad, documentación y distribución nativa de Windows.

## Instalación en Windows

Descarga `albion-market-data-platform-v0.1.2-windows-amd64.zip`, descomprímelo
en una carpeta nueva y conserva desde tu instalación anterior:

- `.env`;
- `data/`;
- `secrets/`.

Después ejecuta:

```powershell
.\albion-market-receiver.exe --version
.\scripts\receiver.ps1
```

No copies el ejecutable antiguo sobre esta versión. El paquete no requiere Go
instalado e incluye checksums SHA-256, SBOM SPDX y attestations de GitHub.
