# Contribuir

¡Gracias por querer mejorar Minecraft-CodeDecompiler! Guía rápida:

## Requisitos

- Go 1.27+ y Java 17+ (Temurin u Oracle).
- Sin dependencias externas de Go: solo stdlib.

## Flujo

1. Haz fork y crea una rama (`git checkout -b feat/mi-cambio`).
2. Haz tu cambio con mensajes claros (español o inglés).
3. Verifica antes del PR:
   ```bash
   gofmt -l .        # debe estar vacío
   go vet ./...
   go build -o minecraftdecompiler .
   ```
4. Si tocas descargas/remap/decompile, prueba con una versión pequeña
   (1.8.9 es la referencia: `build` completo tarda ~10 min).
5. Abre el PR contra `main` describiendo qué cambia y cómo lo probaste.

## Convenciones

- CLI solo-terminal: nada de GUI; todo por stdout/stderr y exit codes.
- Sin dependencias Go nuevas salvo justificación fuerte.
- Los motores externos (CFR, Vineflower, tiny-remapper, SpecialSource) se
  orquestan vía `java`, nunca se reimplementan.
- No commitear artefactos: `versions/`, `tools/`, `*.exe` están ignorados.
- Ayudas del CLI en español, concisas; README al día si cambia UX.

## Reportar bugs

Usa la plantilla de issue e incluye: versión del CLI (`version`),
versión de Minecraft, comando exacto, salida relevante y `java -version`.
