# Política de seguridad

## Versiones soportadas

Solo la última release (`main`). Si usas un binario viejo, actualiza y
reproduce el problema antes de reportar.

## Reportar una vulnerabilidad

No abras un issue público. Escribe al mantenedor por el canal privado que
indique su perfil de GitHub con:

- descripción y alcance del problema,
- pasos para reproducirlo,
- versión del CLI y plataforma.

Se acusará recibo en un máximo de 7 días. No hay programa de recompensas.

## Notas

- Este CLI descarga y ejecuta jars de terceros (Mojang, Fabric, MCP, CFR...).
  Las URLs están pineadas en el código (`internal/tools`, `internal/mcp`,
  `internal/mojang`); cualquier cambio de URL debe ir con justificación.
- Nunca ejecutes binarios `minecraftdecompiler*` de fuentes no oficiales.
- Los hashes sha1 de Mojang se verifican siempre tras descargar.
