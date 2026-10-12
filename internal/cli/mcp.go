package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/NovaStepStudio/Minecraft-CodeDecompiler/internal/mcp"
)

// cmdMCP gestiona el espejo local de mappings (el "repo como tool").
func cmdMCP(args []string) int {
	if len(args) == 0 {
		printMCPHelp()
		return 0
	}
	sub, subArgs := args[0], args[1:]
	switch sub {
	case "sync":
		return cmdMCPSync(subArgs)
	case "help", "--help", "-h":
		printMCPHelp()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "error: subcomando mcp desconocido %q\n\n", sub)
		printMCPHelp()
		return 1
	}
}

func printMCPHelp() {
	fmt.Fprint(os.Stdout, `USO:
  minecraftdecompiler mcp sync [--jobs 10]

DESCRIPCIÓN:
  Descarga UNA vez todo lo mapeable (último estable + último snapshot por
  versión legacy, srg legacy y tsrgs) al espejo local tools/mcp/.
  Después, remap/map trabajan 100% en local sin tocar la red
  (salvo el refresco de 24h del índice, que usa ETag/304).
  Idempotente: omite lo ya cacheado. Las versiones con mappings
  oficiales (>=1.14.4) se saltan: no necesitan MCP.

EJEMPLOS:
  minecraftdecompiler mcp sync
  minecraftdecompiler mcp sync --jobs 16
`)
}

func cmdMCPSync(args []string) int {
	fs := flag.NewFlagSet("mcp sync", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	jobs := fs.Int("jobs", 10, "descargas paralelas")
	toolsDir := fs.String("tools-dir", "", "directorio de herramientas (defecto: ./tools o $MCDECOMP_TOOLS)")
	showHelp := fs.Bool("help", false, "muestra la ayuda")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *showHelp {
		printMCPHelp()
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()
	tdir := *toolsDir
	if tdir == "" {
		if env := os.Getenv("MCDECOMP_TOOLS"); env != "" {
			tdir = env
		} else {
			tdir = "tools"
		}
	}
	if _, err := mcp.SyncAll(ctx, tdir, *jobs); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	return 0
}
