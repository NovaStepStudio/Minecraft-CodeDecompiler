package mcp

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/NovaStepStudio/Minecraft-CodeDecompiler/internal/tools"
)

// RemapMCP remapea inJar (ofuscado legacy) con nombres MCP:
// prepara mcp.srg fusionado y aplica SpecialSource en una pasada.
// Salida: outJar con clases net/minecraft/... y miembros MCP
// (getMinecraft, thePlayer, theWorld...). No borra el original.
func RemapMCP(ctx context.Context, toolsDir, mc, channel string, refresh bool, inJar, outJar, javaBin string) error {
	if javaBin == "" {
		javaBin = "java"
	}
	srg, desc, err := Ensure(ctx, toolsDir, mc, channel, refresh)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "[..] fuente MCP: %s\n", desc)
	ss, err := tools.EnsureSpecialSource(ctx, toolsDir)
	if err != nil {
		return err
	}
	if _, err := os.Stat(inJar); err != nil {
		return fmt.Errorf("falta %s", inJar)
	}
	// Probe: el srg debe corresponder a ESTE jar (notch). Un donante con
	// deriva silenciosa mapea clases a nombres equivocados: abortar.
	rate, n, verr := AppliesTo(inJar, srg, 60)
	if verr != nil {
		return fmt.Errorf("verificando mapeo: %w", verr)
	}
	fmt.Fprintf(os.Stderr, "[..] probe notch: %d%% de %d miembros del srg existen en el jar\n",
		int(rate*100+0.5), n)
	if rate < 0.7 {
		return fmt.Errorf("el mapeo no corresponde a este jar (solo %d%% coincide): notch distinto, abortado sin tocar nada",
			int(rate*100+0.5))
	}
	args := []string{"-jar", ss, "-i", inJar, "-o", outJar, "-m", srg}
	fmt.Fprintf(os.Stderr, "[..] SpecialSource: %s -jar %s -i %s -o %s -m %s\n",
		javaBin, ss, inJar, outJar, srg)
	cmd := exec.CommandContext(ctx, javaBin, args...)
	out, err := cmd.CombinedOutput()
	fmt.Fprintf(os.Stderr, "%s\n", out)
	if err != nil {
		return fmt.Errorf("SpecialSource falló: %w", err)
	}
	fmt.Fprintf(os.Stderr, "[ok] remap MCP -> %s\n", outJar)
	return nil
}
