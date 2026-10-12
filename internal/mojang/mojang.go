// Package mojang accede a piston-meta / piston-data sin dependencias externas.
package mojang

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	// ManifestURL es el endpoint canónico. No construir URLs de versión a mano:
	// siempre leer VersionEntry.URL del manifest.
	ManifestURL       = "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json"
	ManifestURLLegacy = "https://launchermeta.mojang.com/mc/game/version_manifest_v2.json"

	userAgent = "minecraft-codedecompiler/1.0.0"
)

// Manifest es version_manifest_v2.json
type Manifest struct {
	Latest struct {
		Release  string `json:"release"`
		Snapshot string `json:"snapshot"`
	} `json:"latest"`
	Versions []VersionEntry `json:"versions"`
}

type VersionEntry struct {
	ID              string `json:"id"`
	Type            string `json:"type"` // release, snapshot, old_beta, old_alpha
	URL             string `json:"url"`
	Time            string `json:"time"`
	ReleaseTime     string `json:"releaseTime"`
	SHA1            string `json:"sha1"`
	ComplianceLevel int    `json:"complianceLevel"`
}

// Artifact es un fichero descargable con hash y tamaño.
type Artifact struct {
	SHA1 string `json:"sha1"`
	Size int64  `json:"size"`
	URL  string `json:"url"`
}

// AssetIndex referencia al índice de assets.
type AssetIndex struct {
	ID        string `json:"id"`
	SHA1      string `json:"sha1"`
	Size      int64  `json:"size"`
	TotalSize int64  `json:"totalSize"`
	URL       string `json:"url"`
}

// VersionDetail es el JSON de una versión (<id>.json).
// Los mappings son opcionales: no existen antes de 1.14.4 / 19w36a.
type VersionDetail struct {
	ID                     string `json:"id"`
	Type                   string `json:"type"`
	Time                   string `json:"time"`
	ReleaseTime            string `json:"releaseTime"`
	MainClass              string `json:"mainClass"`
	MinimumLauncherVersion int    `json:"minimumLauncherVersion"`
	Assets                 string `json:"assets"`
	ComplianceLevel        int    `json:"complianceLevel"`

	JavaVersion struct {
		Component    string `json:"component"`
		MajorVersion int    `json:"majorVersion"`
	} `json:"javaVersion"`

	AssetIndex *AssetIndex `json:"assetIndex"`

	Downloads struct {
		Client         *Artifact `json:"client"`
		Server         *Artifact `json:"server"`
		WindowsServer  *Artifact `json:"windows_server"`
		ClientMappings *Artifact `json:"client_mappings"`
		ServerMappings *Artifact `json:"server_mappings"`
	} `json:"downloads"`
}

func httpClient() *http.Client {
	return &http.Client{Timeout: 0}
}

func newRequest(ctx context.Context, url string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	return req, nil
}

// FetchManifest descarga y parsea el manifest. Reintenta con el mirror legacy.
func FetchManifest(ctx context.Context) (*Manifest, error) {
	var lastErr error
	for _, u := range []string{ManifestURL, ManifestURLLegacy} {
		m, err := fetchJSON[Manifest](ctx, u)
		if err == nil {
			return m, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("manifest: %w", lastErr)
}

// FetchVersionDetail descarga el JSON de detalle de una versión.
func FetchVersionDetail(ctx context.Context, url string) (*VersionDetail, error) {
	return fetchJSON[VersionDetail](ctx, url)
}

func fetchJSON[T any](ctx context.Context, url string) (*T, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := newRequest(ctx, url)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	var v T
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return nil, fmt.Errorf("decode %s: %w", url, err)
	}
	return &v, nil
}

// FindVersion busca una versión por id exacto en el manifest.
func FindVersion(m *Manifest, id string) (*VersionEntry, error) {
	for i := range m.Versions {
		if m.Versions[i].ID == id {
			return &m.Versions[i], nil
		}
	}
	return nil, fmt.Errorf("versión %q no encontrada en el manifest", id)
}

// HasMappings indica si hay mappings oficiales (solo >= 1.14.4 / 19w36a).
func (v *VersionDetail) HasClientMappings() bool { return v.Downloads.ClientMappings != nil }
func (v *VersionDetail) HasServerMappings() bool { return v.Downloads.ServerMappings != nil }

// DownloadArtifact descarga un artefacto a dest, verificando sha1 y tamaño.
// Si el fichero ya existe y el hash coincide, lo omite (resume manual).
// Informa progreso simple por stderr.
func DownloadArtifact(ctx context.Context, a *Artifact, dest string, label string) error {
	if a == nil {
		return fmt.Errorf("%s: artefacto no disponible para esta versión", label)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	if ok, _ := verifyFile(dest, a.SHA1, a.Size); ok {
		fmt.Fprintf(os.Stderr, "[ok] %s ya existe y coincide (%s)\n", label, dest)
		return nil
	}

	req, err := newRequest(ctx, a.URL)
	if err != nil {
		return err
	}
	resp, err := httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("descargar %s: %w", label, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("descargar %s: %s", label, resp.Status)
	}

	tmp := dest + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	h := sha1.New()
	var written int64
	buf := make([]byte, 256*1024)
	var lastPct int = -1
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				return werr
			}
			h.Write(buf[:n])
			written += int64(n)
			if a.Size > 0 {
				pct := int(written * 100 / a.Size)
				if pct != lastPct && pct%5 == 0 {
					fmt.Fprintf(os.Stderr, "\r[..] %s %d%% (%d/%d bytes)", label, pct, written, a.Size)
					lastPct = pct
				}
			} else {
				fmt.Fprintf(os.Stderr, "\r[..] %s %d bytes", label, written)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			return fmt.Errorf("descargar %s: %w", label, rerr)
		}
	}
	fmt.Fprint(os.Stderr, "\r")
	f.Close()

	if a.Size > 0 && written != a.Size {
		os.Remove(tmp)
		return fmt.Errorf("%s: tamaño inesperado: got %d want %d", label, written, a.Size)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if a.SHA1 != "" && got != a.SHA1 {
		os.Remove(tmp)
		return fmt.Errorf("%s: sha1 mismatch: got %s want %s", label, got, a.SHA1)
	}
	if err := os.Rename(tmp, dest); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "[ok] %s -> %s (%d bytes, sha1 %s)\n", label, dest, written, got[:12])
	return nil
}

func verifyFile(path, sha1hex string, size int64) (bool, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	if size > 0 && fi.Size() != size {
		return false, nil
	}
	if sha1hex == "" {
		return true, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, err
	}
	return hex.EncodeToString(h.Sum(nil)) == sha1hex, nil
}

// DownloadURL descarga una URL sin hash conocido (para version.json, asset index).
// Si expectedSHA1 != "" lo verifica.
func DownloadURL(ctx context.Context, url, dest, label, expectedSHA1 string, expectedSize int64) error {
	return DownloadArtifact(ctx, &Artifact{SHA1: expectedSHA1, Size: expectedSize, URL: url}, dest, label)
}
