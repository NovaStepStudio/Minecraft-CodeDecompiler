package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/NovaStepStudio/Minecraft-CodeDecompiler/internal/decompile"
	"github.com/NovaStepStudio/Minecraft-CodeDecompiler/internal/tools"
)

func cmdDecompile(args []string) int {
	fs := flag.NewFlagSet("decompile", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	mcVersion := fs.String("version", "", "versión descargada con fetch (usa versions/<id>/client.jar)")
	jar := fs.String("jar", "", "jar de entrada explícito (alternativa a --version)")
	out := fs.String("out", "", "directorio base de salida (defecto: versions/<id>)")
	engine := fs.String("engine", "both", "motor: cfr|vine|both")
	toolsDir := fs.String("tools-dir", "", "directorio de motores (defecto: ./tools o $MCDECOMP_TOOLS)")
	javaBin := fs.String("java", "java", "binario de Java")
	threads := fs.Int("threads", runtime.NumCPU(), "hilos para Vineflower")
	autoTools := fs.Bool("auto-tools", true, "descargar motores faltantes automáticamente")
	obfuscated := fs.Bool("obfuscated", false, "forzar jar ofuscado aunque exista el remapeado")
	showHelp := fs.Bool("help", false, "muestra la ayuda")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	// Compat: `decompile <archivo>` antiguo.
	if *jar == "" && *mcVersion == "" && fs.NArg() > 0 {
		*jar = fs.Arg(0)
	}
	if *showHelp || (*jar == "" && *mcVersion == "") {
		fmt.Fprint(os.Stdout, `USO:
  minecraft-codedecompiler decompile --version <id> [--engine cfr|vine|both]
  minecraft-codedecompiler decompile --jar <input.jar> --out <dir> [--engine both]

FLAGS:
  --version <id>    usa versions/<id>/client.jar (descargado con fetch)
  --jar <path>      jar explícito (ignora --version)
  --out <dir>       salida (defecto: versions/<id> -> src-cfr/, src-vine/)
  --obfuscated      forzar client.jar ofuscado aunque exista client-remapped.jar
                    (por defecto se usa el remapeado = código LIMPIO)
  --engine <e>      cfr|vine|both (defecto: both)
                    cfr  = CFR 0.152, mejor con ofuscado (1.6-1.13)
                    vine = Vineflower 1.12.0, estándar moderno (1.14-1.21.x)
  --tools-dir <d>   motores (defecto: ./tools)
  --java <bin>      binario java (defecto: java, se necesita 17+ para vine)
  --threads <n>     hilos Vineflower (defecto: NumCPU)
  --auto-tools      descargar motores si faltan (defecto: true)

EJEMPLOS:
  minecraft-codedecompiler decompile --version 1.20.1 --engine both
  minecraft-codedecompiler decompile --version 1.8.9 --engine cfr
  minecraft-codedecompiler decompile --jar ./versions/1.20.1/client.jar --out ./out --engine vine
`)
		if *jar == "" && *mcVersion == "" {
			return 2
		}
		return 0
	}

	inputJar := *jar
	outBase := *out
	if inputJar == "" {
		remapped := filepath.Join("versions", *mcVersion, "client-remapped.jar")
		plain := filepath.Join("versions", *mcVersion, "client.jar")
		if !*obfuscated {
			if _, err := os.Stat(remapped); err == nil {
				inputJar = remapped
				fmt.Fprintf(os.Stderr, "[..] usando jar remapeado (nombres oficiales): %s\n", inputJar)
			} else {
				inputJar = plain
				fmt.Fprintf(os.Stderr, "[..] sin remap: usando jar ofuscado %s (ejecuta remap --version %s para código limpio)\n", inputJar, *mcVersion)
			}
		} else {
			inputJar = plain
		}
		if outBase == "" {
			outBase = filepath.Join("versions", *mcVersion)
		}
	} else {
		if outBase == "" {
			outBase = "out"
		}
	}
	eng := tools.NormalizeEngine(*engine)
	if *engine != "" {
		switch strings.ToLower(*engine) {
		case "cfr", "vine", "vineflower", "both", "all":
		default:
			fmt.Fprintf(os.Stderr, "error: --engine debe ser cfr|vine|both, got %q\n", *engine)
			return 2
		}
	}
	tdir := tools.DefaultDir(*toolsDir)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()

	if *autoTools {
		if err := tools.Ensure(ctx, tdir, eng); err != nil {
			fmt.Fprintf(os.Stderr, "error: motores: %v\n", err)
			return 1
		}
	}
	// Chequeo de Java (avisa, no bloquea salvo error grave).
	if major, err := tools.CheckJava(*javaBin, eng, 1, 20); err != nil {
		fmt.Fprintf(os.Stderr, "[!!] java: %v (detectado %d)\n", err, major)
		if eng != "cfr" {
			return 1
		}
		fmt.Fprint(os.Stderr, "[..] continuando solo porque el motor es cfr (funciona con Java 8+)\n")
	} else {
		fmt.Fprintf(os.Stderr, "[ok] java %d (%s)\n", major, *javaBin)
	}

	outs, err := decompile.Run(ctx, tdir, inputJar, outBase, decompile.Options{
		JavaBin: *javaBin,
		Engine:  eng,
		Threads: *threads,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stdout, "descompilado (%s) ->\n", eng)
	for _, o := range outs {
		fmt.Fprintf(os.Stdout, "  %s\n", o)
	}
	return 0
}
