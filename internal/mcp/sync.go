package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/NovaStepStudio/Minecraft-CodeDecompiler/internal/tools"
)

// SyncStats resume de mcp sync.
type SyncStats struct {
	Versions int
	StableOK int
	SnapOK   int
	SRGOK    int
	TSRGOK   int
	Skipped  int // con mappings oficiales (no necesitan MCP)
	Failed   int
}

// SyncAll pre-descarga TODO lo mapeable para operar 100% en local después:
// último estable + último snapshot por versión legacy, srg legacy y tsrgs.
// Idempotente: omite lo cacheado. Tras el sync, remap/map no tocan la red
// (salvo el refresco de 24h del índice, que usa ETag/304).
func SyncAll(ctx context.Context, toolsDir string, jobs int) (SyncStats, error) {
	var st SyncStats
	if jobs <= 0 {
		jobs = 10
	}
	vmap, err := loadVersions(ctx, toolsDir, true)
	if err != nil {
		return st, err
	}
	haveTSRG := fetchMCPConfigIndex(ctx, toolsDir)

	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, jobs)
	do := func(label string, fn func() error, okCount *int) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if err := fn(); err != nil {
				mu.Lock()
				st.Failed++
				mu.Unlock()
				fmt.Fprintf(os.Stderr, "[..] sync %s: omitido (%v)\n", label, err)
				return
			}
			mu.Lock()
			*okCount++
			mu.Unlock()
		}()
	}

	for mc := range vmap {
		if skipOfficial(mc) {
			mu.Lock()
			st.Skipped++
			mu.Unlock()
			continue
		}
		mu.Lock()
		st.Versions++
		mu.Unlock()
		mc := mc
		cdir := func(kind string, num int) string {
			return filepath.Join(toolsDir, "mcp", mc, fmt.Sprintf("%s-%d", kind, num))
		}
		// Estable + snapshot (últimos).
		if best := bestStableKind(vmap[mc], "stable"); best > 0 {
			kind, num := "stable", best
			do(fmt.Sprintf("%s %s_%d", mc, kind, num), func() error {
				return syncChannelZip(ctx, cdir(kind, num), mc, kind, num)
			}, &st.StableOK)
		}
		if best := bestStableKind(vmap[mc], "snapshot"); best > 0 {
			kind, num := "snapshot", best
			do(fmt.Sprintf("%s %s_%d", mc, kind, num), func() error {
				return syncChannelZip(ctx, cdir(kind, num), mc, kind, num)
			}, &st.SnapOK)
		}
		// srg legacy (intento silencioso: muchas versiones no tienen).
		do(fmt.Sprintf("%s srg", mc), func() error {
			sdir := filepath.Join(toolsDir, "mcp", mc, "shared")
			if err := os.MkdirAll(sdir, 0o755); err != nil {
				return err
			}
			zip := filepath.Join(sdir, fmt.Sprintf("mcp-%s-srg.zip", mc))
			url := fmt.Sprintf("%s/mcp/%s/mcp-%s-srg.zip", ArchiveBase, mc, mc)
			if err := tools.FetchURL(ctx, url, zip, "mcp srg "+mc, 50*1024); err != nil {
				return err
			}
			_, err := extractMember(zip, "joined.srg", filepath.Join(sdir, "joined.srg"))
			return err
		}, &st.SRGOK)
		// tsrg MCPConfig (solo si el índice lo lista).
		if haveTSRG[mc] {
			do(fmt.Sprintf("%s tsrg", mc), func() error {
				sdir := filepath.Join(toolsDir, "mcp", mc, "shared")
				if err := os.MkdirAll(sdir, 0o755); err != nil {
					return err
				}
				dest := filepath.Join(sdir, "joined.tsrg")
				if fi, err := os.Stat(dest); err == nil && fi.Size() >= 100*1024 {
					return nil
				}
				url := fmt.Sprintf("%s/versions/release/%s/joined.tsrg", MCPConfigBase, mc)
				tmp := dest + ".tmp"
				if err := downloadText(ctx, url, tmp); err != nil {
					os.Remove(tmp)
					return err
				}
				if fi, err := os.Stat(tmp); err != nil || fi.Size() < 100*1024 {
					os.Remove(tmp)
					return fmt.Errorf("tsrg inválido")
				}
				os.Remove(dest)
				return os.Rename(tmp, dest)
			}, &st.TSRGOK)
		}
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	fmt.Fprintf(os.Stderr, "[ok] sync: %d versiones (%d estables, %d snapshots, %d srg, %d tsrg, %d con oficiales, %d fallos)\n",
		st.Versions, st.StableOK, st.SnapOK, st.SRGOK, st.TSRGOK, st.Skipped, st.Failed)
	return st, nil
}

// syncChannelZip descarga el zip del canal y extrae methods/fields.csv.
func syncChannelZip(ctx context.Context, cdir, mc, kind string, num int) error {
	if err := os.MkdirAll(cdir, 0o755); err != nil {
		return err
	}
	base := fmt.Sprintf("mcp_%s-%d-%s.zip", kind, num, mc)
	urls := []string{
		fmt.Sprintf("%s/mcp_%s/%d-%s/%s", ArchiveBase, kind, num, mc, base),
		fmt.Sprintf("https://maven.minecraftforge.net/de/oceanlabs/mcp/mcp_%s/%d-%s/%s", kind, num, mc, base),
		fmt.Sprintf("https://maven.neoforged.net/releases/de/oceanlabs/mcp/mcp_%s/%d-%s/%s", kind, num, mc, base),
	}
	dest := filepath.Join(cdir, base)
	var lastErr error
	for _, u := range urls {
		if err := tools.FetchURL(ctx, u, dest, "mcp "+kind, 50*1024); err == nil {
			lastErr = nil
			break
		} else {
			lastErr = err
		}
	}
	if lastErr != nil {
		return lastErr
	}
	if _, err := extractMember(dest, "methods.csv", filepath.Join(cdir, "methods.csv")); err != nil {
		return err
	}
	_, err := extractMember(dest, "fields.csv", filepath.Join(cdir, "fields.csv"))
	return err
}

// skipOfficial dice si la versión ya tiene mappings oficiales Mojang
// (>=1.14.4) y por tanto no necesita MCP. Lo no parseable se incluye.
func skipOfficial(mc string) bool {
	parts := strings.Split(strings.SplitN(mc, "-", 2)[0], ".")
	if len(parts) < 2 || parts[0] != "1" {
		return false
	}
	maj, err1 := strconv.Atoi(parts[1])
	if err1 != nil {
		return false
	}
	if maj > 14 {
		return true
	}
	if maj < 14 || len(parts) < 3 {
		return false
	}
	min, err2 := strconv.Atoi(parts[2])
	return err2 == nil && min >= 4
}

// fetchMCPConfigIndex trae (1 sola petición API) la lista de versiones con
// tsrg en MCPConfig y la cachea. Si falla, usa caché o vacío (el probe en
// caliente sigue funcionando).
func fetchMCPConfigIndex(ctx context.Context, toolsDir string) map[string]bool {
	out := map[string]bool{}
	idxPath := filepath.Join(toolsDir, "mcp", "mcpconfig-index.json")
	if raw, err := os.ReadFile(idxPath); err == nil {
		var names []string
		if jerr := json.Unmarshal(raw, &names); jerr == nil {
			for _, n := range names {
				out[n] = true
			}
			fmt.Fprintf(os.Stderr, "[ok] índice MCPConfig en caché (%d versiones)\n", len(names))
			return out
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.github.com/repos/MinecraftForge/MCPConfig/contents/versions/release", nil)
	if err != nil {
		return out
	}
	req.Header.Set("User-Agent", "minecraft-codedecompiler/1.0.0")
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[!!] índice MCPConfig: %v (sigo sin él)\n", err)
		return out
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "[!!] índice MCPConfig: %s (sigo sin él)\n", resp.Status)
		return out
	}
	var entries []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return out
	}
	var names []string
	for _, e := range entries {
		if e.Type == "dir" {
			out[e.Name] = true
			names = append(names, e.Name)
		}
	}
	_ = os.MkdirAll(filepath.Join(toolsDir, "mcp"), 0o755)
	raw, _ := json.Marshal(names)
	_ = os.WriteFile(idxPath, raw, 0o644)
	fmt.Fprintf(os.Stderr, "[ok] índice MCPConfig: %d versiones con tsrg\n", len(names))
	return out
}
