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
	MCPConfigBase    = "https://raw.githubusercontent.com/MinecraftForge/MCPConfig/master"
	VersionsJSONPath = "versions.json"
)

// staleAfter: el índice MCP se re-descarga si es más viejo (siempre actualizado).
const staleAfter = 24 * time.Hour

// loadVersions lee versions.json del archive (cacheado en tools/mcp/).
// Lo re-descarga si falta, si force es true o si supera staleAfter.
// Si el refresco falla pero hay caché, avisa y sigue con la caché.
func loadVersions(ctx context.Context, toolsDir string, force bool) (map[string]map[string][]json.RawMessage, error) {
	dir := filepath.Join(toolsDir, "mcp")
	idxPath := filepath.Join(dir, VersionsJSONPath)
	st, statErr := os.Stat(idxPath)
	reason := ""
	switch {
	case statErr != nil:
		reason = "descargando índice MCP"
	case force:
		reason = "refresco forzado del índice MCP (--refresh)"
	case time.Since(st.ModTime()) > staleAfter:
		reason = "índice MCP viejo, actualizando"
	}
	if reason != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
		tmp := idxPath + ".tmp"
		if derr := downloadText(ctx, ArchiveBase+"/"+VersionsJSONPath, tmp); derr != nil {
			os.Remove(tmp)
			if statErr != nil {
				return nil, fmt.Errorf("índice MCP: %w", derr)
			}
			fmt.Fprintf(os.Stderr, "[!!] no se pudo actualizar el índice MCP (%v); usando caché\n", derr)
		} else if rerr := os.Rename(tmp, idxPath); rerr != nil {
			os.Remove(tmp)
			if statErr != nil {
				return nil, fmt.Errorf("índice MCP: %w", rerr)
			}
			fmt.Fprintf(os.Stderr, "[!!] no se pudo guardar el índice MCP (%v); usando caché\n", rerr)
		} else {
			fmt.Fprintf(os.Stderr, "[ok] índice MCP actualizado (%s)\n", reason)
		}
	}
	raw, err := os.ReadFile(idxPath)
	if err != nil {
		return nil, err
	}
	var vmap map[string]map[string][]json.RawMessage
	if err := json.Unmarshal(raw, &vmap); err != nil {
		return nil, fmt.Errorf("versions.json MCP: %w", err)
	}
	return vmap, nil
}

func bestStableKind(entry map[string][]json.RawMessage, kind string) int {
	best := 0
	for _, r := range entry[kind] {
		n, err := flexInt(r)
		if err != nil {
			continue
		}
		if n > best {
			best = n
		}
	}
	return best
}

// stripPatch quita el tercer componente: 1.12.2 -> 1.12.
func stripPatch(v string) string {
	parts := strings.Split(v, ".")
	if len(parts) > 2 {
		return strings.Join(parts[:2], ".")
	}
	return v
}

// Channel es un canal MCP resuelto: estable o snapshot, de mc o su donante.
type Channel struct {
	Kind   string // "stable" | "snapshot"
	Num    int
	CSVVer string // versión dueña de los CSVs (exacta o donante)
}

// Tag devuelve stable_22 o snapshot_20160301.
func (c Channel) Tag() string { return fmt.Sprintf("%s_%d", c.Kind, c.Num) }

// resolveChannel interpreta spec contra mc y su donante.
// spec: stable|snapshot (último) o exacto (22, stable_22, 20160301...).
func resolveChannel(vmap map[string]map[string][]json.RawMessage, mc, spec string) (Channel, error) {
	spec = strings.ToLower(strings.TrimSpace(spec))
	cands := []string{mc}
	if d := stripPatch(mc); d != mc {
		cands = append(cands, d)
	}
	kind, num := "", 0
	switch {
	case spec == "" || spec == "stable":
		kind = "stable"
	case spec == "snapshot":
		kind = "snapshot"
	default:
		s := spec
		if rest, ok := strings.CutPrefix(s, "snapshot_"); ok {
			kind, s = "snapshot", rest
		} else if rest, ok := strings.CutPrefix(s, "snapshot"); ok {
			kind, s = "snapshot", strings.TrimPrefix(rest, "_")
		} else if rest, ok := strings.CutPrefix(s, "stable_"); ok {
			kind, s = "stable", rest
		} else if rest, ok := strings.CutPrefix(s, "stable"); ok {
			kind, s = "stable", strings.TrimPrefix(rest, "_")
		}
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			return Channel{}, fmt.Errorf("--mcp-channel inválido %q (stable|snapshot|22|stable_22|20160301|snapshot_20160301)", spec)
		}
		if kind == "" {
			for _, cand := range cands {
				if e, ok := vmap[cand]; ok {
					for _, k := range []string{"stable", "snapshot"} {
						for _, r := range e[k] {
							if m, _ := flexInt(r); m == n {
								return Channel{k, n, cand}, nil
							}
						}
					}
				}
			}
			return Channel{}, fmt.Errorf("canal MCP %d no existe para %s", n, mc)
		}
		num = n
	}
	for _, cand := range cands {
		if e, ok := vmap[cand]; ok {
			if num > 0 {
				for _, r := range e[kind] {
					if m, _ := flexInt(r); m == num {
						return Channel{kind, num, cand}, nil
					}
				}
				continue
			}
			if best := bestStableKind(e, kind); best > 0 {
				return Channel{kind, best, cand}, nil
			}
		}
	}
	if num > 0 {
		return Channel{}, fmt.Errorf("canal MCP %s_%d no existe para %s", kind, num, mc)
	}
	return Channel{}, fmt.Errorf("MCP sin canal %s para %s (ni donante)", kind, mc)
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

// Ensure descarga y fusiona el mapeo MCP para mc según el canal pedido
// (stable|snapshot|exacto). Estrategias notch: srg legacy exacto ->
// MCPConfig tsrg exacto -> srg legacy donante. Caché por canal en
// tools/mcp/<mc>/<kind>-<num>/; notch compartido en .../<mc>/shared/.
// Devuelve (mcp.srg, descripción). Nunca inventa nombres.
func Ensure(ctx context.Context, toolsDir, mc, channelSpec string, refresh bool) (string, string, error) {
	// El índice se revisa SIEMPRE primero: así --refresh y la caducidad
	// de 24h funcionan aunque el fusionado ya exista (puede haber canales
	// nuevos aguas arriba y el canal resuelto cambiaría de directorio).
	vmap, err := loadVersions(ctx, toolsDir, refresh)
	if err != nil {
		return "", "", err
	}
	ch, err := resolveChannel(vmap, mc, channelSpec)
	if err != nil {
		return "", "", err
	}
	cdir := filepath.Join(toolsDir, "mcp", mc, fmt.Sprintf("%s-%d", ch.Kind, ch.Num))
	sdir := filepath.Join(toolsDir, "mcp", mc, "shared")
	fused := filepath.Join(cdir, "mcp.srg")
	srcFile := filepath.Join(cdir, "source.txt")
	if st, err := os.Stat(fused); err == nil && st.Size() > 100*1024 {
		desc := ch.Tag() + " (" + ch.CSVVer + ", cache)"
		if raw, rerr := os.ReadFile(srcFile); rerr == nil {
			desc = strings.TrimSpace(string(raw))
		}
		fmt.Fprintf(os.Stderr, "[ok] mcp.srg ya existe (%s) [%s]\n", fused, desc)
		return fused, desc, nil
	}
	if err := os.MkdirAll(cdir, 0o755); err != nil {
		return "", "", err
	}

	// 1. methods/fields.csv del canal (archive + fallbacks Maven).
	zipBase := fmt.Sprintf("mcp_%s-%d-%s.zip", ch.Kind, ch.Num, ch.CSVVer)
	zipURLs := []string{
		fmt.Sprintf("%s/mcp_%s/%d-%s/%s", ArchiveBase, ch.Kind, ch.Num, ch.CSVVer, zipBase),
		fmt.Sprintf("https://maven.minecraftforge.net/de/oceanlabs/mcp/mcp_%s/%d-%s/%s", ch.Kind, ch.Num, ch.CSVVer, zipBase),
		fmt.Sprintf("https://maven.neoforged.net/releases/de/oceanlabs/mcp/mcp_%s/%d-%s/%s", ch.Kind, ch.Num, ch.CSVVer, zipBase),
	}
	zipPath := filepath.Join(cdir, zipBase)
	if err := fetchFirst(ctx, zipURLs, zipPath, fmt.Sprintf("mcp %s %s", ch.Tag(), ch.CSVVer), 50*1024); err != nil {
		return "", "", err
	}
	methods, err := extractMember(zipPath, "methods.csv", filepath.Join(cdir, "methods.csv"))
	if err != nil {
		return "", "", err
	}
	fields, err := extractMember(zipPath, "fields.csv", filepath.Join(cdir, "fields.csv"))
	if err != nil {
		return "", "", err
	}

	// 2. Notch->searge (compartido, independiente del canal).
	joined, notchNote, err := notchShared(ctx, sdir, mc)
	if err != nil {
		return "", "", err
	}
	desc := fmt.Sprintf("%s (%s)%s", ch.Tag(), ch.CSVVer, notchNote)
	if err := fuse(joined, methods, fields, fused); err != nil {
		return "", "", err
	}
	_ = os.WriteFile(srcFile, []byte(desc), 0o644)
	fmt.Fprintf(os.Stderr, "[ok] mcp.srg fusionado [%s] -> %s\n", desc, fused)
	return fused, desc, nil
}

// notchShared resuelve el notch en dir compartido: legacy exacto ->
// MCPConfig tsrg exacto -> legacy donante. Devuelve (joined.srg, nota, err).
func notchShared(ctx context.Context, sdir, mc string) (string, string, error) {
	if err := os.MkdirAll(sdir, 0o755); err != nil {
		return "", "", err
	}
	if joined, err := legacySRG(ctx, sdir, mc); err == nil {
		return joined, "", nil
	} else {
		fmt.Fprintf(os.Stderr, "[..] sin srg legacy para %s; probando MCPConfig tsrg\n", mc)
	}
	tsrgPath := filepath.Join(sdir, "joined.tsrg")
	tsrgURL := fmt.Sprintf("%s/versions/release/%s/joined.tsrg", MCPConfigBase, mc)
	if derr := downloadText(ctx, tsrgURL, tsrgPath); derr == nil {
		if fi, serr := os.Stat(tsrgPath); serr == nil && fi.Size() >= 100*1024 {
			joined := filepath.Join(sdir, "joined-from-tsrg.srg")
			if terr := tsrgToLegacy(tsrgPath, joined); terr == nil {
				return joined, " +tsrg MCPConfig", nil
			} else {
				fmt.Fprintf(os.Stderr, "[!!] tsrg inválido: %v\n", terr)
			}
		}
	}
	if donor := stripPatch(mc); donor != mc {
		if joined, err := legacySRG(ctx, sdir, donor); err == nil {
			return joined, fmt.Sprintf(" +donante %s (el probe validará)", donor), nil
		}
	}
	return "", "", fmt.Errorf("sin notch para %s: ni srg legacy, ni MCPConfig tsrg, ni donante", mc)
}

// legacySRG descarga el zip srg legacy de ver y extrae joined.srg.
func legacySRG(ctx context.Context, dir, ver string) (string, error) {
	srgZip := filepath.Join(dir, fmt.Sprintf("mcp-%s-srg.zip", ver))
	srgURLs := []string{fmt.Sprintf("%s/mcp/%s/mcp-%s-srg.zip", ArchiveBase, ver, ver)}
	if err := fetchFirst(ctx, srgURLs, srgZip, "mcp srg "+ver, 50*1024); err != nil {
		return "", err
	}
	return extractMember(srgZip, "joined.srg", filepath.Join(dir, "joined.srg"))
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
	var nCL, nFD, nMD, namedMD, namedFD int
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
				namedFD++
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
				namedMD++
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
	fmt.Fprintf(os.Stderr, "[ok] nombres MCP aplicados: %d/%d metodos, %d/%d fields (resto queda searge)\n",
		namedMD, nMD, namedFD, nFD)
	if nMD > 0 && namedMD*10 < nMD {
		fmt.Fprintf(os.Stderr, "[!!] cobertura MCP muy baja: los CSVs podrian no corresponder al srg de esta versión\n")
	}
	return nil
}

// tsrgToLegacy convierte joined.tsrg de MCPConfig (notch->searge, sin
// cabecera, clases sin prefijo y miembros indentados con tab) al SRG legacy
// que entiende fuse(). Los descriptores derechos se convierten de notch a
// searge con el mapa de clases (dos pasadas).
func tsrgToLegacy(tsrgPath, legacyPath string) error {
	data, err := os.ReadFile(tsrgPath)
	if err != nil {
		return err
	}
	type mem struct {
		kind             string // "F" o "M"
		name, desc, srgn string
	}
	type cls struct {
		notch, srg string
		members    []mem
	}
	var classes []cls
	classMap := map[string]string{}
	cur := -1
	for i, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimRight(raw, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.HasPrefix(line, "\t") || strings.HasPrefix(line, " ") {
			if cur < 0 {
				return fmt.Errorf("tsrg:%d miembro sin clase: %q", i+1, line)
			}
			t := strings.Fields(strings.TrimSpace(line))
			if len(t) == 2 {
				classes[cur].members = append(classes[cur].members, mem{"F", t[0], "", t[1]})
			} else if len(t) == 3 {
				classes[cur].members = append(classes[cur].members, mem{"M", t[0], t[1], t[2]})
			} else {
				return fmt.Errorf("tsrg:%d miembro malformado: %q", i+1, line)
			}
			continue
		}
		t := strings.Fields(line)
		if len(t) != 2 {
			return fmt.Errorf("tsrg:%d clase malformada: %q", i+1, line)
		}
		classes = append(classes, cls{t[0], t[1], nil})
		classMap[t[0]] = t[1]
		cur = len(classes) - 1
	}
	if len(classes) < 500 {
		return fmt.Errorf("tsrg con solo %d clases, posible fichero corrupto", len(classes))
	}
	var b strings.Builder
	for _, c := range classes {
		fmt.Fprintf(&b, "CL: %s %s\n", c.notch, c.srg)
		for _, mm := range c.members {
			if mm.kind == "F" {
				fmt.Fprintf(&b, "FD: %s/%s %s/%s\n", c.notch, mm.name, c.srg, mm.srgn)
			} else {
				fmt.Fprintf(&b, "MD: %s/%s %s %s/%s %s\n",
					c.notch, mm.name, mm.desc, c.srg, mm.srgn, remapDesc(mm.desc, classMap))
			}
		}
	}
	return os.WriteFile(legacyPath, []byte(b.String()), 0o644)
}

// remapDesc reescribe un descriptor JVM cambiando Lnotch; por su searge.
func remapDesc(desc string, cls map[string]string) string {
	var b strings.Builder
	for i := 0; i < len(desc); {
		if desc[i] != 'L' {
			b.WriteByte(desc[i])
			i++
			continue
		}
		j := strings.IndexByte(desc[i:], ';')
		if j < 0 {
			b.WriteString(desc[i:])
			break
		}
		name := desc[i+1 : i+j]
		if s, ok := cls[name]; ok {
			name = s
		}
		b.WriteString("L" + name + ";")
		i += j + 1
	}
	return b.String()
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
