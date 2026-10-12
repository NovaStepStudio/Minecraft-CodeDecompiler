// Package assets descarga los objetos de assets de Mojang y los deja
// UTILIZABLES: no solo hashes, sino árbol con nombres reales + assets
// extraídos del client.jar + unión de ambos.
//
// Layout dentro de versions/<id>/:
//
//	assets/indexes/<assetId>.json   índice (copia de asset_index.json)
//	assets/objects/<hh>/<hash>      objetos content-addressed (caché, launcher-compat)
//	assets/virtual/<assetId>/...    árbol con NOMBRES REALES (ej. minecraft/sounds/mob/pig/say1.ogg)
//	client-assets/assets/...        extraído de client.jar (texturas, modelos, blockstates...)
//	client-assets/data/...          datapacks vanilla del client.jar
//	merged-assets/...               unión virtual + client-assets (como mcasset.cloud)
package assets

import (
	"archive/zip"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const ObjectsBaseURL = "https://resources.download.minecraft.net"

// Index es el asset index JSON: {objects: {<ruta>: {hash, size}}}
type Index struct {
	Objects map[string]Object `json:"objects"`
}

type Object struct {
	Hash string `json:"hash"`
	Size int64  `json:"size"`
}

func ObjectURL(hash string) string {
	return ObjectsBaseURL + "/" + hash[:2] + "/" + hash
}

func ParseIndex(path string) (*Index, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	var idx Index
	if err := json.NewDecoder(f).Decode(&idx); err != nil {
		return nil, 0, fmt.Errorf("parse %s: %w", path, err)
	}
	var total int64
	for _, o := range idx.Objects {
		total += o.Size
	}
	return &idx, total, nil
}

// VersionAssetID lee versions/<id>/version.json (JSON crudo de Mojang)
// y devuelve assetIndex.id.
func VersionAssetID(versionJSONPath string) (string, error) {
	f, err := os.Open(versionJSONPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	var v struct {
		AssetIndex *struct {
			ID string `json:"id"`
		} `json:"assetIndex"`
		Assets string `json:"assets"`
	}
	if err := json.NewDecoder(f).Decode(&v); err != nil {
		return "", fmt.Errorf("parse %s: %w", versionJSONPath, err)
	}
	if v.AssetIndex != nil && v.AssetIndex.ID != "" {
		return v.AssetIndex.ID, nil
	}
	if v.Assets != "" {
		return v.Assets, nil
	}
	return "", fmt.Errorf("sin assetIndex en %s", versionJSONPath)
}

type DownloadStats struct {
	FilesTotal int
	FilesSkip  int64
	FilesGet   int64
	BytesTotal int64
	BytesGet   int64
	BytesSkip  int64
}

// DownloadObjects descarga los objetos del índice a objectsDir con workers
// en paralelo, reintentos y verificación sha1+size. Idempotente: omite lo
// que ya existe con tamaño correcto. Si limit > 0 solo los primeros N
// (orden alfabético, modo prueba). Devuelve estadísticas.
func DownloadObjects(ctx context.Context, idx *Index, objectsDir string, jobs, limit int) (DownloadStats, error) {
	keys := make([]string, 0, len(idx.Objects))
	for k := range idx.Objects {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if limit > 0 && limit < len(keys) {
		keys = keys[:limit]
	}
	if jobs <= 0 {
		jobs = 12
	}

	var st DownloadStats
	st.FilesTotal = len(keys)
	for _, k := range keys {
		st.BytesTotal += idx.Objects[k].Size
	}

	type job struct {
		path string
		obj  Object
	}
	ch := make(chan job)
	errCh := make(chan error, len(keys))
	var wg sync.WaitGroup
	var progressMu sync.Mutex
	lastPct := -1
	report := func() {
		got := atomic.LoadInt64(&st.BytesGet) + atomic.LoadInt64(&st.BytesSkip)
		pct := 0
		if st.BytesTotal > 0 {
			pct = int(got * 100 / st.BytesTotal)
		}
		progressMu.Lock()
		defer progressMu.Unlock()
		if pct != lastPct && (pct%5 == 0 || pct == 100) {
			fmt.Fprintf(os.Stderr, "\r[..] objetos %d%% (%d/%d ficheros, %d/%d bytes)",
				pct, atomic.LoadInt64(&st.FilesGet)+atomic.LoadInt64(&st.FilesSkip),
				st.FilesTotal, got, st.BytesTotal)
			lastPct = pct
		}
	}

	client := &http.Client{Timeout: 60 * time.Second}
	for w := 0; w < jobs; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range ch {
				select {
				case <-ctx.Done():
					return
				default:
				}
				dest := filepath.Join(objectsDir, j.obj.Hash[:2], j.obj.Hash)
				if fi, err := os.Stat(dest); err == nil && fi.Size() == j.obj.Size {
					atomic.AddInt64(&st.FilesSkip, 1)
					atomic.AddInt64(&st.BytesSkip, j.obj.Size)
					report()
					continue
				}
				if err := downloadOne(ctx, client, j.obj, dest); err != nil {
					errCh <- fmt.Errorf("%s: %w", j.path, err)
					return
				}
				atomic.AddInt64(&st.FilesGet, 1)
				atomic.AddInt64(&st.BytesGet, j.obj.Size)
				report()
			}
		}()
	}
	for _, k := range keys {
		select {
		case <-ctx.Done():
			close(ch)
			wg.Wait()
			return st, ctx.Err()
		case ch <- job{k, idx.Objects[k]}:
		}
	}
	close(ch)
	wg.Wait()
	close(errCh)
	fmt.Fprint(os.Stderr, "\r")
	for err := range errCh {
		if err != nil {
			return st, err
		}
	}
	fmt.Fprintf(os.Stderr, "[ok] objetos: %d descargados, %d ya existían (%d/%d bytes)\n",
		st.FilesGet, st.FilesSkip, st.BytesGet+st.BytesSkip, st.BytesTotal)
	return st, nil
}

func downloadOne(ctx context.Context, client *http.Client, obj Object, dest string) error {
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * time.Second):
			}
		}
		if err := tryDownload(ctx, client, obj, dest); err != nil {
			lastErr = err
			if isRetryable(err) {
				continue
			}
			return err
		}
		return nil
	}
	return fmt.Errorf("tras 4 intentos: %v", lastErr)
}

type retryableError struct{ status int }

func (e *retryableError) Error() string { return fmt.Sprintf("HTTP %d", e.status) }

func isRetryable(err error) bool {
	if _, ok := err.(*retryableError); ok {
		return true
	}
	// Errores de red: reintentar.
	return true
}

func tryDownload(ctx context.Context, client *http.Client, obj Object, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ObjectURL(obj.Hash), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "minecraft-codedecompiler/1.0.0")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return &retryableError{resp.StatusCode}
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s (no reintentable)", ObjectURL(obj.Hash), resp.Status)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
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
	if n != obj.Size {
		os.Remove(tmp)
		return fmt.Errorf("tamaño inesperado: got %d want %d", n, obj.Size)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != obj.Hash {
		os.Remove(tmp)
		return fmt.Errorf("sha1 mismatch: got %s want %s", got, obj.Hash)
	}
	return os.Rename(tmp, dest)
}

// Materialize copia cada objeto a su RUTA REAL bajo virtualDir:
// objects/<hh>/<hash> -> virtualDir/<ruta del index>.
// Mismo hash en varias rutas -> se copia a cada una.
func Materialize(idx *Index, objectsDir, virtualDir string, limit int) (int, error) {
	keys := make([]string, 0, len(idx.Objects))
	for k := range idx.Objects {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if limit > 0 && limit < len(keys) {
		keys = keys[:limit]
	}
	done := 0
	for _, p := range keys {
		o := idx.Objects[p]
		src := filepath.Join(objectsDir, o.Hash[:2], o.Hash)
		dst := filepath.Join(virtualDir, filepath.FromSlash(p))
		if _, err := os.Stat(dst); err == nil {
			done++
			continue
		}
		if err := copyFile(src, dst); err != nil {
			return done, fmt.Errorf("materializar %s: %w", p, err)
		}
		done++
	}
	fmt.Fprintf(os.Stderr, "[ok] virtual: %d ficheros con nombres reales -> %s\n", done, virtualDir)
	return done, nil
}

// ExtractClientAssets extrae assets/ y data/ del client.jar a destDir.
// Ahí viven texturas, modelos, blockstates, etc. que el índice NO trae.
func ExtractClientAssets(clientJar, destDir string) (int, error) {
	zr, err := zip.OpenReader(clientJar)
	if err != nil {
		return 0, err
	}
	defer zr.Close()
	n := 0
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if !strings.HasPrefix(f.Name, "assets/") && !strings.HasPrefix(f.Name, "data/") {
			continue
		}
		dst := filepath.Join(destDir, filepath.FromSlash(f.Name))
		if _, err := os.Stat(dst); err == nil {
			n++
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return n, err
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			rc.Close()
			return n, err
		}
		out, err := os.Create(dst)
		if err != nil {
			rc.Close()
			return n, err
		}
		_, err = io.Copy(out, rc)
		out.Close()
		rc.Close()
		if err != nil {
			return n, err
		}
		n++
	}
	fmt.Fprintf(os.Stderr, "[ok] client-assets: %d ficheros (assets/ + data/ de client.jar) -> %s\n", n, destDir)
	return n, nil
}

// Merge copia src1 y luego src2 en dst (src2 gana en conflictos).
func Merge(src1, src2, dst string) error {
	if err := copyTree(src1, dst); err != nil {
		return err
	}
	return copyTreeOverwrite(src2, dst)
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if _, err := os.Stat(target); err == nil {
			return nil // dst gana si ya existe (segunda pasada pisa igual)
		}
		return copyFile(path, target)
	})
}

// copyTreeOverwrite igual pero la 2ª pasada SÍ pisa (salta si mismo tamaño).
func copyTreeOverwrite(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if st, err := os.Stat(target); err == nil && st.Size() == info.Size() {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return copyFileNoMkdir(path, target)
	})
}

func copyFileNoMkdir(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	cerr := out.Close()
	if err != nil {
		return err
	}
	return cerr
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	out.Close()
	return err
}
