package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/NovaStepStudio/Minecraft-CodeDecompiler/internal/assets"
)

// versionAssetsLayout devuelve rutas del workspace de assets.
type assetsLayout struct {
	root      string // versions/<id>
	indexJSON string // versions/<id>/asset_index.json
	versionJS string // versions/<id>/version.json
	clientJar string
	objects   string // versions/<id>/assets/objects
	indexes   string // versions/<id>/assets/indexes
	virtual   string // versions/<id>/assets/virtual/<assetId>
	clientAss string // versions/<id>/client-assets
	merged    string // versions/<id>/merged-assets
}

func layoutFor(outBase, version string) (*assetsLayout, error) {
	root := filepath.Join(outBase, version)
	aid, err := assets.VersionAssetID(filepath.Join(root, "version.json"))
	if err != nil {
		return nil, fmt.Errorf("resolviendo assetIndex: %w (¿fetch --version %s primero?)", err, version)
	}
	return &assetsLayout{
		root:      root,
		indexJSON: filepath.Join(root, "asset_index.json"),
		versionJS: filepath.Join(root, "version.json"),
		clientJar: filepath.Join(root, "client.jar"),
		objects:   filepath.Join(root, "assets", "objects"),
		indexes:   filepath.Join(root, "assets", "indexes"),
		virtual:   filepath.Join(root, "assets", "virtual", aid),
		clientAss: filepath.Join(root, "client-assets"),
		merged:    filepath.Join(root, "merged-assets"),
	}, nil
}

func cmdAssets(args []string) int {
	if len(args) == 0 {
		printAssetsHelp()
		return 0
	}
	sub, subArgs := args[0], args[1:]
	switch sub {
	case "download":
		return cmdAssetsDownload(subArgs)
	case "build":
		return cmdAssetsBuild(subArgs)
	case "help", "--help", "-h":
		printAssetsHelp()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "error: subcomando assets desconocido %q\n\n", sub)
		printAssetsHelp()
		return 1
	}
}

func printAssetsHelp() {
	fmt.Fprint(os.Stdout, `USO:
  minecraft-codedecompiler assets download --version <id> [--jobs 12] [--limit 0]
  minecraft-codedecompiler assets build --version <id> [--jobs 12] [--limit 0]

DESCRIPCIÓN:
  download: descarga TODOS los objetos del índice a assets/objects/
            (content-addressed, como el launcher) + copia del índice a
            assets/indexes/. Verifica sha1+size. Idempotente.
  build:    asegura objetos + materializa árbol con NOMBRES REALES en
            assets/virtual/<id>/ + extrae assets/ y data/ de client.jar a
            client-assets/ + une ambos en merged-assets/ (como mcasset.cloud).

FLAGS:
  --version <id>  versión [requerido]
  --out <dir>     base (defecto: versions)
  --jobs <n>      descargas paralelas (defecto: 12)
  --limit <n>     solo primeros N objetos (0 = todos; para pruebas)

EJEMPLOS:
  minecraft-codedecompiler assets download --version 1.20.1
  minecraft-codedecompiler assets build --version 1.20.1
  minecraft-codedecompiler assets download --version 1.20.1 --limit 20  (prueba rápida)

TAMAÑOS: 1.8.9 ~110MB (734 objetos), 1.20.1 ~621MB (3598 objetos).
`)
}

func assetsFlags(name string, args []string) (mcVersion, outBase string, jobs, limit, code int) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	mcVersionP := fs.String("version", "", "versión [requerido]")
	outBaseP := fs.String("out", "versions", "directorio base")
	jobsP := fs.Int("jobs", 12, "descargas paralelas")
	limitP := fs.Int("limit", 0, "solo primeros N objetos (0 = todos)")
	helpP := fs.Bool("help", false, "muestra la ayuda")
	if err := fs.Parse(args); err != nil {
		return "", "", 0, 0, 2
	}
	if *helpP {
		printAssetsHelp()
		return "", "", 0, 0, 1
	}
	if *mcVersionP == "" && fs.NArg() > 0 {
		*mcVersionP = fs.Arg(0)
	}
	if *mcVersionP == "" {
		fmt.Fprint(os.Stderr, "error: falta --version <id>\n")
		return "", "", 0, 0, 2
	}
	return *mcVersionP, *outBaseP, *jobsP, *limitP, 0
}

func cmdAssetsDownload(args []string) int {
	mcVersion, outBase, jobs, limit, code := assetsFlags("assets download", args)
	if code == 1 {
		return 0 // --help
	}
	if code != 0 {
		return code
	}
	lay, err := layoutFor(outBase, mcVersion)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	if err := ensureObjectsAndIndex(ctx, lay, jobs, limit); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

func cmdAssetsBuild(args []string) int {
	mcVersion, outBase, jobs, limit, code := assetsFlags("assets build", args)
	if code == 1 {
		return 0 // --help
	}
	if code != 0 {
		return code
	}
	lay, err := layoutFor(outBase, mcVersion)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	if err := ensureObjectsAndIndex(ctx, lay, jobs, limit); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	idx, _, err := assets.ParseIndex(lay.indexJSON)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	if _, err := assets.Materialize(idx, lay.objects, lay.virtual, limit); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	if _, err := os.Stat(lay.clientJar); err == nil {
		if _, err := assets.ExtractClientAssets(lay.clientJar, lay.clientAss); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		if err := assets.Merge(lay.virtual, lay.clientAss, lay.merged); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		fmt.Fprintf(os.Stdout, "assets utilizables en:\n  %s\n  %s\n  %s\n", lay.virtual, lay.clientAss, lay.merged)
	} else {
		fmt.Fprintf(os.Stdout, "assets utilizables en:\n  %s\n(sin client.jar: no hay client-assets/merged)\n", lay.virtual)
	}
	return 0
}

// ensureObjectsAndIndex descarga objetos y deja copia del índice en assets/indexes/.
func ensureObjectsAndIndex(ctx context.Context, lay *assetsLayout, jobs, limit int) error {
	idx, total, err := assets.ParseIndex(lay.indexJSON)
	if err != nil {
		return err
	}
	aid, err := assets.VersionAssetID(lay.versionJS)
	if err != nil {
		return err
	}
	n := len(idx.Objects)
	if limit > 0 && limit < n {
		n = limit
	}
	fmt.Fprintf(os.Stderr, "[..] índice: %d objetos (%d con límite), %d bytes totales\n", len(idx.Objects), n, total)
	if _, err := assets.DownloadObjects(ctx, idx, lay.objects, jobs, limit); err != nil {
		return err
	}
	// Copia launcher-compat del índice: assets/indexes/<id>.json
	dst := filepath.Join(lay.indexes, aid+".json")
	if _, err := os.Stat(dst); err != nil {
		data, rerr := os.ReadFile(lay.indexJSON)
		if rerr != nil {
			return rerr
		}
		if err := os.MkdirAll(lay.indexes, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "[ok] índice -> %s\n", dst)
	} else {
		fmt.Fprintf(os.Stderr, "[ok] índice ya existe (%s)\n", dst)
	}
	return nil
}
