// Package tools gestiona los motores de decompilación externos (jars Java).
//
// Motores elegidos (investigación 2026):
//   - CFR 0.152 (MIT, github.com/leibnitz27/cfr): mejor con código ofuscado,
//     ideal para 1.6-1.13. Corre en JRE 8+.
//   - Vineflower 1.12.0 (Apache-2.0, github.com/Vineflower/vineflower):
//     continuación activa de QuiltFlower/FernFlower, estándar de la
//     comunidad (Fabric Loom, Quilt, NeoForge). Requiere Java 17+.
//
// QuiltFlower está archivado y redirige a Vineflower: no se usa.
// FernFlower original no publica binarios: no se empaqueta.
package tools

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	CFRVersion        = "0.152"
	VineflowerVersion = "1.12.0"

	CFRFileName        = "cfr-0.152.jar"
	VineflowerFileName = "vineflower-1.12.0.jar"
)

var (
	// URLs primarias (GitHub releases) con fallback a Maven Central.
	CFRURLs = []string{
		"https://github.com/leibnitz27/cfr/releases/download/0.152/cfr-0.152.jar",
		"https://www.benf.org/other/cfr/cfr-0.152.jar",
		"https://repo1.maven.org/maven2/org/benf/cfr/0.152/cfr-0.152.jar",
	}
	VineflowerURLs = []string{
		"https://github.com/Vineflower/vineflower/releases/download/1.12.0/vineflower-1.12.0.jar",
		"https://repo1.maven.org/maven2/org/vineflower/vineflower/1.12.0/vineflower-1.12.0.jar",
	}
)

// DefaultDir devuelve el directorio de herramientas.
// Prioridad: flag --tools-dir > env MCDECOMP_TOOLS > ./tools
func DefaultDir(flagDir string) string {
	if flagDir != "" {
		return flagDir
	}
	if env := os.Getenv("MCDECOMP_TOOLS"); env != "" {
		return env
	}
	return "tools"
}

func CFRPath(dir string) string        { return filepath.Join(dir, CFRFileName) }
func VineflowerPath(dir string) string { return filepath.Join(dir, VineflowerFileName) }

// Ensure instala los jars que falten (cfr, vineflower o ambos).
// engine: "cfr" | "vine" | "vineflower" | "both" | "all"
func Ensure(ctx context.Context, dir, engine string) error {
	engine = normalizeEngine(engine)
	if engine == "cfr" || engine == "both" {
		if err := ensureOne(ctx, dir, CFRFileName, CFRURLs, "CFR "+CFRVersion); err != nil {
			return err
		}
	}
	if engine == "vine" || engine == "both" {
		if err := ensureOne(ctx, dir, VineflowerFileName, VineflowerURLs, "Vineflower "+VineflowerVersion); err != nil {
			return err
		}
	}
	return nil
}

func normalizeEngine(e string) string {
	e = strings.ToLower(strings.TrimSpace(e))
	switch e {
	case "vineflower", "vine", "vf":
		return "vine"
	case "cfr":
		return "cfr"
	case "", "both", "all":
		return "both"
	default:
		return "both"
	}
}

// NormalizeEngine expone la normalización para otros paquetes.
func NormalizeEngine(e string) string { return normalizeEngine(e) }

func ensureOne(ctx context.Context, dir, file string, urls []string, label string) error {
	dest := filepath.Join(dir, file)
	if st, err := os.Stat(dest); err == nil && st.Size() > 1024*1024 {
		fmt.Fprintf(os.Stderr, "[ok] %s ya existe (%s, %d bytes)\n", label, dest, st.Size())
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var lastErr error
	for _, u := range urls {
		fmt.Fprintf(os.Stderr, "[..] descargando %s desde %s\n", label, u)
		if err := download(ctx, u, dest); err != nil {
			fmt.Fprintf(os.Stderr, "[!!] fallo %s: %v\n", u, err)
			lastErr = err
			continue
		}
		fmt.Fprintf(os.Stderr, "[ok] %s -> %s\n", label, dest)
		return nil
	}
	return fmt.Errorf("no se pudo descargar %s: %v", label, lastErr)
}

func download(ctx context.Context, url, dest string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
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
		return fmt.Errorf("%s", resp.Status)
	}
	tmp := dest + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	h := sha1.New()
	n, err := io.Copy(io.MultiWriter(f, h), resp.Body)
	f.Close()
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if n < 300*1024 {
		os.Remove(tmp)
		return fmt.Errorf("fichero sospechosamente pequeño (%d bytes), posible página de error", n)
	}
	if err := checkZip(tmp); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "     sha1 %s\n", hex.EncodeToString(h.Sum(nil))[:12])
	return nil
}

// Status informa qué herramientas están instaladas.
func Status(dir string) (cfrOK bool, vineOK bool) {
	if st, err := os.Stat(CFRPath(dir)); err == nil && st.Size() > 0 {
		cfrOK = true
	}
	if st, err := os.Stat(VineflowerPath(dir)); err == nil && st.Size() > 0 {
		vineOK = true
	}
	return
}

// ToolStatus es una fila de FullStatus.
type ToolStatus struct {
	Name string
	OK   bool
}

func sizeOK(path string, min int64) bool {
	st, err := os.Stat(path)
	return err == nil && st.Size() >= min
}

// FullStatus informa de las 4 herramientas.
func FullStatus(dir string) []ToolStatus {
	return []ToolStatus{
		{"cfr-" + CFRVersion + ".jar (descompilador legacy)", sizeOK(CFRPath(dir), 1024*1024)},
		{"vineflower-" + VineflowerVersion + ".jar (descompilador moderno)", sizeOK(VineflowerPath(dir), 1024*1024)},
		{TinyRemapperFileName + " (remap oficial Mojang)", sizeOK(TinyRemapperPath(dir), 300*1024)},
		{SpecialSourceFileName + " (remap MCP legacy)", sizeOK(SpecialSourcePath(dir), 1024*1024)},
	}
}

// EnsureOne instala una herramienta: cfr|vine|tiny|special.
func EnsureOne(ctx context.Context, dir, which string) error {
	switch which {
	case "cfr":
		return Ensure(ctx, dir, "cfr")
	case "vine":
		return Ensure(ctx, dir, "vine")
	case "tiny":
		_, err := EnsureTinyRemapper(ctx, dir)
		return err
	case "special":
		_, err := EnsureSpecialSource(ctx, dir)
		return err
	default:
		return fmt.Errorf("herramienta desconocida %q (cfr|vine|tiny|special)", which)
	}
}

// EnsureAll instala las 4 herramientas.
func EnsureAll(ctx context.Context, dir string) error {
	if err := Ensure(ctx, dir, "both"); err != nil {
		return err
	}
	if _, err := EnsureTinyRemapper(ctx, dir); err != nil {
		return err
	}
	if _, err := EnsureSpecialSource(ctx, dir); err != nil {
		return err
	}
	return nil
}

var versionRe = regexp.MustCompile(`"?(\d+)(?:\.(\d+))?`)

// JavaVersion ejecuta `java -version` y devuelve el major (8, 11, 17, 21...).
// `javaBin` puede ser "java" o una ruta.
func JavaVersion(ctx context.Context, javaBin string) (int, string, error) {
	if javaBin == "" {
		javaBin = "java"
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, javaBin, "-version")
	out, err := cmd.CombinedOutput()
	raw := strings.TrimSpace(string(out))
	if err != nil {
		return 0, raw, fmt.Errorf("no se pudo ejecutar %q -version: %v\n%s", javaBin, err, raw)
	}
	// Formatos: openjdk version "21.0.3" ... / java version "1.8.0_402"
	m := versionRe.FindStringSubmatch(raw)
	if m == nil {
		return 0, raw, fmt.Errorf("no se pudo parsear la versión de java: %s", firstLine(raw))
	}
	if m[1] == "1" && m[2] != "" {
		v, _ := strconv.Atoi(m[2]) // 1.8 -> 8
		return v, firstLine(raw), nil
	}
	v, _ := strconv.Atoi(m[1])
	return v, firstLine(raw), nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// checkZip verifica la firma de un fichero zip (jars) descargado.
func checkZip(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var magic [4]byte
	if _, err := io.ReadFull(f, magic[:]); err != nil {
		return fmt.Errorf("no se pudo leer el fichero: %w", err)
	}
	if magic[0] != 'P' || magic[1] != 'K' {
		return fmt.Errorf("no es un zip/jar válido (firma %x), posible página de error", magic)
	}
	return nil
}

// CheckJava verifica el requisito según el motor y la versión de MC.
// Vineflower >= 1.11 exige Java 17+. MC >= 1.18 exige 17, MC >= 1.20.5 exige 21.
func CheckJava(javaBin string, engine string, mcMajor, mcMinor int) (int, error) {
	major, raw, err := JavaVersion(context.Background(), javaBin)
	if err != nil {
		return 0, err
	}
	engine = normalizeEngine(engine)
	need := 8
	if engine == "vine" || engine == "both" {
		need = 17
	}
	// Requisitos del cliente MC (bytecode objetivo, no del decompilador).
	if mcMajor > 1 || (mcMajor == 1 && mcMinor >= 20) {
		// 1.20.5+ / 1.21.x -> Java 21 para el juego; para descompilar basta 17,
		// pero se avisa. Vineflower con --jrt puede usar otro runtime.
	}
	if major < need {
		return major, fmt.Errorf("java %d detectado (%s): el motor %q necesita Java %d+; instala Temurin 17 o 21", major, raw, engine, need)
	}
	return major, nil
}
