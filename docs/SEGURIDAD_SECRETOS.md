# Secretos del forwarder y rotación de ingesta

El receiver y el backfill pueden leer el token de la API central desde una
variable de entorno o desde un archivo. El archivo es la alternativa
recomendada porque evita exponer el token en argumentos de proceso y facilita
usar Docker secrets, Kubernetes Secrets o credenciales administradas.

## Configuración recomendada

En `.env` local:

```env
UPSTREAM_TOKEN=
UPSTREAM_TOKEN_FILE=./secrets/upstream-current.token
UPSTREAM_MIN_TOKEN_LENGTH=32
```

En producción:

```env
APP_ENV=production
UPSTREAM_BASE_URL=https://api.example.com
UPSTREAM_REQUIRE_HTTPS=true
UPSTREAM_TOKEN_FILE=/run/secrets/albion_ingest_token
```

`UPSTREAM_TOKEN` y `UPSTREAM_TOKEN_FILE` son mutuamente excluyentes. El receiver
rechaza tokens cortos, placeholders, espacios y archivos de permisos demasiado
amplios en Linux productivo.

El cliente HTTP no sigue redirecciones. Esto evita reenviar la cabecera Bearer a
otro destino y convierte cualquier respuesta 3xx en un error operacional.

## Generar o rotar la credencial local

Detén la API y el receiver. Desde la raíz de esta plataforma:

```powershell
Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass
.\scripts\rotate-ingest-token.ps1 `
  -ApiPath "$HOME\Desktop\albion-market-api"
```

El script:

1. mueve el token actual de la API a `ingest-previous.token`, si existe;
2. genera 32 bytes criptográficamente aleatorios;
3. escribe el token nuevo en la API y en la plataforma;
4. actualiza `.env.local` de la API y `.env` de la plataforma para usar archivos;
5. no imprime el secreto.

Reinicia primero la API y después el receiver. Cuando los logs de la API solo
muestren el `auth_key_id` nuevo, retira la credencial anterior:

```powershell
.\scripts\rotate-ingest-token.ps1 `
  -ApiPath "$HOME\Desktop\albion-market-api" `
  -DropPrevious
```

`-DropPrevious` retira únicamente la credencial anterior; no genera un token
nuevo ni modifica la credencial actual de la plataforma.

## Backfill

El backfill usa la misma configuración:

```powershell
.\scripts\backfill-history.ps1
```

También admite `--token-file` al ejecutar el binario directamente. No se
recomienda `--token` porque los argumentos pueden quedar en el historial o ser
visibles en la lista de procesos.

## Exclusiones

`.gitignore` excluye `.env`, `secrets/`, `*.token` y `*.secret`. Los logs muestran
solamente `credential_source=file|environment`, nunca el valor.
