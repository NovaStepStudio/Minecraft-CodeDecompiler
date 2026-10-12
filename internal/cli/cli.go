package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
)

const (
	Name    = "minecraft-codedecompiler"
	Version = "1.0.0"
)

// Run ejecuta el CLI. Solo terminal (stdin/stdout/stderr).
// Devuelve el exit code.
func Run(args []string) int {
	fs := flag.NewFlagSet(Name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	showHelp := fs.Bool("help", false, "muestra la ayuda")
	showH := fs.Bool("h", false, "muestra la ayuda")
	showVersion := fs.Bool("version", false, "muestra la versión")
	showV := fs.Bool("v", false, "muestra la versión")

	if err := fs.Parse(args); err != nil {
		printHelp(os.Stdout)
		return 2
	}

	if *showHelp || *showH {
		printHelp(os.Stdout)
		return 0
	}
	if *showVersion || *showV {
		fmt.Printf("%s %s\n", Name, Version)
		return 0
	}

	rest := fs.Args()
	if len(rest) == 0 {
		printHelp(os.Stdout)
		return 0
	}

	cmd, cmdArgs := rest[0], rest[1:]
	switch cmd {
	case "help", "--help", "-h":
		printHelp(os.Stdout)
		return 0
	case "version", "--version", "-v":
		fmt.Printf("%s %s\n", Name, Version)
		return 0
	case "fetch", "download", "download-to-extract", "setup":
		return cmdFetch(cmdArgs)
	case "build":
		return cmdBuild(cmdArgs)
	case "remap":
		return cmdRemap(cmdArgs)
	case "map":
		return cmdMap(cmdArgs)
	case "assets":
		return cmdAssets(cmdArgs)
	case "decompile":
		return cmdDecompile(cmdArgs)
	case "tools":
		return cmdTools(cmdArgs)
	case "versions", "list", "list-versions":
		return cmdVersions(cmdArgs)
	default:
		fmt.Fprintf(os.Stderr, "error: comando desconocido %q\n\n", cmd)
		printHelp(os.Stderr)
		return 1
	}
}

func printHelp(w io.Writer) {
	fmt.Fprintf(w, `%s %s - decompilador de Minecraft (solo terminal)

USO:
  %s [flags] <comando> [args]

FLAGS:
  -h, --help       muestra esta ayuda
  -v, --version    muestra la versión

COMANDOS:
  build              construye una versión AL COMPLETO en un paso:
                     fetch -> remap -> decompile (conserva intermedios)
  fetch              descarga todo lo necesario de una versión:
                     jar/cliente, mappings e índice de assets.
                     Alias: download, download-to-extract, setup
  remap              remapea el jar ofuscado a nombres oficiales Mojang
                     (tiny-remapper + client_mappings.txt) -> jar LIMPIO
  map                genera mappings/*.txt + AGENT.md para un --javaagent
                     externo (obf => legible, objetivos curados resueltos)
  assets             descarga objetos y construye assets UTILIZABLES
                     (nombres reales, no solo hashes)
  decompile          descompila un jar con CFR y/o Vineflower
                     (usa el jar remapeado si existe)
  tools              instala/verifica motores (CFR + Vineflower) y Java
  versions           lista versiones disponibles (manifest de Mojang)
  help               muestra esta ayuda
  version            muestra la versión

FLUJO TÍPICO (sin librerías del juego):
  %s build --version 1.20.1 --with-objects --materialize   (todo en un paso)
  %s fetch --version 1.12.2                               (legacy, sin mappings)
  %s remap --version 1.20.1                               (jar LIMPIO)
  %s decompile --version 1.20.1 --engine both             (código LIMPIO + assets reales)

  %s build --help
  %s map --help
  %s fetch --help
  %s remap --help
  %s assets --help
  %s decompile --help

RANGO: 1.0 hasta 1.21.11 (ofuscadas, con mappings desde 1.14.4).
  26.1+ ya viene SIN ofuscar: se detecta solo y va directo a CFR.

NOTAS:
  - Mappings oficiales: 1.14.4/19w36a hasta 1.21.11. En legacy se intenta
    MCP ModCoderPack; sin MCP disponible se descompila ofuscado.
  - Sin librerías del juego. Objetos de assets: solo con
    --with-objects o assets download (1.20.1 ~621MB).
  - Pipeline: fetch -> remap (tiny-remapper) -> decompile.
    decompile usa client-remapped.jar si existe (código LIMPIO).
`, Name, Version, Name, Name, Name, Name, Name, Name, Name, Name, Name, Name, Name)
}
