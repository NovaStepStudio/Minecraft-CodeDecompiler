// Package mapping genera el MAPEO legible en TXT (por chunks) que necesita
// un --javaagent externo (p.ej. NovaCore-Agent) para intervenir el juego:
//
//	mappings/classes.txt  <obf> <legible>
//	mappings/methods.txt  <obfOwner>.<obfNombre><descObf> => <legible...>
//	mappings/fields.txt   <obfOwner>.<obfNombre>:<tipoObf> => <legible...>
//	mappings/MANIFEST.txt formato, fuente, cobertura y correspondencia
//	mappings/AGENT.md     guía para otra IA: objetivos curados resueltos
//
// Regla de oro: en runtime (juego normal) todo va OFUSCADO (lado izquierdo);
// en el código descompilado/remappeado van los nombres legibles (derecha).
// Fuentes: client_mappings.txt oficial (ProGuard, 1.14.4-1.21.11) o
// mcp.srg fusionado (MCP legacy). Siempre corresponde a client-remapped.jar.
package mapping

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Class en forma dot (obf: ave — legible: net.minecraft.client.Minecraft).
type Class struct{ Obf, Read string }

// Method con descriptores JVM en ambos namespaces.
type Method struct {
	OwnerObf, NameObf, DescObf    string
	OwnerRead, NameRead, DescRead string
}

// Field con tipos JVM en ambos namespaces.
type Field struct {
	OwnerObf, NameObf, TypeObf    string
	OwnerRead, NameRead, TypeRead string
}

type Mapping struct {
	Classes []Class
	Methods []Method
	Fields  []Field
}

func slash(dot string) string { return strings.ReplaceAll(dot, ".", "/") }

// ---------------- SRG (notch->searge/MCP, descriptores ya JVM) ----------------

// ParseSRG lee joined.srg o mcp.srg fusionado (PK/CL/FD/MD).
func ParseSRG(path string) (*Mapping, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	m := &Mapping{}
	for i, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimRight(raw, "\r")
		if line == "" {
			continue
		}
		no := i + 1
		switch {
		case strings.HasPrefix(line, "PK: "):
			continue // renombrados de paquete, triviales
		case strings.HasPrefix(line, "CL: "):
			t := strings.Fields(line[4:])
			if len(t) != 2 {
				return nil, fmt.Errorf("srg %s:%d CL malformado", path, no)
			}
			m.Classes = append(m.Classes, Class{dot(t[0]), dot(t[1])})
		case strings.HasPrefix(line, "FD: "):
			t := strings.Fields(line[4:])
			if len(t) != 2 {
				return nil, fmt.Errorf("srg %s:%d FD malformado", path, no)
			}
			oo, on := splitMember(t[0])
			ro, rn := splitMember(t[1])
			m.Fields = append(m.Fields, Field{
				OwnerObf: dot(oo), NameObf: on, TypeObf: "?",
				OwnerRead: dot(ro), NameRead: rn, TypeRead: "?",
			})
		case strings.HasPrefix(line, "MD: "):
			t := strings.Fields(line[4:])
			if len(t) != 4 {
				return nil, fmt.Errorf("srg %s:%d MD malformado", path, no)
			}
			oo, on := splitMember(t[0])
			ro, rn := splitMember(t[2])
			m.Methods = append(m.Methods, Method{
				OwnerObf: dot(oo), NameObf: on, DescObf: t[1],
				OwnerRead: dot(ro), NameRead: rn, DescRead: t[3],
			})
		default:
			return nil, fmt.Errorf("srg %s:%d línea desconocida: %q", path, no, line)
		}
	}
	m.sort()
	return m, nil
}

// Nota: SRG no trae el TIPO del field (solo owner/nombre); el TXT marca "?"
// porque el agente debe matchear fields por owner+nombre (los descriptores
// de fields no son necesarios para transformar con ASM en la práctica).

func dot(slash string) string { return strings.ReplaceAll(slash, "/", ".") }

func splitMember(ref string) (owner, name string) {
	if i := strings.LastIndex(ref, "/"); i >= 0 {
		return ref[:i], ref[i+1:]
	}
	return "", ref
}

// ---------------- ProGuard oficial (oficial->obf, firmas Java) ----------------

var prim = map[string]string{
	"void": "V", "boolean": "Z", "byte": "B", "char": "C",
	"short": "S", "int": "I", "float": "F", "long": "J", "double": "D",
}

// jvmType convierte un tipo Java ("int", "java.lang.String", "Foo[]") a JVM.
// cls mapea oficial-dot -> obf-dot; con nil no remapea (lado legible).
func jvmType(t string, cls map[string]string) string {
	depth := 0
	for strings.HasSuffix(t, "[]") {
		t = t[:len(t)-2]
		depth++
	}
	var base string
	if v, ok := prim[t]; ok {
		base = v
	} else {
		name := t
		if cls != nil {
			if o, ok := cls[t]; ok {
				name = o
			}
		}
		base = "L" + slash(name) + ";"
	}
	return strings.Repeat("[", depth) + base
}

// ParseProGuard lee client_mappings.txt oficial (oficial -> ofuscado) y lo
// invierte al modelo obf -> legible con descriptores JVM en ambos lados.
func ParseProGuard(path string) (*Mapping, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	// Pasada 1: clases oficial -> obf.
	cls := map[string]string{}
	type rawClass struct{ read, obf string }
	var order []rawClass
	for i, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimRight(raw, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			continue // miembros en pasada 2
		}
		if !strings.HasSuffix(line, ":") || !strings.Contains(line, " -> ") {
			return nil, fmt.Errorf("proguard %s:%d clase malformada: %q", path, i+1, line)
		}
		parts := strings.SplitN(strings.TrimSuffix(line, ":"), " -> ", 2)
		cls[parts[0]] = parts[1]
		order = append(order, rawClass{parts[0], parts[1]})
	}
	m := &Mapping{}
	for _, c := range order {
		m.Classes = append(m.Classes, Class{Obf: c.obf, Read: c.read})
	}
	// Pasada 2: miembros (con contexto de clase).
	var curRead, curObf string
	for i, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimRight(raw, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			parts := strings.SplitN(strings.TrimSuffix(line, ":"), " -> ", 2)
			curRead, curObf = parts[0], parts[1]
			continue
		}
		line = strings.TrimSpace(line)
		// Quitar rangos de línea "12:13:" de métodos.
		if j := strings.Index(line, ":"); j > 0 && !strings.Contains(line[:j], " ") && !strings.Contains(line[:j], "(") {
			rest := strings.TrimSpace(line[j+1:])
			if strings.Contains(rest, " -> ") {
				line = rest
			}
		}
		parts := strings.SplitN(line, " -> ", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("proguard %s:%d miembro malformado: %q", path, i+1, line)
		}
		left, obfName := parts[0], parts[1]
		if k := strings.Index(left, "("); k >= 0 {
			// Método: "Ret nombre(Arg1,Arg2)".
			head, argStr := left[:k], left[k+1:]
			argStr = strings.TrimSuffix(argStr, ")")
			hf := strings.Fields(head)
			if len(hf) != 2 {
				return nil, fmt.Errorf("proguard %s:%d método malformado: %q", path, i+1, line)
			}
			ret, name := hf[0], hf[1]
			var argsObf, argsRead []string
			if strings.TrimSpace(argStr) != "" {
				for _, a := range strings.Split(argStr, ",") {
					a = strings.TrimSpace(a)
					argsObf = append(argsObf, jvmType(a, cls))
					argsRead = append(argsRead, jvmType(a, nil))
				}
			}
			m.Methods = append(m.Methods, Method{
				OwnerObf: curObf, NameObf: obfName,
				DescObf:   "(" + strings.Join(argsObf, "") + ")" + jvmType(ret, cls),
				OwnerRead: curRead, NameRead: name,
				DescRead: "(" + strings.Join(argsRead, "") + ")" + jvmType(ret, nil),
			})
		} else {
			// Field: "Tipo nombre".
			ff := strings.Fields(left)
			if len(ff) != 2 {
				return nil, fmt.Errorf("proguard %s:%d field malformado: %q", path, i+1, line)
			}
			m.Fields = append(m.Fields, Field{
				OwnerObf: curObf, NameObf: obfName, TypeObf: jvmType(ff[0], cls),
				OwnerRead: curRead, NameRead: ff[1], TypeRead: jvmType(ff[0], nil),
			})
		}
	}
	m.sort()
	return m, nil
}

func (m *Mapping) sort() {
	sort.Slice(m.Classes, func(i, j int) bool { return m.Classes[i].Obf < m.Classes[j].Obf })
	sort.Slice(m.Methods, func(i, j int) bool {
		a, b := m.Methods[i], m.Methods[j]
		if a.OwnerObf != b.OwnerObf {
			return a.OwnerObf < b.OwnerObf
		}
		if a.NameObf != b.NameObf {
			return a.NameObf < b.NameObf
		}
		return a.DescObf < b.DescObf
	})
	sort.Slice(m.Fields, func(i, j int) bool {
		a, b := m.Fields[i], m.Fields[j]
		if a.OwnerObf != b.OwnerObf {
			return a.OwnerObf < b.OwnerObf
		}
		return a.NameObf < b.NameObf
	})
}

// ---------------- Emisores TXT ----------------

// WriteTXT escribe classes/methods/fields.txt deterministas en dir.
func (m *Mapping) WriteTXT(dir, header string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n# formato: <ofuscado> <legible> (obf = runtime, legible = fuentes)\n# total: %d\n", header, len(m.Classes))
	for _, c := range m.Classes {
		fmt.Fprintf(&b, "%s %s\n", c.Obf, c.Read)
	}
	if err := os.WriteFile(filepath.Join(dir, "classes.txt"), []byte(b.String()), 0o644); err != nil {
		return err
	}
	b.Reset()
	fmt.Fprintf(&b, "# %s\n# formato: <ownerObf>.<nombreObf><descObf> => <ownerLegible>.<nombreLegible><descLegible>\n# descriptores JVM; sobrecargas se distinguen por descriptor\n# total: %d\n", header, len(m.Methods))
	for _, mt := range m.Methods {
		fmt.Fprintf(&b, "%s.%s%s => %s.%s%s\n",
			mt.OwnerObf, mt.NameObf, mt.DescObf, mt.OwnerRead, mt.NameRead, mt.DescRead)
	}
	if err := os.WriteFile(filepath.Join(dir, "methods.txt"), []byte(b.String()), 0o644); err != nil {
		return err
	}
	b.Reset()
	fmt.Fprintf(&b, "# %s\n# formato: <ownerObf>.<nombreObf>:<tipoObf> => <ownerLegible>.<nombreLegible>:<tipoLegible>\n# (fuente SRG/MCP: tipo = ? , matchear por owner+nombre)\n# total: %d\n", header, len(m.Fields))
	for _, f := range m.Fields {
		fmt.Fprintf(&b, "%s.%s:%s => %s.%s:%s\n",
			f.OwnerObf, f.NameObf, f.TypeObf, f.OwnerRead, f.NameRead, f.TypeRead)
	}
	return os.WriteFile(filepath.Join(dir, "fields.txt"), []byte(b.String()), 0o644)
}
