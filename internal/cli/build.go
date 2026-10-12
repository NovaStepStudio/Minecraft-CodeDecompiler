package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/NovaStepStudio/Minecraft-CodeDecompiler/internal/remap"
)

// cmdBuild construye una versión AL COMPLETO en un solo paso:
// fetch -> remap (si aplica) -> decompile. No borra ningún intermedio:
// client.jar, mappings, client-remapped.jar y fuentes conviven.
func cmdBuild(args []string) int {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	mcVersion := fs.String("version", "", "versión de Minecraft (ej. 1.12.2, 1.20.1) [requerido]")
	outBase := fs.String("out", "versions", "directorio base de salida")
	side := fs.String("side", "client", "client|server|both")
	engine := fs.String("engine", "auto", "cfr|vine|both|auto (auto: cfr si ya viene limpio 26.1+, both en resto)")
	withObjects := fs.Bool("with-objects", false, "descargar TODOS los objetos de assets (pesado)")
	materialize := fs.Bool("materialize", false, "assets UTILIZABLES con nombres reales")
	jobs := fs.Int("jobs", 12, "descargas paralelas de objetos")
	threads := fs.Int("threads", 4, "hilos de tiny-remapper/Vineflower")
	mcpChannel := fs.String("mcp-channel", "stable", "canal MCP legacy: stable|snapshot|exacto")
	refresh := fs.Bool("refresh", false, "forzar actualización del índice MCP")
	toolsDir := fs.String("tools-dir", "", "directorio de herramientas (defecto: ./tools o $MCDECOMP_TOOLS)")
	javaBin := fs.String("java", "java", "binario de Java")
	showHelp := fs.Bool("help", false, "muestra la ayuda")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *mcVersion == "" && fs.NArg() > 0 {
		*mcVersion = fs.Arg(0)
	}
	if *showHelp {
		fmt.Fprint(os.Stdout, `USO:
  minecraft-codedecompiler build --version <id> [flags]

DESCRIPCIÓN:
  Construye una versión AL COMPLETO en un solo paso:
    1. fetch: version.json, client.jar, mappings (si hay), asset_index.json
    2. remap: client.jar ofuscado -> client-remapped.jar LIMPIO (tiny-remapper)
       - Sin mappings (< 1.14.4): se sigue con el jar ofuscado (aviso).
       - Jar ya limpio (26.1+, sin ofuscar): se deja listo sin remapear.
    3. decompile: CFR y/o Vineflower -> src-cfr/ y/o src-vine/
  No borra NADA: todos los intermedios se conservan en versions/<id>/.
  Rango soportado: 1.0 hasta 1.21.11 (ofuscadas) y 26.1+ (ya limpias).

FLAGS:
  --version <id>    versión [requerido]
  --out <dir>       base (defecto: versions)
  --side <s>        client|server|both (defecto: client)
  --engine <e>      cfr|vine|both|auto (defecto: auto)
  --with-objects    descargar objetos de assets (pesado)
  --materialize     assets utilizables con nombres reales
  --jobs <n>        descargas paralelas (defecto: 12)
  --threads <n>     hilos de tiny-remapper (defecto: 4; decompile usa NumCPU)
  --mcp-channel <c> stable|snapshot|exacto (defecto: stable)
  --refresh         forzar actualización del índice MCP
  --tools-dir <d>   herramientas (defecto: ./tools)
  --java <bin>      java (defecto: java)

EJEMPLOS:
  minecraft-codedecompiler build --version 1.20.1 --with-objects --materialize
  minecraft-codedecompiler build --version 1.12.2 --engine cfr
  minecraft-codedecompiler build --version 26.3
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
	eng := *engine
	if eng != "auto" && eng != "cfr" && eng != "vine" && eng != "both" {
		fmt.Fprintf(os.Stderr, "error: --engine debe ser cfr|vine|both|auto, got %q\n", eng)
		return 2
	}

	t0 := time.Now()
	fmt.Fprintf(os.Stderr, "=== build %s (nada se borrará) ===\n", *mcVersion)

	// Paso 1: fetch.
	fetchArgs := []string{"--version", *mcVersion, "--out", *outBase, "--side", *side}
	if *withObjects {
		fetchArgs = append(fetchArgs, "--with-objects")
	}
	if *materialize {
		fetchArgs = append(fetchArgs, "--materialize")
	}
	fetchArgs = append(fetchArgs, "--jobs", fmt.Sprint(*jobs))
	fmt.Fprintln(os.Stderr, "--- [1/3] fetch ---")
	if code := cmdFetch(fetchArgs); code != 0 {
		fmt.Fprintln(os.Stderr, "build: falló fetch")
		return code
	}

	// Paso 2: remap (tolerante: sin mappings se sigue con ofuscado).
	fmt.Fprintln(os.Stderr, "--- [2/3] remap ---")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()
	sides := []string{*side}
	if *side == "both" {
		sides = []string{"client", "server"}
	}
	root := filepath.Join(*outBase, *mcVersion)
	if !runRemap(ctx, remapConfig{
		mcVersion: *mcVersion, outBase: *outBase, side: *side,
		toolsDir: *toolsDir, javaBin: *javaBin,
		threads: *threads, autoTools: true,
		mcpChannel: *mcpChannel, refresh: *refresh,
	}) {
		fmt.Fprintln(os.Stderr, "[..] remap parcial: se descompilará el jar ofuscado tal cual")
	}

	// Paso 3: decompile.
	if eng == "auto" {
		eng = "both" // remapeado o legacy ofuscado: ambos motores
		for _, s := range sides {
			inJar, _, _ := remapPaths(root, s)
			if obf, _, derr := remap.DetectObfuscation(inJar, s); derr == nil && !obf {
				eng = "cfr" // jar que ya venía limpio (26.1+): solo CFR
				fmt.Fprintln(os.Stderr, "[..] motor auto: jar sin ofuscar -> CFR")
				break
			}
		}
	}
	// Paso 2b: mapeo TXT para javaagent externo (best-effort, no bloquea).
	fmt.Fprintln(os.Stderr, "--- [map] mappings/*.txt + AGENT.md ---")
	for _, s := range sides {
		if !runMap(ctx, *outBase, *mcVersion, s, *mcpChannel, *refresh) {
			fmt.Fprintln(os.Stderr, "[..] map parcial: sin fuente de mapeo para ese lado")
		}
	}
	fmt.Fprintln(os.Stderr, "--- [3/3] decompile ("+eng+") ---")
	decArgs := []string{"--version", *mcVersion, "--out", root, "--engine", eng,
		"--tools-dir", *toolsDir, "--java", *javaBin}
	if code := cmdDecompile(decArgs); code != 0 {
		fmt.Fprintln(os.Stderr, "build: falló decompile")
		return code
	}

	fmt.Fprintf(os.Stderr, "=== build %s OK en %s; intermedios conservados en %s ===\n",
		*mcVersion, time.Since(t0).Round(time.Second), root)
	printBuildSummary(root)
	return 0
}

func printBuildSummary(root string) {
	ls := func(p, label string) {
		fi, err := os.Stat(p)
		if err != nil {
			return
		}
		if fi.IsDir() {
			n := countFiles(p)
			fmt.Fprintf(os.Stdout, "  %s/ (%d ficheros)\n", label, n)
			return
		}
		fmt.Fprintf(os.Stdout, "  %s (%d bytes)\n", label, fi.Size())
	}
	fmt.Fprintf(os.Stdout, "contenido de %s:\n", root)
	for _, f := range []string{"version.json", "client.jar", "client_mappings.txt",
		"server.jar", "server_mappings.txt", "client-remapped.jar", "server-remapped.jar",
		"asset_index.json"} {
		ls(filepath.Join(root, f), f)
	}
	for _, d := range []string{"assets", "client-assets", "merged-assets", "mappings", "mappings-server", "src-cfr", "src-vine"} {
		ls(filepath.Join(root, d), d)
	}
}

func countFiles(dir string) int64 {
	var n int64
	_ = filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			n++
		}
		return nil
	})
	return n
}
