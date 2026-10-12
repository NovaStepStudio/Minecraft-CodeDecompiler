# Changelog

Todos los cambios notables de este proyecto se documentan aquí.
Formato basado en [Keep a Changelog](https://keepachangelog.com/es-ES/1.0.0/).

## [1.1.0] - 2026-10-12

### Añadido

- Comando `mcp sync`: descarga UNA vez todo lo mapeable (último estable +
  último snapshot por versión legacy, srg legacy y tsrgs) al espejo local.
  Después `remap`/`map` trabajan 100% en local sin tocar la red.
  Las versiones con mappings oficiales se saltan (no necesitan MCP).

## [1.0.2] - 2026-10-12

### Añadido

- Anti rate-limit GitHub: nada se re-descarga si ya está en caché.
  El `joined.tsrg` se cachea (antes bajaba en cada run) y el índice MCP usa
  ETag condicional (304 sin gastar cuota si no cambió aguas arriba).

## [1.0.1] - 2026-10-12

### Añadido

- Canales MCP snapshot además de estables: `--mcp-channel stable|snapshot`
  o canal exacto (`22`, `stable_22`, `20160301`, `snapshot_20160301`).
  Caché separada por canal en `tools/mcp/<mc>/<kind>-<num>/`.
- Índice MCP siempre actualizado: se re-descarga si tiene +24h o con
  `--refresh` (con tolerancia a fallos: sigue con la caché si no hay red).
- Probe de aplicabilidad: antes de remapear se verifica que el srg
  corresponde al jar (muestreo de miembros); si deriva, aborta sin tocar nada.

### Corregido

- 1.12.2 daba salida corrupta en silencio: el srg donante 1.12 tiene los
  notch globales derivados. Ahora usa el `joined.tsrg` exacto de MCPConfig
  (+ CSVs `stable_39` de 1.12, como hacía Forge) y el probe lo valida.
- Verificado: 1.8.9 snapshot (83% nombres, probe 98%), 1.12.2 (probe 100%,
  `getMinecraft`/`displayGuiScreen` reales), 1.13.2 (probe 100%).

## [1.0.0] - 2026-10-12

Primera versión pública.

### Añadido

- Comando `build`: pipeline completo en un paso (fetch → remap → decompile
  + map). No borra intermedios; re-ejecutar es seguro (skip por sha1/tamaño).
- Comando `fetch` (alias `download`, `download-to-extract`, `setup`):
  `version.json`, `client.jar`/`server.jar`, mappings oficiales y
  `asset_index.json` con verificación sha1.
- Comando `remap`: jar ofuscado → `client-remapped.jar` limpio.
  Orden: mappings oficiales Mojang (tiny-remapper, 1.14.4–1.21.11),
  MCP ModCoderPack (SpecialSource, legacy con canal estable),
  jar ya limpio (26.1+) detectado por contenido y copiado sin tocar.
- Comando `map`: `mappings/` en TXT por chunks (`classes/methods/fields.txt`,
  obf ⇒ legible) + `MANIFEST.txt` + `AGENT.md` con objetivos curados
  resueltos para `--javaagent` externos.
- Comando `assets` (`download`/`build`): objetos en paralelo con reintentos,
  árbol `virtual/` con nombres reales, extracción de `client.jar` y unión
  `merged-assets/`. Flags `--with-objects` / `--materialize` en `fetch`.
- Comando `decompile`: doble motor CFR 0.152 + Vineflower 1.12.0
  (usa el jar remappeado si existe; `--obfuscated` fuerza el original).
- Comandos `tools` (las 4 herramientas + Java) y `versions` (manifest Mojang).
- Rango 1.0 – 1.21.11 (ofuscadas) y 26.1+ (sin ofuscar, directo a CFR).

### Verificado

- 1.8.9 al completo: MCP `stable_22` (`getMinecraft`, `theWorld`...),
  1612 fuentes por motor, assets 734/734 objetos y `Vec3.java` recompilado
  con `javac` (exit 0).
- 1.20.1: remap oficial (7103 clases) y assets completos.
- 1.12.2, 1.0 y detección 26.x sin ofuscar.
