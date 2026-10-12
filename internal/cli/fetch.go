package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/NovaStepStudio/Minecraft-CodeDecompiler/internal/assets"
	"github.com/NovaStepStudio/Minecraft-CodeDecompiler/internal/mojang"
	"github.com/NovaStepStudio/Minecraft-CodeDecompiler/internal/store"
)

func cmdFetch(args []string) int {
	fs := flag.NewFlagSet("fetch", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	mcVersion := fs.String("version", "", "versión de Minecraft (ej. 1.20.1, 1.8.9) [requerido]")
	outBase := fs.String("out", "versions", "directorio base de salida")
	side := fs.String("side", "client", "qué descargar: client|server|both")
	skipJar := fs.Bool("skip-jar", false, "no descargar jars")
	skipMappings := fs.Bool("skip-mappings", false, "no descargar mappings")
	skipAssets := fs.Bool("skip-assets", false, "no descargar índice de assets")
	withObjects := fs.Bool("with-objects", false, "descargar TODOS los objetos a assets/objects/ (pesado)")
	materialize := fs.Bool("materialize", false, "construir assets UTILIZABLES (implica --with-objects)")
	jobs := fs.Int("jobs", 12, "descargas paralelas de objetos")
	manifestURL := fs.String("manifest-url", "", "override del manifest (debug)")
	showHelp := fs.Bool("help", false, "muestra la ayuda")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	// Permitir versión posicional: `fetch 1.20.1`
	if *mcVersion == "" && fs.NArg() > 0 {
		*mcVersion = fs.Arg(0)
	}
	if *showHelp {
		fmt.Fprint(os.Stdout, `USO:
  minecraft-codedecompiler fetch --version <id> [flags]
  minecraft-codedecompiler download-to-extract --version <id>   (alias)

FLAGS:
  --version <id>     versión (ej. 1.20.1, 1.8.9) [requerido]
  --out <dir>        base de salida (defecto: versions) -> versions/<id>/
  --side <s>         client|server|both (defecto: client)
  --skip-jar         no descargar client.jar/server.jar
  --skip-mappings    no descargar client_mappings.txt/server_mappings.txt
  --skip-assets      no descargar asset_index.json
  --with-objects     descargar TODOS los objetos (1.20.1 ~621MB, 1.8.9 ~110MB)
  --materialize      assets UTILIZABLES con nombres reales (implica --with-objects)
  --jobs <n>         descargas paralelas (defecto: 12)

EJEMPLOS:
  minecraft-codedecompiler fetch --version 1.20.1
  minecraft-codedecompiler fetch --version 1.8.9 --side both
  minecraft-codedecompiler fetch --version 1.20.1 --with-objects --materialize
  minecraft-codedecompiler download-to-extract --version 1.20.1 --out ./versions

DESCARGA (sin librerías):
  version.json, client.jar, client_mappings.txt (si existe),
  server.jar + server_mappings.txt (si --side incluye server),
  asset_index.json (índice, no los objetos).
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
	wantClient := *side == "client" || *side == "both"
	wantServer := *side == "server" || *side == "both"

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	manifestURLStr := mojang.ManifestURL
	if *manifestURL != "" {
		manifestURLStr = *manifestURL
	}
	fmt.Fprintf(os.Stderr, "[..] manifest: %s\n", manifestURLStr)
	manifest, err := mojang.FetchManifest(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: manifest: %v\n", err)
		return 1
	}
	entry, err := mojang.FindVersion(manifest, *mcVersion)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "[..] versión %s (%s): %s\n", entry.ID, entry.Type, entry.URL)
	detail, err := mojang.FetchVersionDetail(ctx, entry.URL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: detalle de versión: %v\n", err)
		return 1
	}

	paths := store.ForVersion(*outBase, detail.ID)
	if err := os.MkdirAll(paths.Root, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "error: mkdir: %v\n", err)
		return 1
	}
	// Guardar version.json CRUDO (completo, con librerías) verificando sha1 del manifest.
	if err := mojang.DownloadURL(ctx, entry.URL, paths.VersionJSON, "version.json", entry.SHA1, 0); err != nil {
		fmt.Fprintf(os.Stderr, "error: guardar version.json: %v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "[ok] version.json -> %s (java %d, assets %s)\n",
		paths.VersionJSON, detail.JavaVersion.MajorVersion, detail.Assets)

	failed := false
	if !*skipJar {
		if wantClient {
			if detail.Downloads.Client == nil {
				fmt.Fprint(os.Stderr, "[!!] esta versión no tiene client jar\n")
				failed = true
			} else if err := mojang.DownloadArtifact(ctx, detail.Downloads.Client, paths.ClientJar, "client.jar"); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				failed = true
			}
		}
		if wantServer {
			if detail.Downloads.Server == nil {
				fmt.Fprint(os.Stderr, "[!!] esta versión no tiene server jar\n")
				failed = true
			} else if err := mojang.DownloadArtifact(ctx, detail.Downloads.Server, paths.ServerJar, "server.jar"); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				failed = true
			}
		}
	}
	if !*skipMappings {
		if wantClient {
			if detail.Downloads.ClientMappings == nil {
				fmt.Fprintf(os.Stderr, "[..] sin client_mappings oficiales (normal en < 1.14.4): se descompilará ofuscado\n")
			} else if err := mojang.DownloadArtifact(ctx, detail.Downloads.ClientMappings, paths.ClientMappings, "client_mappings.txt"); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				failed = true
			}
		}
		if wantServer {
			if detail.Downloads.ServerMappings == nil {
				fmt.Fprintf(os.Stderr, "[..] sin server_mappings oficiales (normal en < 1.14.4)\n")
			} else if err := mojang.DownloadArtifact(ctx, detail.Downloads.ServerMappings, paths.ServerMappings, "server_mappings.txt"); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				failed = true
			}
		}
	}
	if !*skipAssets {
		if detail.AssetIndex == nil {
			fmt.Fprint(os.Stderr, "[!!] esta versión no declara assetIndex\n")
		} else {
			a := &mojang.Artifact{SHA1: detail.AssetIndex.SHA1, Size: detail.AssetIndex.Size, URL: detail.AssetIndex.URL}
			if err := mojang.DownloadArtifact(ctx, a, paths.AssetIndex, "asset_index.json"); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				failed = true
			} else {
				fmt.Fprintf(os.Stderr, "[..] assetIndex id=%s totalSize=%d bytes (objetos no descargados)\n",
					detail.AssetIndex.ID, detail.AssetIndex.TotalSize)
			}
		}
	}

	if *materialize && !*withObjects {
		*withObjects = true
	}
	if *withObjects && !*skipAssets && detail.AssetIndex != nil {
		lay, lerr := layoutFor(*outBase, detail.ID)
		if lerr != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", lerr)
			failed = true
		} else if err := ensureObjectsAndIndex(ctx, lay, *jobs, 0); err != nil {
			fmt.Fprintf(os.Stderr, "error: objetos: %v\n", err)
			failed = true
		} else if *materialize {
			idx, _, perr := assets.ParseIndex(lay.indexJSON)
			if perr != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", perr)
				failed = true
			} else {
				if _, err := assets.Materialize(idx, lay.objects, lay.virtual, 0); err != nil {
					fmt.Fprintf(os.Stderr, "error: %v\n", err)
					failed = true
				}
				if wantClient {
					if _, err := os.Stat(paths.ClientJar); err == nil {
						if _, err := assets.ExtractClientAssets(paths.ClientJar, lay.clientAss); err != nil {
							fmt.Fprintf(os.Stderr, "error: %v\n", err)
							failed = true
						} else if err := assets.Merge(lay.virtual, lay.clientAss, lay.merged); err != nil {
							fmt.Fprintf(os.Stderr, "error: %v\n", err)
							failed = true
						}
					}
				}
			}
		}
	}

	fmt.Fprintf(os.Stderr, "\n[ok] listo: %s\n", paths.Root)
	printFetchSummary(paths.Root, detail, wantClient, wantServer)
	if failed {
		fmt.Fprint(os.Stderr, "[!!] hubo fallos parciales (ver arriba), exit 1\n")
		return 1
	}
	return 0
}

func printFetchSummary(root string, detail *mojang.VersionDetail, wantClient, wantServer bool) {
	fmt.Fprintf(os.Stdout, "versión: %s (%s)  java=%d  assets=%s\n",
		detail.ID, detail.Type, detail.JavaVersion.MajorVersion, detail.Assets)
	fmt.Fprintf(os.Stdout, "dir: %s\n", root)
	ls := func(p, label string) {
		fi, err := os.Stat(p)
		if err != nil {
			fmt.Fprintf(os.Stdout, "  [x] %s: ausente\n", label)
			return
		}
		fmt.Fprintf(os.Stdout, "  [ok] %s: %s (%d bytes)\n", label, filepath.Base(p), fi.Size())
	}
	ls(filepath.Join(root, "version.json"), "version.json")
	if wantClient {
		ls(filepath.Join(root, "client.jar"), "client.jar")
		if detail.HasClientMappings() {
			ls(filepath.Join(root, "client_mappings.txt"), "client_mappings.txt")
		} else {
			fmt.Fprint(os.Stdout, "  [..] client_mappings.txt: no oficial (versión vieja)\n")
		}
	}
	if wantServer {
		ls(filepath.Join(root, "server.jar"), "server.jar")
		if detail.HasServerMappings() {
			ls(filepath.Join(root, "server_mappings.txt"), "server_mappings.txt")
		} else {
			fmt.Fprint(os.Stdout, "  [..] server_mappings.txt: no oficial (versión vieja)\n")
		}
	}
	ls(filepath.Join(root, "asset_index.json"), "asset_index.json")
	fmt.Fprint(os.Stdout, "siguiente: remap --version "+detail.ID+"  ->  decompile --version "+detail.ID+" --engine both\n")
}
