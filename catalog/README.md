# Catálogo local

El servicio local es la única fuente de ubicaciones de mercado para el backend
y la calculadora React.

- `items.txt`: resuelve `AlbionId` a identificador y nombre del objeto.
- `markets.json`: define las claves estables, nombres y códigos observados de
  cada mercado.

Los identificadores se almacenan como texto. Esto es obligatorio para conservar
códigos con ceros iniciales, como Thetford (`0000` y `0007`).

## Mercados regulares verificados

| Clave | Ciudad | Zona central | Mercado |
|---|---|---:|---:|
| `bridgewatch` | Bridgewatch | `2000` | `2004` |
| `martlock` | Martlock | `3004` | `3008` |
| `lymhurst` | Lymhurst | `1000` | `1002` |
| `fort_sterling` | Fort Sterling | `4000` | `4002` |
| `thetford` | Thetford | `0000` | `0007` |
| `caerleon` | Caerleon | `3003` | `3005` |
| `brecilien` | Brecilien | `5000` | `5003` |

`black_market` queda declarado pero deshabilitado hasta capturar y verificar su
ubicación exacta. No se mezcla con el mercado regular de Caerleon.

Una ubicación desconocida no bloquea la captura: se conserva solamente su ID.
Después de modificar `markets.json`, ejecuta `scripts/reprocess.ps1 -Rebuild`.
