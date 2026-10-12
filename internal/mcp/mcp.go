// Package mcp obtiene nombres legibles para versiones SIN mappings
// oficiales (< 1.14.4) usando MCP (ModCoderPack, el estándar de la era).
//
// Cadena: joined.srg (notch->searge) + methods/fields.csv (searge->mcp)
// se fusionan en Go en UN solo mcp.srg (notch->mcp) y se aplica con
// SpecialSource en una pasada. Sin Gradle, solo java + jars.
package mcp

import (
	"archive/zip"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/NovaStepStudio/Minecraft-CodeDecompiler/internal/tools"
)

const (
	ArchiveBase      = "https://raw.githubusercontent.com/ModCoderPack/MCPMappingsArchive/master"
	VersionsJSONPath = "versions.json"
)

// ResolveChannel devuelve el último canal estable MCP para mc (ej. 22 para
// 1.8.9) leyendo versions.json (cacheado en tools/mcp/).
func ResolveChannel(ctx context.Context, toolsDir, mc string) (int, error) {
	dir := filepath.Join(toolsDir, "mcp")
	idxPath := filepath.Join(dir, VersionsJSONPath)
	if _, err := os.Stat(idxPath); err != nil {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return 0, err
		}
		if err := downloadText(ctx, ArchiveBase+"/"+VersionsJSONPath, idxPath); err != nil {
			return 0, fmt.Errorf("índice MCP: %w", err)
		}
	}
	raw, err := os.ReadFile(idxPath)
	if err != nil {
		return 0, err
	}
	var vmap map[string]map[string][]json.RawMessage
	if err := json.Unmarshal(raw, &vmap); err != nil {
		return 0, fmt.Errorf("versions.json MCP: %w", err)
	}
	entry, ok := vmap[mc]
	if !ok {
		return 0, fmt.Errorf("MCP no lista la versión %s", mc)
	}
	best := 0
	for _, r := range entry["stable"] {
		n, err := flexInt(r)
		if err != nil {
			continue
		}
		if n > best {
			best = n
		}
	}
	if best == 0 {
		return 0, fmt.Errorf("MCP sin canal estable para %s (solo snapshots)", mc)
	}
	return best, nil
}

func flexInt(r json.RawMessage) (int, error) {
	s := strings.Trim(string(r), `"`)
	return strconv.Atoi(s)
}

func downloadText(ctx context.Context, url, dest string) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "minecraft-codedecompiler/1.0.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, resp.Body)
	f.Close()
	return err
}

// Ensure descarga y fusiona el mapeo MCP para mc. Devuelve la ruta de mcp.srg.
func Ensure(ctx context.Context, toolsDir, mc string) (string, error) {
	ch, err := ResolveChannel(ctx, toolsDir, mc)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(toolsDir, "mcp", mc)
	fused := filepath.Join(dir, "mcp.srg")
	if st, err := os.Stat(fused); err == nil && st.Size() > 100*1024 {
		fmt.Fprintf(os.Stderr, "[ok] mcp.srg ya existe (%s)\n", fused)
		return fused, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	// 1. joined.srg (notch->searge).
	srgZip := filepath.Join(dir, fmt.Sprintf("mcp-%s-srg.zip", mc))
	srgURLs := []string{fmt.Sprintf("%s/mcp/%s/mcp-%s-srg.zip", ArchiveBase, mc, mc)}
	if err := fetchFirst(ctx, srgURLs, srgZip, "mcp srg "+mc, 50*1024); err != nil {
		return "", err
	}
	// 2. methods/fields.csv del canal estable (con docs; fallback Maven).
	stableZip := filepath.Join(dir, fmt.Sprintf("mcp_stable-%d-%s.zip", ch, mc))
	stableURLs := []string{
		fmt.Sprintf("%s/mcp_stable/%d-%s/mcp_stable-%d-%s.zip", ArchiveBase, ch, mc, ch, mc),
		fmt.Sprintf("https://maven.minecraftforge.net/de/oceanlabs/mcp/mcp_stable/%d-%s/mcp_stable-%d-%s.zip", ch, mc, ch, mc),
		fmt.Sprintf("https://maven.neoforged.net/releases/de/oceanlabs/mcp/mcp_stable/%d-%s/mcp_stable-%d-%s.zip", ch, mc, ch, mc),
	}
	if err := fetchFirst(ctx, stableURLs, stableZip, fmt.Sprintf("mcp stable_%d %s", ch, mc), 50*1024); err != nil {
		return "", err
	}

	joined, err := extractMember(srgZip, "joined.srg", filepath.Join(dir, "joined.srg"))
	if err != nil {
		return "", err
	}
	methods, err := extractMember(stableZip, "methods.csv", filepath.Join(dir, "methods.csv"))
	if err != nil {
		return "", err
	}
	fields, err := extractMember(stableZip, "fields.csv", filepath.Join(dir, "fields.csv"))
	if err != nil {
		return "", err
	}
	if err := fuse(joined, methods, fields, fused); err != nil {
		return "", err
	}
	fmt.Fprintf(os.Stderr, "[ok] mcp.srg fusionado (stable_%d) -> %s\n", ch, fused)
	return fused, nil
}

func fetchFirst(ctx context.Context, urls []string, dest, label string, minSize int64) error {
	var lastErr error
	for _, u := range urls {
		fmt.Fprintf(os.Stderr, "[..] descargando %s desde %s\n", label, u)
		if err := tools.FetchURL(ctx, u, dest, label, minSize); err != nil {
			fmt.Fprintf(os.Stderr, "[!!] fallo: %v\n", err)
			lastErr = err
			continue
		}
		return nil
	}
	return fmt.Errorf("sin espejo válido para %s: %v", label, lastErr)
}

func extractMember(zipPath, member, dest string) (string, error) {
	if _, err := os.Stat(dest); err == nil {
		return dest, nil
	}
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", err
	}
	defer zr.Close()
	for _, f := range zr.File {
		base := f.Name
		if i := strings.LastIndex(base, "/"); i >= 0 {
			base = base[i+1:]
		}
		if base != member {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		out, err := os.Create(dest)
		if err != nil {
			rc.Close()
			return "", err
		}
		_, err = io.Copy(out, rc)
		rc.Close()
		out.Close()
		if err != nil {
			return "", err
		}
		fmt.Fprintf(os.Stderr, "[ok] %s extraído de %s\n", member, filepath.Base(zipPath))
		return dest, nil
	}
	return "", fmt.Errorf("%s no encontrado en %s", member, zipPath)
}

// fuse combina joined.srg + CSVs en mcp.srg (notch->mcp en una pasada).
// Clases (CL/PK) se copian tal cual: searge == mcp. Miembros FD/MD:
// se sustituye el nombre searge por el MCP; los descriptores derechos
// ya vienen en namespace final y se dejan intactos.
func fuse(joinedSRG, methodsCSV, fieldsCSV, out string) error {
	methods, err := readCSVMap(methodsCSV)
	if err != nil {
		return err
	}
	fields, err := readCSVMap(fieldsCSV)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(joinedSRG)
	if err != nil {
		return err
	}
	var b strings.Builder
	var nCL, nFD, nMD int
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "PK: ") || strings.HasPrefix(line, "CL: "):
			b.WriteString(line + "\n")
			nCL++
		case strings.HasPrefix(line, "FD: "):
			// FD: notchOwner/notchName seargeOwner/seargeName
			t := strings.Fields(line[4:])
			if len(t) != 2 {
				return fmt.Errorf("FD malformado: %q", line)
			}
			owner, name := splitMember(t[1])
			if mcp, ok := fields[name]; ok {
				name = mcp
			}
			b.WriteString("FD: " + t[0] + " " + owner + "/" + name + "\n")
			nFD++
		case strings.HasPrefix(line, "MD: "):
			// MD: notchOwner/notchName notchDesc seargeOwner/seargeName seargeDesc
			t := strings.Fields(line[4:])
			if len(t) != 4 {
				return fmt.Errorf("MD malformado: %q", line)
			}
			owner, name := splitMember(t[2])
			if mcp, ok := methods[name]; ok {
				name = mcp
			}
			b.WriteString("MD: " + t[0] + " " + t[1] + " " + owner + "/" + name + " " + t[3] + "\n")
			nMD++
		default:
			return fmt.Errorf("línea SRG desconocida: %q", line)
		}
	}
	// Validación suelta: un mapeo roto no debe colarse.
	if nCL < 500 || nMD < 1000 || len(methods) < 1000 || len(fields) < 1000 {
		return fmt.Errorf("mapeo incompleto: CL=%d MD=%d methods=%d fields=%d", nCL, nMD, len(methods), len(fields))
	}
	if err := os.WriteFile(out, []byte(b.String()), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "[ok] fusión: CL/PK=%d FD=%d MD=%d (methods.csv=%d fields.csv=%d)\n",
		nCL, nFD, nMD, len(methods), len(fields))
	return nil
}

func splitMember(ref string) (owner, name string) {
	if i := strings.LastIndex(ref, "/"); i >= 0 {
		return ref[:i], ref[i+1:]
	}
	return "", ref
}

// readCSVMap lee methods/fields.csv (searge,name,side,desc) -> searge:mcp.
func readCSVMap(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	records, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("csv %s: %w", path, err)
	}
	m := map[string]string{}
	for _, rec := range records {
		if len(rec) < 2 || rec[0] == "searge" || rec[0] == "" {
			continue
		}
		m[strings.TrimSpace(rec[0])] = strings.TrimSpace(rec[1])
	}
	return m, nil
}

// SortedKeys util para diagnósticos.
func SortedKeys(m map[string]string) []string {
	k := make([]string, 0, len(m))
	for s := range m {
		k = append(k, s)
	}
	sort.Strings(k)
	return k
}
