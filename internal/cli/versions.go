package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/NovaStepStudio/Minecraft-CodeDecompiler/internal/mojang"
)

func cmdVersions(args []string) int {
	fs := flag.NewFlagSet("versions", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	typ := fs.String("type", "release", "filtro: release|snapshot|old_beta|old_alpha|all")
	limit := fs.Int("limit", 20, "máximo a mostrar (0 = todas)")
	search := fs.String("search", "", "filtrar por prefijo/substring (ej. 1.20)")
	showHelp := fs.Bool("help", false, "muestra la ayuda")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *showHelp {
		fmt.Fprint(os.Stdout, `USO:
  minecraft-codedecompiler versions [--type release] [--limit 20] [--search 1.20]

FLAGS:
  --type <t>     release|snapshot|old_beta|old_alpha|all (defecto: release)
  --limit <n>    máximo (defecto: 20, 0 = todas)
  --search <s>   filtrar por substring (ej. 1.20, 1.8)

EJEMPLOS:
  minecraft-codedecompiler versions
  minecraft-codedecompiler versions --type all --search 1.8 --limit 30
  minecraft-codedecompiler versions --type snapshot --limit 10
`)
		return 0
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	m, err := mojang.FetchManifest(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: manifest: %v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stdout, "latest: release=%s snapshot=%s (total %d)\n", m.Latest.Release, m.Latest.Snapshot, len(m.Versions))
	shown := 0
	for _, v := range m.Versions {
		if *typ != "all" && v.Type != *typ {
			continue
		}
		if *search != "" && !strings.Contains(v.ID, *search) {
			continue
		}
		fmt.Fprintf(os.Stdout, "  %-22s %-10s %s\n", v.ID, v.Type, v.ReleaseTime)
		shown++
		if *limit > 0 && shown >= *limit {
			break
		}
	}
	fmt.Fprintf(os.Stdout, "(mostrando %d)\n", shown)
	return 0
}
