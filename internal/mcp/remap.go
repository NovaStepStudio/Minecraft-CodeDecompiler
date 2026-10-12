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
func RemapMCP(ctx context.Context, toolsDir, mc, inJar, outJar, javaBin string) error {
	if javaBin == "" {
		javaBin = "java"
	}
	srg, err := Ensure(ctx, toolsDir, mc)
	if err != nil {
		return err
	}
	ss, err := tools.EnsureSpecialSource(ctx, toolsDir)
	if err != nil {
		return err
	}
	if _, err := os.Stat(inJar); err != nil {
		return fmt.Errorf("falta %s", inJar)
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
