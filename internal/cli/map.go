package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/NovaStepStudio/Minecraft-CodeDecompiler/internal/mapping"
	"github.com/NovaStepStudio/Minecraft-CodeDecompiler/internal/mcp"
)

// cmdMap genera mappings/*.txt + MANIFEST.txt + AGENT.md para un javaagent
// externo (p.ej. NovaCore-Agent). Misma prioridad que remap: oficiales,
// si no MCP legacy. No toca los jars.
func cmdMap(args []string) int {
	fs := flag.NewFlagSet("map", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	mcVersion := fs.String("version", "", "versión con remap disponible (ej. 1.8.9) [requerido]")
	outBase := fs.String("out", "versions", "directorio base")
	side := fs.String("side", "client", "client|server|both")
	showHelp := fs.Bool("help", false, "muestra la ayuda")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *mcVersion == "" && fs.NArg() > 0 {
		*mcVersion = fs.Arg(0)
	}
	if *showHelp {
		fmt.Fprint(os.Stdout, `USO:
  minecraft-codedecompiler map --version <id> [--side client|server|both]

DESCRIPCIÓN:
  Genera en versions/<id>/mappings/ el mapeo en TXT por chunks para un
  --javaagent externo: classes.txt, methods.txt, fields.txt (obf => legible),
  MANIFEST.txt (formato y cobertura) y AGENT.md (objetivos curados resueltos:
  menus, botones, jugador... listos para intervenir/inyectar).
  Fuente: mappings oficiales si existen, si no MCP legacy (descarga mcp.srg).

EJEMPLOS:
  minecraft-codedecompiler map --version 1.8.9
  minecraft-codedecompiler map --version 1.20.1
`)
		return 0
	}
	if *mcVersion == "" {
		fmt.Fprint(os.Stderr, "error: falta --version <id>\n")
		return 2
	}
	if *side != "client" && *side != "server" && *side != "both" {
		fmt.Fprintf(os.Stderr, "error: --side debe ser client|server|both, got %q\n", *side)
		return 2
	}
	ctx := context.Background()
	sides := []string{*side}
	if *side == "both" {
		sides = []string{"client", "server"}
	}
	ok := true
	for _, s := range sides {
		if !runMap(ctx, *outBase, *mcVersion, s) {
			ok = false
		}
	}
	if !ok {
		return 1
	}
	return 0
}

// runMap genera el mapeo de un lado en <root>/mappings[-server]/. Devuelve false
// si no hay fuente de mapeo (ni oficiales ni MCP).
func runMap(ctx context.Context, outBase, mcVersion, side string) bool {
	root := filepath.Join(outBase, mcVersion)
	var mapDir, jarName, mapFile string
	if side == "server" {
		mapDir = filepath.Join(root, "mappings-server")
		jarName = "server-remapped.jar"
		mapFile = filepath.Join(root, "server_mappings.txt")
	} else {
		mapDir = filepath.Join(root, "mappings")
		jarName = "client-remapped.jar"
		mapFile = filepath.Join(root, "client_mappings.txt")
	}
	var m *mapping.Mapping
	var source string
	if _, err := os.Stat(mapFile); err == nil {
		mm, perr := mapping.ParseProGuard(mapFile)
		if perr != nil {
			fmt.Fprintf(os.Stderr, "error: parseando %s: %v\n", mapFile, perr)
			return false
		}
		m, source = mm, "oficial Mojang (ProGuard)"
	} else {
		srg, serr := mcp.Ensure(ctx, toolsDirForMap(), mcVersion)
		if serr != nil {
			fmt.Fprintf(os.Stderr, "[..] map: sin fuente para %s (ni oficiales ni MCP: %v)\n", mcVersion, serr)
			return false
		}
		mm, perr := mapping.ParseSRG(srg)
		if perr != nil {
			fmt.Fprintf(os.Stderr, "error: parseando %s: %v\n", srg, perr)
			return false
		}
		m, source = mm, "MCP ModCoderPack (SRG)"
	}
	header := fmt.Sprintf("mapeo %s %s (%s)", mcVersion, side, source)
	if err := m.WriteTXT(mapDir, header); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return false
	}
	meta := mapping.GuideMeta{MCVersion: mcVersion, Side: side, Source: source, Jar: jarName}
	if err := m.WriteManifest(mapDir, meta); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return false
	}
	if err := m.WriteAgentGuide(mapDir, meta); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return false
	}
	fmt.Fprintf(os.Stdout, "map %s: %d clases, %d metodos, %d fields (%s) -> %s\n",
		side, len(m.Classes), len(m.Methods), len(m.Fields), source, mapDir)
	return true
}

// toolsDirForMap resuelve el directorio de herramientas igual que tools.DefaultDir.
func toolsDirForMap() string {
	if env := os.Getenv("MCDECOMP_TOOLS"); env != "" {
		return env
	}
	return "tools"
}
