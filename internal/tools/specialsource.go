package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

const (
	SpecialSourceVersion  = "1.11.0"
	SpecialSourceFileName = "SpecialSource-1.11.0-shaded.jar"
)

// SpecialSource lee .srg legacy nativo: la herramienta de la era MCP.
var SpecialSourceURLs = []string{
	"https://repo1.maven.org/maven2/net/md-5/SpecialSource/1.11.0/SpecialSource-1.11.0-shaded.jar",
	"https://repo.maven.apache.org/maven2/net/md-5/SpecialSource/1.11.0/SpecialSource-1.11.0-shaded.jar",
}

func SpecialSourcePath(dir string) string { return filepath.Join(dir, SpecialSourceFileName) }

// EnsureSpecialSource descarga el jar shaded (~1.8MB, sin dependencias).
func EnsureSpecialSource(ctx context.Context, dir string) (string, error) {
	dest := SpecialSourcePath(dir)
	if st, err := os.Stat(dest); err == nil && st.Size() > 1024*1024 {
		fmt.Fprintf(os.Stderr, "[ok] SpecialSource ya existe (%s, %d bytes)\n", dest, st.Size())
		return dest, nil
	}
	var lastErr error
	for _, u := range SpecialSourceURLs {
		fmt.Fprintf(os.Stderr, "[..] descargando SpecialSource desde %s\n", u)
		if err := FetchURL(ctx, u, dest, "SpecialSource "+SpecialSourceVersion, 1024*1024); err != nil {
			fmt.Fprintf(os.Stderr, "[!!] fallo: %v\n", err)
			lastErr = err
			continue
		}
		return dest, nil
	}
	return "", fmt.Errorf("no se pudo descargar SpecialSource: %v", lastErr)
}
