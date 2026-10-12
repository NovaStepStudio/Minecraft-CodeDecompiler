# Changelog

Todos los cambios notables de este proyecto se documentan aquí.
Formato basado en [Keep a Changelog](https://keepachangelog.com/es-ES/1.0.0/).

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
