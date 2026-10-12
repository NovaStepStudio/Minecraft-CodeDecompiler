// Package mcp — verificación de aplicabilidad del mapeo al jar objetivo.
//
// Un srg puede ser estructuralmente válido pero corresponder a OTRO jar
// (p.ej. srg donante 1.12 aplicado a 1.12.2: los notch globales derivaron).
// SpecialSource lo aplicaría igual y generaría clases MAL nombradas en
// silencio. AppliesTo lo evita: muestrea triples (owner,nombre,descriptor)
// del mcp.srg fusionado y comprueba que existen en el jar (parser .class
// mínimo, sin dependencias). Exige >=70% o aborta el remap.
package mcp

import (
	"archive/zip"
	"encoding/binary"
	"fmt"
	"os"
	"strings"
)

// AppliesTo devuelve la fracción de triples MD muestreados del srg que
// existen tal cual en el jar. sample = nº de triples a comprobar.
func AppliesTo(jarPath, fusedSRG string, sample int) (float64, int, error) {
	triples, err := sampleTriples(fusedSRG, sample)
	if err != nil {
		return 0, 0, err
	}
	if len(triples) == 0 {
		return 0, 0, fmt.Errorf("sin métodos en %s", fusedSRG)
	}
	zr, err := zip.OpenReader(jarPath)
	if err != nil {
		return 0, 0, err
	}
	defer zr.Close()
	byName := map[string]*zip.File{}
	for _, f := range zr.File {
		byName[f.Name] = f
	}
	parsed := map[string]map[string]bool{} // owner -> set(name\x00desc)
	getMembers := func(owner string) (map[string]bool, error) {
		if m, ok := parsed[owner]; ok {
			return m, nil
		}
		f := byName[owner+".class"]
		if f == nil {
			parsed[owner] = nil
			return nil, nil
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		m, err := classMembers(rc)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", owner, err)
		}
		parsed[owner] = m
		return m, nil
	}
	matched := 0
	for _, t := range triples {
		m, err := getMembers(t.owner)
		if err != nil {
			return 0, 0, err
		}
		if m != nil && m[t.name+"\x00"+t.desc] {
			matched++
		}
	}
	return float64(matched) / float64(len(triples)), len(triples), nil
}

type triple struct{ owner, name, desc string }

// sampleTriples toma `sample` triples MD equidistantes y deterministas.
func sampleTriples(fusedSRG string, sample int) ([]triple, error) {
	data, err := os.ReadFile(fusedSRG)
	if err != nil {
		return nil, err
	}
	var all []triple
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimRight(raw, "\r")
		if !strings.HasPrefix(line, "MD: ") {
			continue
		}
		t := strings.Fields(line[4:])
		if len(t) != 4 {
			continue
		}
		oo, on := splitMember(t[0])
		all = append(all, triple{oo, on, t[1]})
	}
	if len(all) == 0 {
		return nil, fmt.Errorf("sin líneas MD en %s", fusedSRG)
	}
	if sample <= 0 || sample > len(all) {
		sample = len(all)
	}
	step := len(all) / sample
	if step < 1 {
		step = 1
	}
	var out []triple
	for i := 0; i < len(all) && len(out) < sample; i += step {
		out = append(out, all[i])
	}
	return out, nil
}

// classMembers lista (nombre, descriptor) de fields+methods de un .class.
func classMembers(r interface {
	Read([]byte) (int, error)
}) (map[string]bool, error) {
	data, err := readAll(r)
	if err != nil {
		return nil, err
	}
	p := &parser{b: data}
	if p.u32() != 0xCAFEBABE {
		return nil, fmt.Errorf("magic inválido")
	}
	p.u16()
	p.u16() // minor, major
	n := int(p.u16())
	utf8 := map[int]string{}
	for i := 1; i < n; i++ {
		tag := p.u8()
		switch tag {
		case 1:
			l := int(p.u16())
			utf8[i] = string(p.bytes(l))
		case 3, 4:
			p.skip(4)
		case 5, 6:
			p.skip(8)
			i++ // long/double ocupan dos entradas
		case 7, 8, 16, 19, 20:
			p.skip(2)
		case 9, 10, 11, 12, 17, 18:
			p.skip(4)
		case 15:
			p.skip(3)
		default:
			return nil, fmt.Errorf("constant pool tag %d", tag)
		}
	}
	if p.err != nil {
		return nil, p.err
	}
	p.skip(6) // access, this, super
	nif := int(p.u16())
	for k := 0; k < nif; k++ {
		p.skip(2)
	} // interfaces
	members := map[string]bool{}
	for _, isMethod := range []bool{false, true} {
		_ = isMethod
		count := int(p.u16())
		for k := 0; k < count; k++ {
			p.skip(2) // access
			name := utf8[int(p.u16())]
			desc := utf8[int(p.u16())]
			members[name+"\x00"+desc] = true
			na := int(p.u16())
			for a := 0; a < na; a++ {
				p.skip(2)
				p.skip(int(p.u32()))
			}
		}
	}
	return members, p.err
}

func readAll(r interface {
	Read([]byte) (int, error)
}) ([]byte, error) {
	var out []byte
	buf := make([]byte, 32768)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			out = append(out, buf[:n]...)
		}
		if err != nil {
			break
		}
	}
	return out, nil
}

type parser struct {
	b   []byte
	o   int
	err error
}

func (p *parser) need(n int) bool {
	if p.err != nil {
		return false
	}
	if p.o+n > len(p.b) {
		p.err = fmt.Errorf("class truncado")
		return false
	}
	return true
}

func (p *parser) u8() byte {
	if !p.need(1) {
		return 0
	}
	v := p.b[p.o]
	p.o++
	return v
}

func (p *parser) u16() uint16 {
	if !p.need(2) {
		return 0
	}
	v := binary.BigEndian.Uint16(p.b[p.o:])
	p.o += 2
	return v
}

func (p *parser) u32() uint32 {
	if !p.need(4) {
		return 0
	}
	v := binary.BigEndian.Uint32(p.b[p.o:])
	p.o += 4
	return v
}

func (p *parser) bytes(n int) []byte {
	if !p.need(n) {
		return nil
	}
	v := p.b[p.o : p.o+n]
	p.o += n
	return v
}

func (p *parser) skip(n int) {
	if p.need(n) {
		p.o += n
	}
}
