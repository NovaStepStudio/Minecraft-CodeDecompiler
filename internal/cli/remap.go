package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/NovaStepStudio/Minecraft-CodeDecompiler/internal/mcp"
	"github.com/NovaStepStudio/Minecraft-CodeDecompiler/internal/remap"
	"github.com/NovaStepStudio/Minecraft-CodeDecompiler/internal/tools"
)

// Nombres de ficheros del pipeline por lado.
func remapPaths(root, side string) (inJar, mappings, outJar string) {
	if side == "server" {
		return filepath.Join(root, "server.jar"),
			filepath.Join(root, "server_mappings.txt"),
			filepath.Join(root, "server-remapped.jar")
	}
	return filepath.Join(root, "client.jar"),
		filepath.Join(root, "client_mappings.txt"),
		filepath.Join(root, "client-remapped.jar")
}

type remapConfig struct {
	mcVersion string
	outBase   string
	side      string
	toolsDir  string
	javaBin   string
	threads   int
	autoTools bool
}

func cmdRemap(args []string) int {
	fs := flag.NewFlagSet("remap", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	mcVersion := fs.String("version", "", "versión descargada con fetch [requerido]")
	outBase := fs.String("out", "versions", "directorio base")
	side := fs.String("side", "client", "client|server|both")
	toolsDir := fs.String("tools-dir", "", "directorio de herramientas (defecto: ./tools o $MCDECOMP_TOOLS)")
	javaBin := fs.String("java", "java", "binario de Java")
	threads := fs.Int("threads", 4, "hilos de tiny-remapper")
	autoTools := fs.Bool("auto-tools", true, "descargar tiny-remapper si falta")
	showHelp := fs.Bool("help", false, "muestra la ayuda")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *mcVersion == "" && fs.NArg() > 0 {
		*mcVersion = fs.Arg(0)
	}
	if *showHelp {
		fmt.Fprint(os.Stdout, `USO:
  minecraft-codedecompiler remap --version <id> [--side client|server|both]

DESCRIPCIÓN:
  Remapea el jar OFUSCADO a nombres legibles y guarda client-remapped.jar.
  Orden: 1) mappings oficiales Mojang (tiny-remapper, 1.14.4-1.21.11),
  2) MCP ModCoderPack (SpecialSource, legacy con canal estable),
  3) si el jar ya viene limpio (26.1+) lo deja listo sin remapear.
  No borra nada: client.jar y mappings se conservan.

FLAGS:
  --version <id>    versión (ej. 1.20.1) [requerido]
  --out <dir>       base (defecto: versions)
  --side <s>        client|server|both (defecto: client)
  --tools-dir <d>   herramientas (defecto: ./tools)
  --java <bin>      java (defecto: java)
  --threads <n>     hilos (defecto: 4)

EJEMPLOS:
  minecraft-codedecompiler remap --version 1.20.1
  minecraft-codedecompiler remap --version 1.20.1 --side both
`)
		return 0
	}
	if *mcVersion == "" {
		fmt.Fprint(os.Stderr, "error: falta --version <id> (ej. --version 1.20.1)\n")
		return 2
	}
	if *side != "client" && *side != "server" && *side != "both" {
		fmt.Fprintf(os.Stderr, "error: --side debe ser client|server|both, got %q\n", *side)
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()
	if runRemap(ctx, remapConfig{
		mcVersion: *mcVersion, outBase: *outBase, side: *side,
		toolsDir: *toolsDir, javaBin: *javaBin,
		threads: *threads, autoTools: *autoTools,
	}) {
		fmt.Fprintf(os.Stdout, "siguiente: decompile --version %s --engine both  (usará el jar remapeado)\n", *mcVersion)
		return 0
	}
	return 1
}

// runRemap remapea los lados pedidos. Devuelve true si todos OK.
func runRemap(ctx context.Context, cfg remapConfig) bool {
	sides := []string{cfg.side}
	if cfg.side == "both" {
		sides = []string{"client", "server"}
	}
	tdir := tools.DefaultDir(cfg.toolsDir)
	if cfg.autoTools {
		if _, err := tools.EnsureTinyRemapper(ctx, tdir); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return false
		}
	}
	root := filepath.Join(cfg.outBase, cfg.mcVersion)
	ok := true
	for _, s := range sides {
		if !runRemapSide(ctx, tdir, root, cfg.mcVersion, s, cfg.javaBin, cfg.threads) {
			ok = false
		}
	}
	return ok
}

// runRemapSide remapea un lado. Casos:
//   - jar ofuscado + mappings oficiales -> tiny-remapper -> outJar (LIMPIO)
//   - jar ofuscado + MCP (legacy con canal estable) -> SpecialSource (LIMPIO)
//   - jar ya limpio (26.1+)    -> copia a outJar, sin remapear
//   - sin nada (< 1.14.4 sin MCP) -> false (seguir con ofuscado)
//
// Nunca borra el jar original ni los mappings.
func runRemapSide(ctx context.Context, toolsDir, root, mcVersion, side, javaBin string, threads int) bool {
	inJar, mappings, outJar := remapPaths(root, side)
	if _, err := os.Stat(inJar); err != nil {
		fmt.Fprintf(os.Stderr, "error: falta %s (ejecuta fetch --version %s primero)\n", inJar, mcVersion)
		return false
	}
	obf, _, err := remap.DetectObfuscation(inJar, side)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: inspeccionando %s: %v\n", inJar, err)
		return false
	}
	if !obf {
		fmt.Fprintf(os.Stderr, "[..] %s ya trae nombres oficiales (>=500 clases net/minecraft): sin remap\n", inJar)
		if _, err := os.Stat(outJar); err != nil {
			if err := copyFileLocal(inJar, outJar); err != nil {
				fmt.Fprintf(os.Stderr, "error: copiando jar limpio: %v\n", err)
				return false
			}
		}
		fmt.Fprintf(os.Stdout, "remap %s: ya limpio -> %s\n", side, outJar)
		return true
	}
	if _, err := os.Stat(mappings); err == nil {
		if err := remap.Remap(ctx, toolsDir, inJar, mappings, outJar, javaBin, threads); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return false
		}
	} else if merr := mcp.RemapMCP(ctx, toolsDir, mcVersion, inJar, outJar, javaBin); merr != nil {
		fmt.Fprintf(os.Stderr, "[..] sin mappings para %s: ni oficiales (%s ausente) ni MCP (%v).\n", mcVersion, mappings, merr)
		fmt.Fprint(os.Stderr, "  Oficiales: 1.14.4/19w36a hasta 1.21.11. MCP: versiones con canal estable.\n")
		fmt.Fprint(os.Stderr, "  Se sigue con el jar ofuscado tal cual (usa --engine cfr).\n")
		return false
	} else {
		fmt.Fprintf(os.Stderr, "[..] remap MCP aplicado (nombres ModCoderPack)\n")
	}
	var mustHave []string
	if side == "server" {
		mustHave = []string{"net/minecraft/server/MinecraftServer.class"}
	} else {
		mustHave = []string{
			"net/minecraft/client/Minecraft.class",
			"net/minecraft/client/main/Main.class",
		}
	}
	found, verr := remap.VerifyOfficial(outJar, mustHave, nil)
	if verr != nil {
		fmt.Fprintf(os.Stderr, "[!!] verificación: %v\n", verr)
		return false
	}
	total, _ := remap.CountOfficial(outJar)
	fmt.Fprintf(os.Stdout, "remap %s: %d clases oficiales verificadas (total net/minecraft: %d) -> %s\n",
		side, found, total, outJar)
	return true
}

func copyFileLocal(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	cerr := out.Close()
	if err != nil {
		return err
	}
	return cerr
}
