package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/NovaStepStudio/Minecraft-CodeDecompiler/internal/tools"
)

func cmdTools(args []string) int {
	if len(args) == 0 {
		printToolsHelp()
		return 0
	}
	sub, subArgs := args[0], args[1:]
	switch sub {
	case "install":
		return cmdToolsInstall(subArgs)
	case "check":
		return cmdToolsCheck(subArgs)
	case "help", "--help", "-h":
		printToolsHelp()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "error: subcomando tools desconocido %q\n\n", sub)
		printToolsHelp()
		return 1
	}
}

func printToolsHelp() {
	fmt.Fprint(os.Stdout, `USO:
  minecraft-codedecompiler tools install [--tools-dir <d>] [--engine all]
  minecraft-codedecompiler tools check [--tools-dir <d>]

HERRAMIENTAS (se autodescargan a ./tools o $MCDECOMP_TOOLS):
  CFR 0.152                      descompilador, mejor con ofuscado legacy
  Vineflower 1.12.0              descompilador moderno (exige Java 17+)
  tiny-remapper 0.15.0           remap a nombres oficiales Mojang
  SpecialSource 1.11.0           remap a nombres MCP (legacy)
  (QuiltFlower está archivado -> se usa Vineflower, su continuación)

JAVA:
  CFR, tiny-remapper y SpecialSource corren en Java 8+.
  Vineflower 1.12 exige Java 17+ (recomendado 21 para MC 1.20.5+/1.21.x).

EJEMPLOS:
  minecraft-codedecompiler tools install
  minecraft-codedecompiler tools install --engine cfr
  minecraft-codedecompiler tools check
`)
}

func cmdToolsInstall(args []string) int {
	fs := flag.NewFlagSet("tools install", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	toolsDir := fs.String("tools-dir", "", "directorio (defecto: ./tools o $MCDECOMP_TOOLS)")
	engine := fs.String("engine", "all", "cfr|vine|tiny|special|both|all")
	showHelp := fs.Bool("help", false, "muestra la ayuda")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *showHelp {
		printToolsHelp()
		return 0
	}
	tdir := tools.DefaultDir(*toolsDir)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	var err error
	switch *engine {
	case "all":
		err = tools.EnsureAll(ctx, tdir)
	case "both":
		err = tools.Ensure(ctx, tdir, "both")
	case "cfr", "vine", "tiny", "special":
		err = tools.EnsureOne(ctx, tdir, *engine)
	default:
		fmt.Fprintf(os.Stderr, "error: --engine debe ser cfr|vine|tiny|special|both|all, got %q\n", *engine)
		return 2
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	return cmdToolsCheck([]string{"--tools-dir", tdir})
}

func cmdToolsCheck(args []string) int {
	fs := flag.NewFlagSet("tools check", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	toolsDir := fs.String("tools-dir", "", "directorio (defecto: ./tools o $MCDECOMP_TOOLS)")
	javaBin := fs.String("java", "java", "binario de Java")
	showHelp := fs.Bool("help", false, "muestra la ayuda")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *showHelp {
		printToolsHelp()
		return 0
	}
	tdir := tools.DefaultDir(*toolsDir)
	st := tools.FullStatus(tdir)
	fmt.Fprintf(os.Stdout, "tools dir: %s\n", tdir)
	for _, t := range st {
		mark := "x"
		if t.OK {
			mark = "ok"
		}
		fmt.Fprintf(os.Stdout, "  [%s] %s\n", mark, t.Name)
	}
	if _, err := os.Stat(filepath.Join(tdir, "mcp")); err == nil {
		fmt.Fprint(os.Stdout, "  [ok] mcp/ (caché de mapeos legacy)\n")
	} else {
		fmt.Fprint(os.Stdout, "  [x] mcp/ (se descarga solo con remap/map legacy)\n")
	}
	major, raw, err := tools.JavaVersion(context.Background(), *javaBin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "java: %v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stdout, "java: major=%d (%s)\n", major, raw)
	if major < 17 {
		fmt.Fprint(os.Stderr, "[!!] Vineflower necesita Java 17+; instala Temurin 17/21\n")
	}
	return 0
}
