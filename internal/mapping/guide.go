package mapping

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// GuideMeta describe la fuente del mapeo para MANIFEST.txt y AGENT.md.
type GuideMeta struct {
	MCVersion string // ej. 1.8.9
	Side      string // client|server
	Source    string // ej. "MCP stable_22 (ModCoderPack)" u "oficial Mojang (ProGuard)"
	Jar       string // jar al que corresponde (client-remapped.jar)
}

// Target es un objetivo curado para el javaagent: etiqueta + candidatos
// legibles de clase y miembro (se resuelve el primero que exista).
type Target struct {
	Label   string
	Classes []string
	Members []string
}

// CuratedTargets cubre 1.0–modernas con candidatos por era.
func CuratedTargets() []Target {
	return []Target{
		{Label: "instancia-cliente",
			Classes: []string{"net.minecraft.client.Minecraft"},
			Members: []string{"getMinecraft", "getInstance"}},
		{Label: "menu-principal",
			Classes: []string{
				"net.minecraft.client.gui.GuiMainMenu",
				"net.minecraft.client.gui.screens.TitleScreen",
			}},
		{Label: "pantalla-base",
			Classes: []string{
				"net.minecraft.client.gui.GuiScreen",
				"net.minecraft.client.gui.screens.Screen",
			}},
		{Label: "boton",
			Classes: []string{
				"net.minecraft.client.gui.GuiButton",
				"net.minecraft.client.gui.components.Button",
			},
			Members: []string{"actionPerformed", "onPress"}},
		{Label: "abrir-pantalla",
			Classes: []string{"net.minecraft.client.Minecraft"},
			Members: []string{"displayGuiScreen", "setScreen"}},
		{Label: "jugador",
			Classes: []string{
				"net.minecraft.client.entity.EntityPlayerSP",
				"net.minecraft.client.player.LocalPlayer",
			}},
		{Label: "mundo-cliente",
			Classes: []string{
				"net.minecraft.client.multiplayer.WorldClient",
				"net.minecraft.client.multiplayer.ClientLevel",
			}},
		{Label: "ajustes",
			Classes: []string{
				"net.minecraft.client.settings.GameSettings",
				"net.minecraft.client.Options",
			}},
	}
}

// Resolved es un objetivo con sus nombres runtime (ofuscados) encontrados.
type Resolved struct {
	Label                         string
	ClassRead, ClassObf           string
	MemberRead, MemberObf         string
	MemberDescObf, MemberDescRead string
	FoundClass, FoundMember       bool
}

// ResolveTargets resuelve los curados contra el mapeo.
func (m *Mapping) ResolveTargets() []Resolved {
	byClass := map[string]*Class{}
	for i := range m.Classes {
		byClass[m.Classes[i].Read] = &m.Classes[i]
	}
	byOwner := map[string][]Method{}
	for i := range m.Methods {
		mt := m.Methods[i]
		byOwner[mt.OwnerRead] = append(byOwner[mt.OwnerRead], mt)
	}
	var out []Resolved
	for _, t := range CuratedTargets() {
		r := Resolved{Label: t.Label}
		for _, c := range t.Classes {
			if hit, ok := byClass[c]; ok {
				r.ClassRead, r.ClassObf = hit.Read, hit.Obf
				r.FoundClass = true
				break
			}
		}
		if r.FoundClass {
			for _, mem := range t.Members {
				for _, mt := range byOwner[r.ClassRead] {
					if mt.NameRead == mem {
						r.MemberRead, r.MemberObf = mt.NameRead, mt.NameObf
						r.MemberDescObf, r.MemberDescRead = mt.DescObf, mt.DescRead
						r.FoundMember = true
						break
					}
				}
				if r.FoundMember {
					break
				}
			}
		}
		out = append(out, r)
	}
	return out
}

// WriteManifest escribe MANIFEST.txt con formato, fuente y cobertura.
func (m *Mapping) WriteManifest(dir string, meta GuideMeta) error {
	var b strings.Builder
	fmt.Fprintf(&b, "mapeo %s %s (%s)\n", meta.MCVersion, meta.Side, meta.Source)
	fmt.Fprintf(&b, "generado: %s (UTC)\n", time.Now().UTC().Format("2006-01-02 15:04"))
	fmt.Fprintf(&b, "corresponde a: %s (NO al jar ofuscado)\n", meta.Jar)
	fmt.Fprintf(&b, "clases: %d  metodos: %d  fields: %d\n", len(m.Classes), len(m.Methods), len(m.Fields))
	fmt.Fprint(&b, `
REGLA DE ORO: en runtime (juego normal, sin remapear) todo va OFUSCADO:
el javaagent debe matchear por el lado IZQUIERDO. En el codigo
descompilado/remappeado se usan los nombres legibles (lado DERECHO).

CHUNKS:
  classes.txt  una clase por linea:  <obf-dot> <legible-dot>
  methods.txt  un metodo por linea:  <ownerObf>.<nombreObf><descObf> => <ownerLegible>.<nombreLegible><descLegible>
  fields.txt   un field por linea:   <ownerObf>.<nombreObf>:<tipoObf> => <ownerLegible>.<nombreLegible>:<tipoLegible>
  Descriptores y tipos en formato JVM (Lpaquete/Clase; ZBCDFIJSV, [ para arrays).
  Sobrecargas: se distinguen por descriptor. Inners usan $ en ambos lados.
  (Fuente SRG/MCP: fields sin tipo, marcado "?" - matchear por owner+nombre.)
`)
	return os.WriteFile(filepath.Join(dir, "MANIFEST.txt"), []byte(b.String()), 0o644)
}

// WriteAgentGuide escribe AGENT.md: guía para otra IA que programa el javaagent.
func (m *Mapping) WriteAgentGuide(dir string, meta GuideMeta) error {
	res := m.ResolveTargets()
	var b strings.Builder
	fmt.Fprintf(&b, `# Mapa de intervencion NovaCore-Agent - Minecraft %s (%s)

Fuente: %s. Cobertura: %d clases, %d metodos, %d fields.
Corresponde a: %s (remappeado). Generado %s UTC.

## Como leer este mapeo

- Runtime = juego lanzado normal (OFUSCADO): el agente ve el lado IZQUIERDO
  de classes/methods/fields.txt. El transformer debe comparar
  owner/nombre/descriptor OFUSCados.
- Codigo descompilado = lado DERECHO (legible, para programar la logica).
- Ejemplo real de methods.txt:
`,
		meta.MCVersion, meta.Side, meta.Source,
		len(m.Classes), len(m.Methods), len(m.Fields),
		meta.Jar, time.Now().UTC().Format("2006-01-02"))
	if len(m.Methods) > 0 {
		mt := m.Methods[0]
		fmt.Fprintf(&b, "  %s.%s%s => %s.%s%s\n", mt.OwnerObf, mt.NameObf, mt.DescObf, mt.OwnerRead, mt.NameRead, mt.DescRead)
	}
	fmt.Fprint(&b, `
## Objetivos curados (menu e inyeccion)

| objetivo | clase legible | clase runtime | miembro legible | miembro runtime |
|---|---|---|---|---|
`)
	for _, r := range res {
		mr, mo, dr := "-", "-", "-"
		if r.FoundClass {
			mr = r.ClassObf
		}
		if r.FoundMember {
			mo = r.MemberObf
			dr = r.MemberDescObf
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n", r.Label, orDash(r.ClassRead, r.FoundClass), mr, orDash(r.MemberRead, r.FoundMember), mo)
		_ = dr
	}
	fmt.Fprint(&b, `
Detalle runtime de miembros resueltos (owner/nombre/descriptor para ASM):

`)
	for _, r := range res {
		if !r.FoundMember {
			continue
		}
		fmt.Fprintf(&b, "- %s: owner=`%s` name=`%s` desc=`%s` (legible: %s.%s%s)\n",
			r.Label, slash(r.ClassObf), r.MemberObf, r.MemberDescObf,
			r.ClassRead, r.MemberRead, r.MemberDescRead)
	}
	fmt.Fprint(&b, `
## Receta minima del javaagent

1. premain con ClassFileTransformer; en transform(className, ...) className
   viene en forma slash OFUSCADA (ej. ave) - convertir el objetivo con
   classes.txt antes de comparar, nunca comparar strings legibles.
2. Al hallar la clase objetivo, visitar con ASM (ClassReader/ClassWriter o
   AdviceAdapter) y matchear MethodNode por nombre+descriptor runtime.
3. Para menus: interceptar el metodo abrir-pantalla o la clase menu-principal
   e inyectar al inicio/fin (dibujar overlay, anadir botones).
4. Cuidado: sobrecargas comparten nombre (usar descriptor), inners llevan $,
   y los parametros p_xxx no estan renombrados (solo MCP params.csv).

## Limites honestos

- Miembros sin nombre en la fuente quedan searge (func_/field_): marginales.
- Este mapeo es del CLIENTE; el servidor usa otro namespace.
- Si el juego corre ya remappeado, invertir los lados.
`)
	return os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte(b.String()), 0o644)
}

func orDash(s string, ok bool) string {
	if !ok || s == "" {
		return "-"
	}
	return s
}
