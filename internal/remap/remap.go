// Package remap convierte jars ofuscados de Minecraft a nombres oficiales
// Mojang usando tiny-remapper (FabricMC), que lee client_mappings.txt
// (ProGuard) DIRECTO, sin conversión previa.
//
// Namespaces de mapping-io: source = lado izquierdo del fichero (nombres
// oficiales Mojang), target = lado derecho (ofuscado). Por eso se invoca
// con from=target to=source: entrada ofuscada -> salida oficial.
package remap

import (
	"archive/zip"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/NovaStepStudio/Minecraft-CodeDecompiler/internal/tools"
)

// Remap ejecuta tiny-remapper: inJar (ofuscado) -> outJar (oficial).
func Remap(ctx context.Context, toolsDir, inJar, mappings, outJar, javaBin string, threads int) error {
	if javaBin == "" {
		javaBin = "java"
	}
	if threads <= 0 {
		threads = 4
	}
	tiny, err := tools.EnsureTinyRemapper(ctx, toolsDir)
	if err != nil {
		return err
	}
	for _, p := range []string{inJar, mappings} {
		if _, err := os.Stat(p); err != nil {
			return fmt.Errorf("falta %s", p)
		}
	}
	if err := os.MkdirAll(filepath.Dir(outJar), 0o755); err != nil {
		return err
	}
	args := []string{
		"-cp", tiny, "net.fabricmc.tinyremapper.Main",
		inJar, outJar, mappings, "target", "source",
		"--fixPackageAccess", "--threads=" + strconv.Itoa(threads),
	}
	fmt.Fprintf(os.Stderr, "[..] tiny-remapper: %s %s\n", javaBin, strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, javaBin, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("tiny-remapper falló: %w", err)
	}
	fmt.Fprintf(os.Stderr, "[ok] remap -> %s\n", outJar)
	return nil
}

// VerifyOfficial abre outJar y comprueba que contiene clases con nombres
// oficiales (net/minecraft/...) y ya NO las ofuscadas indicadas.
// Devuelve nº de clases oficiales encontradas.
func VerifyOfficial(jarPath string, mustHave, mustLack []string) (int, error) {
	zr, err := zip.OpenReader(jarPath)
	if err != nil {
		return 0, err
	}
	defer zr.Close()
	have := map[string]bool{}
	for _, f := range zr.File {
		have[f.Name] = true
	}
	for _, m := range mustLack {
		if have[m] {
			return 0, fmt.Errorf("el jar sigue ofuscado: aún contiene %s", m)
		}
	}
	found := 0
	var missing []string
	for _, m := range mustHave {
		if have[m] {
			found++
		} else {
			missing = append(missing, m)
		}
	}
	if len(missing) > 0 {
		return found, fmt.Errorf("faltan clases oficiales: %s", strings.Join(missing, ", "))
	}
	return found, nil
}

// CountOfficial cuenta clases bajo net/minecraft/ en el jar.
func CountOfficial(jarPath string) (int, error) {
	zr, err := zip.OpenReader(jarPath)
	if err != nil {
		return 0, err
	}
	defer zr.Close()
	n := 0
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "net/minecraft/") && strings.HasSuffix(f.Name, ".class") {
			n++
		}
	}
	return n, nil
}

// DetectObfuscation inspecciona el jar y dice si está ofuscado.
// Heurística por contenido (no por número de versión):
//   - 500+ clases bajo net/minecraft/ -> LIMPIO (26.1+ sin ofuscar,
//     o jar ya remapeado). Un marcador suelto NO basta: la 1.0 trae
//     net/minecraft/client/Minecraft sin ofuscar pero el resto
//     (a, b, ...) sí lo está.
//   - en otro caso, si hay clases en el paquete raíz -> OFUSCADO.
//
// Devuelve (obfuscated, officialCount, err).
func DetectObfuscation(jarPath, side string) (obfuscated bool, official int, err error) {
	zr, err := zip.OpenReader(jarPath)
	if err != nil {
		return true, 0, err
	}
	defer zr.Close()
	_ = side
	official = 0
	rootClasses := 0
	for _, f := range zr.File {
		if !strings.HasSuffix(f.Name, ".class") {
			continue
		}
		if strings.HasPrefix(f.Name, "net/minecraft/") {
			official++
		} else if !strings.Contains(f.Name, "/") {
			rootClasses++
		}
	}
	if official >= 500 {
		return false, official, nil
	}
	if rootClasses > 0 {
		return true, official, nil
	}
	return false, official, nil
}
