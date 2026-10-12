// Package decompile orquesta los motores CFR y Vineflower vía `java -jar`.
// No los reimplementa: exige los jars (ver internal/tools) y Java instalado.
package decompile

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"
)

type Options struct {
	JavaBin string // "java" por defecto
	Engine  string // "cfr" | "vine" | "both"
	Threads int    // hilos para Vineflower (0 = NumCPU)
	Timeout time.Duration
}

// Run ejecuta uno o ambos motores en secuencia y devuelve las salidas.
// inputJar debe existir. Crea outBase/src-cfr y/o outBase/src-vine.
func Run(ctx context.Context, toolsDir, inputJar, outBase string, opt Options) ([]string, error) {
	if opt.JavaBin == "" {
		opt.JavaBin = "java"
	}
	if opt.Threads <= 0 {
		opt.Threads = runtime.NumCPU()
	}
	if opt.Timeout <= 0 {
		opt.Timeout = 0 // sin límite global; cada motor hereda ctx
	}
	if _, err := os.Stat(inputJar); err != nil {
		return nil, fmt.Errorf("jar de entrada no encontrado: %s", inputJar)
	}
	if err := os.MkdirAll(outBase, 0o755); err != nil {
		return nil, err
	}

	var outs []string
	switch opt.Engine {
	case "cfr":
		out := filepath.Join(outBase, "src-cfr")
		if err := runCFR(ctx, opt, toolsDir, inputJar, out); err != nil {
			return outs, err
		}
		outs = append(outs, out)
	case "vine", "vineflower":
		out := filepath.Join(outBase, "src-vine")
		if err := runVineflower(ctx, opt, toolsDir, inputJar, out); err != nil {
			return outs, err
		}
		outs = append(outs, out)
	default: // both
		outCFR := filepath.Join(outBase, "src-cfr")
		if err := runCFR(ctx, opt, toolsDir, inputJar, outCFR); err != nil {
			return outs, fmt.Errorf("CFR: %w", err)
		}
		outs = append(outs, outCFR)
		outVine := filepath.Join(outBase, "src-vine")
		if err := runVineflower(ctx, opt, toolsDir, inputJar, outVine); err != nil {
			return outs, fmt.Errorf("Vineflower: %w", err)
		}
		outs = append(outs, outVine)
	}
	return outs, nil
}

func cfrJar(toolsDir string) string        { return filepath.Join(toolsDir, "cfr-0.152.jar") }
func vineflowerJar(toolsDir string) string { return filepath.Join(toolsDir, "vineflower-1.12.0.jar") }

func runCFR(ctx context.Context, opt Options, toolsDir, inputJar, outDir string) error {
	jar := cfrJar(toolsDir)
	if _, err := os.Stat(jar); err != nil {
		return fmt.Errorf("falta %s; ejecuta: minecraft-codedecompiler tools install", jar)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	// CFR: java -jar cfr.jar input.jar --outputdir out
	args := []string{"-jar", jar, inputJar, "--outputdir", outDir}
	fmt.Fprintf(os.Stderr, "[..] CFR: %s %s\n", opt.JavaBin, quoteArgs(args))
	cmd := exec.CommandContext(ctx, opt.JavaBin, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("CFR falló: %w", err)
	}
	fmt.Fprintf(os.Stderr, "[ok] CFR -> %s\n", outDir)
	return nil
}

func runVineflower(ctx context.Context, opt Options, toolsDir, inputJar, outDir string) error {
	jar := vineflowerJar(toolsDir)
	if _, err := os.Stat(jar); err != nil {
		return fmt.Errorf("falta %s; ejecuta: minecraft-codedecompiler tools install", jar)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	// Vineflower 1.12.0: opciones PRIMERO con `=`, luego fuente(s) y destino.
	// `Usage: java -jar vineflower.jar --<option>=<value>... <source>... <destination>`
	// Sin `--folder` (se autodetecta por el nombre del destino).
	// NO usar `--folder` suelto ni valores con espacio: el parser se traga
	// el siguiente posicional (src-vine quedaba vacío y nacían carpetas "2").
	args := []string{"-jar", jar, "--thread-count=" + strconv.Itoa(opt.Threads), inputJar, outDir}
	fmt.Fprintf(os.Stderr, "[..] Vineflower: %s %s\n", opt.JavaBin, quoteArgs(args))
	cmd := exec.CommandContext(ctx, opt.JavaBin, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("Vineflower falló: %w", err)
	}
	fmt.Fprintf(os.Stderr, "[ok] Vineflower -> %s\n", outDir)
	return nil
}

func quoteArgs(args []string) string {
	out := ""
	for i, a := range args {
		if i > 0 {
			out += " "
		}
		if a == "" {
			out += `""`
		} else {
			out += a
		}
	}
	return out
}
