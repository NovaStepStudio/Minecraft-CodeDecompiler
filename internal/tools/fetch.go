package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// FetchURL descarga url a dest exigiendo al menos minSize bytes y firma zip.
// No borra un dest válido existente.
func FetchURL(ctx context.Context, url, dest, label string, minSize int64) error {
	if st, err := os.Stat(dest); err == nil && st.Size() >= minSize {
		fmt.Fprintf(os.Stderr, "[ok] %s ya existe (%s, %d bytes)\n", label, dest, st.Size())
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "minecraft-codedecompiler/1.0.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	tmp := dest + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, resp.Body)
	f.Close()
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if n < minSize {
		os.Remove(tmp)
		return fmt.Errorf("%s: solo %d bytes (mínimo %d), posible error", label, n, minSize)
	}
	if err := checkZip(tmp); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("%s: %w", label, err)
	}
	if err := os.Rename(tmp, dest); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "[ok] %s -> %s (%d bytes)\n", label, dest, n)
	return nil
}
