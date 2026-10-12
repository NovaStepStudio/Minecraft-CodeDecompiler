# Minecraft-CodeDecompiler

[![CI](https://github.com/NovaStepStudio/Minecraft-CodeDecompiler/actions/workflows/ci.yml/badge.svg)](https://github.com/NovaStepStudio/Minecraft-CodeDecompiler/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/NovaStepStudio/Minecraft-CodeDecompiler)](https://github.com/NovaStepStudio/Minecraft-CodeDecompiler/releases)
[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?logo=go)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Decompilador de **Minecraft Java 1.0 – 1.21.11** (ofuscadas) y **26.1+** (ya sin ofuscar).
CLI solo para terminal, escrito en Go sin dependencias. Sin GUI. Sin librerías del juego.

Entrega **código limpio** (nombres oficiales Mojang o MCP, no `a`/`b`/`c`),
**assets utilizables** (nombres reales, no solo hashes) y un **mapeo en TXT**
para `--javaagent` externos. Un solo comando lo construye todo y no borra nada.

> **Aviso legal:** proyecto comunitario, sin afiliación con Mojang ni Microsoft.
> Los mappings oficiales son © Microsoft y su uso se rige por el
> [EULA de Minecraft](https://account.mojang.com/documents/minecraft_eula).
> Minecraft es marca de Mojang Synergies AB.

## Instalación

Requisitos: **Java 17+** (Temurin u Oracle; CFR y remappers funcionan desde Java 8).

```bash
# Opción A: desde el código
go install github.com/NovaStepStudio/Minecraft-CodeDecompiler@latest

# Opción B: binario de la [página de releases](https://github.com/NovaStepStudio/Minecraft-CodeDecompiler/releases)
#   Windows: minecraftdecompiler-windows-amd64.exe
#   Linux:   minecraftdecompiler-linux-amd64   (chmod +x)
#   macOS:   minecraftdecompiler-darwin-arm64  (chmod +x)

# Opción C: compilar
git clone https://github.com/NovaStepStudio/Minecraft-CodeDecompiler.git
cd Minecraft-CodeDecompiler
go build -o minecraftdecompiler .
```

Los motores Java (CFR, Vineflower, tiny-remapper, SpecialSource) se
autodescargan a `./tools/` (o `$MCDECOMP_TOOLS`) en el primer uso.

## Uso rápido

```bash
# Una versión AL COMPLETO en un paso (1.8.9 tarda ~10 min)
minecraftdecompiler build --version 1.8.9 --engine both --with-objects --materialize

# Moderna con mappings oficiales
minecraftdecompiler build --version 1.20.1 --with-objects --materialize

# Legacy y 26.x
minecraftdecompiler build --version 1.12.2 --engine cfr
minecraftdecompiler build --version 26.3
```

## Comandos

| Comando | Qué hace |
|---|---|
| `build --version <id>` | Pipeline completo: fetch → remap → decompile (+ map). Conserva intermedios |
| `fetch --version <id>` | Descarga jar, mappings e índice (`download`, `download-to-extract`, `setup` son alias) |
| `remap --version <id>` | Jar ofuscado → `client-remapped.jar` limpio (oficial o MCP) |
| `map --version <id>` | `mappings/*.txt` + `AGENT.md` para un `--javaagent` externo |
| `assets download\|build` | Objetos de assets y árbol utilizable con nombres reales |
| `decompile --version <id>` | CFR y/o Vineflower → `src-cfr/` y/o `src-vine/` |
| `tools install\|check` | Instala y verifica los 4 motores + Java |
| `versions` | Lista versiones del manifest de Mojang |

```bash
minecraftdecompiler fetch --version 1.8.9 --side both
minecraftdecompiler remap --version 1.20.1
minecraftdecompiler map --version 1.8.9
minecraftdecompiler assets build --version 1.20.1
minecraftdecompiler decompile --version 1.12.2 --engine cfr --obfuscated
```

## Cómo funciona

```
piston-meta ──► client.jar (+ mappings si hay, + asset_index.json)
                     │
                     ├─► remap ──► client-remapped.jar (LIMPIO)
                     │      ├─ oficial Mojang 1.14.4–1.21.11 (tiny-remapper)
                     │      ├─ MCP legacy con canal estable (SpecialSource)
                     │      └─ 26.1+ ya limpio: se detecta y no se toca
                     │
                     ├─► decompile ──► src-cfr/ + src-vine/ (doble motor)
                     ├─► assets ──► objects/ + virtual/ + client-assets/ + merged-assets/
                     └─► map ──► mappings/*.txt + AGENT.md (para javaagents)
```

### Comportamiento por rango

| Versiones | Mappings | Resultado |
|---|---|---|
| 1.0 – 1.13 (ej. 1.8.9, 1.12.2) | MCP si hay canal estable | Nombres MCP (`getMinecraft`, `theWorld`); si no, ofuscado |
| 1.14.4 / 19w36a – 1.21.11 | Oficiales Mojang | Nombres oficiales (`getInstance`, ...) |
| 26.1+ | No hay ni hacen falta | Detección por contenido → directo a CFR |

La detección es por contenido del jar (500+ clases `net/minecraft/` = limpio),
no por número de versión. Re-ejecutar es seguro: todo se omite si ya existe
y coincide (sha1/tamaño).

## Layout de `versions/<id>/`

| Ruta | Contenido |
|---|---|
| `version.json` | JSON crudo de Mojang (sha1 verificado) |
| `client.jar` | Original ofuscado (se conserva siempre) |
| `client_mappings.txt` | Oficial Mojang, solo 1.14.4–1.21.11 |
| `client-remapped.jar` | Nombres legibles (se conserva siempre) |
| `asset_index.json` | Índice `{ruta: {hash, size}}` |
| `assets/objects/` | Objetos content-addressed (como el launcher) |
| `assets/virtual/<id>/` | Nombres reales (`sounds/`, `lang/`, `icons/`...) |
| `client-assets/` | `assets/` + `data/` extraídos del client.jar |
| `merged-assets/` | Unión virtual + client-assets |
| `mappings/` | `classes/methods/fields.txt` (obf ⇒ legible) + `MANIFEST.txt` + `AGENT.md` |
| `src-cfr/` / `src-vine/` | Fuentes descompiladas por cada motor |

Tamaños orientativos: 1.8.9 ~110 MB (734 objetos), 1.20.1 ~621 MB (3598 objetos).

## Para autores de javaagents

`mappings/` está pensado para consumirse desde otra herramienta:

- `classes.txt`: `<obf> <legible>` por línea.
- `methods.txt`: `<ownerObf>.<nombreObf><descObf> => <legible...>` (descriptores JVM).
- `fields.txt`: igual para fields (fuente MCP: tipo `?`, matchear por owner+nombre).
- `AGENT.md`: objetivos curados ya resueltos (menú principal, botones, jugador,
  mundo, ajustes...) con el triple runtime exacto para ASM + receta mínima.

Regla de oro: en runtime el juego va ofuscado (lado izquierdo); en el código
descompilado van los legibles (lado derecho).

## Estructura del repo

```text
main.go               entrada -> internal/cli
internal/cli/         build, fetch, remap, map, assets, decompile, tools, versions
internal/mojang/      manifest, detalle de versión, descargas con sha1
internal/assets/      objetos en paralelo, virtual, extract, merge
internal/remap/       tiny-remapper + detección ofuscado/limpio + verificación
internal/mcp/         MCP legacy: descarga, fusión notch->mcp, SpecialSource
internal/mapping/     parsers ProGuard/SRG, emisores TXT y guía del agente
internal/tools/       CFR, Vineflower, tiny-remapper, SpecialSource, Java
internal/decompile/   orquesta `java -jar` por motor
internal/store/       layout versions/<id>/
```

## Preguntas frecuentes

- **¿Por qué no descarga las librerías?** Decisión de diseño: solo se necesita
  el juego descompilado + assets. Compilar el árbol completo exigiría el
  classpath entero (fuera de alcance).
- **¿Funciona sin red?** No en la primera ejecución (descarga Mojang + motores).
  Después, casi todo se reutiliza desde disco.
- **`src-vine` vacío / carpetas `2`, `4`...** Era un bug de invocación
  (`--thread-count` con espacio) corregido en 1.0.0: borra esas carpetas y
  repite el `decompile`.
- **¿1.0 de verdad?** Sí: fetch + CFR verificados en 1.0 (999 clases → 999 .java).

## Contribuir

Lee [CONTRIBUTING.md](CONTRIBUTING.md). Sin dependencias Go nuevas salvo
justificación, `gofmt` + `go vet` limpios, y pruebas con 1.8.9 si tocas el
pipeline. Reporta vulnerabilidades según [SECURITY.md](SECURITY.md).

## Agradecimientos

- Mojang/Microsoft por los mappings oficiales y el EULA que permite este uso.
- ModCoderPack (MCP/MCPMappingsArchive), FabricMC (tiny-remapper, mapping-io),
  Vineflower, Lee Benfield (CFR) y MD_5 (SpecialSource).
- La comunidad de modding (Forge, Fabric, Quilt) por documentar el pipeline.

## Licencia

[MIT](LICENSE) © NovaStepStudio.
