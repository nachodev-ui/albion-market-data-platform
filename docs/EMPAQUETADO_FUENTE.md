# Empaquetado seguro del código fuente

No comprimas el directorio de trabajo completo. Aunque Git ignore archivos
locales, el directorio puede contener `.git`, tokens, logs, ejecutables y
capturas de mercado.

Genera el paquete únicamente desde archivos rastreados:

```powershell
.\scripts\export-source.ps1
```

El resultado predeterminado es:

```text
artifacts/albion-market-data-platform-source.zip
```

El exportador:

- obtiene la lista mediante `git ls-files`;
- rechaza rutas de secretos, runtime, datos y binarios;
- busca marcadores conocidos de claves y tokens en archivos rastreados;
- crea el ZIP con rutas ordenadas y la fecha del commit;
- vuelve a abrir el ZIP y compara sus entradas con la lista de Git;
- imprime el SHA-256 resultante.

La misma comprobación se ejecuta desde `scripts/check.ps1` y GitHub Actions.
Los ZIP generados quedan bajo `artifacts/`, que está ignorado por Git.
