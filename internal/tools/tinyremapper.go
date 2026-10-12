package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	TinyRemapperVersion  = "0.15.0"
	TinyRemapperFileName = "tiny-remapper-0.15.0-fat.jar"
)

var TinyRemapperURLs = []string{
	"https://maven.fabricmc.net/net/fabricmc/tiny-remapper/0.15.0/tiny-remapper-0.15.0-fat.jar",
	"https://maven.fabricmc.net/net/fabricmc/tiny-remapper/0.12.2/tiny-remapper-0.12.2-fat.jar",
}

// TinyRemapperPath devuelve la ruta del fat jar (el primero que exista,
// buscando la versión pineada y luego el fallback).
func TinyRemapperPath(dir string) string { return filepath.Join(dir, TinyRemapperFileName) }

// EnsureTinyRemapper descarga tiny-remapper (fat jar, sin dependencias).
func EnsureTinyRemapper(ctx context.Context, dir string) (string, error) {
	dest := TinyRemapperPath(dir)
	if st, err := os.Stat(dest); err == nil && st.Size() > 500*1024 {
		fmt.Fprintf(os.Stderr, "[ok] tiny-remapper ya existe (%s, %d bytes)\n", dest, st.Size())
		return dest, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	var lastErr error
	for _, u := range TinyRemapperURLs {
		fmt.Fprintf(os.Stderr, "[..] descargando tiny-remapper desde %s\n", u)
		if err := download(ctx, u, dest); err != nil {
			fmt.Fprintf(os.Stderr, "[!!] fallo %s: %v\n", u, err)
			lastErr = err
			continue
		}
		fmt.Fprintf(os.Stderr, "[ok] tiny-remapper -> %s\n", dest)
		return dest, nil
	}
	return "", fmt.Errorf("no se pudo descargar tiny-remapper: %v", lastErr)
}
