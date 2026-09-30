// Package rom links 68000 ELF objects into a console cartridge image — the last step of
// `lyrac build` for a target with no operating system (package target), where there is
// no system linker to hand the objects to (LLVM's lld has no M68k port).
//
// It is a small linker because the job is small: relocatable ELF32 big-endian objects
// from llc and clang, three kinds of section (code and constants in ROM, initialized data
// copied to RAM at start-up, zeroed data in RAM), six relocation types, and archive
// semantics for the runtime's helpers — a library object is linked only if it defines a
// symbol something already linked needs, so an integer-only game carries no soft float.
//
// The console's own layout (where ROM and RAM are, the header) is a Layout; genesis.go
// is the Genesis's.
package rom

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// Object is one relocatable ELF file to link: its name (for messages) and bytes.
type Object struct {
	Name  string
	Bytes []byte
}

// Layout is where a console puts things.
type Layout struct {
	// VectorsSection is placed first, at address 0, and must be VectorsSize bytes.
	VectorsSection string
	VectorsSize    uint32
	// CodeStart is where code, constants and the initialized data's load image begin in
	// ROM, after the vectors and whatever the console keeps between them (a header).
	CodeStart uint32
	// RAMStart and RAMEnd bound initialized and zeroed data. RAMReserve is left free at
	// the top for the stack.
	RAMStart, RAMEnd, RAMReserve uint32
	// Entry is the symbol the vectors must reach for anything to run.
	Entry string
}

// Image is a linked program: the ROM's bytes from address 0, and every global symbol's
// address.
type Image struct {
	ROM     []byte
	Symbols map[string]uint32
	// DataSize and BSSSize are the RAM the program takes, for a report.
	DataSize, BSSSize uint32
}

// The M68k relocation types (the System V ABI's m68k supplement).
const (
	r68kNone = 0
	r68k32   = 1
	r68k16   = 2
	r68k8    = 3
	r68kPC32 = 4
	r68kPC16 = 5
	r68kPC8  = 6
)

// Link lays out required (all linked) and library (linked on demand) under layout.
func Link(required, library []Object, layout Layout) (*Image, error) {
	var objs []*object
	for _, o := range required {
		obj, err := load(o)
		if err != nil {
			return nil, err
		}
		objs = append(objs, obj)
	}
	var lib []*object
	for _, o := range library {
		obj, err := load(o)
		if err != nil {
			return nil, err
		}
		lib = append(lib, obj)
	}
	objs, err := pullLibrary(objs, lib)
	if err != nil {
		return nil, err
	}
	return place(objs, layout)
}

// object is a loaded ELF file.
type object struct {
	name     string
	file     *elf.File
	raw      []byte
	symbols  []elf.Symbol // index i is ELF symbol i+1 (debug/elf drops the null symbol)
	sections []*section   // by ELF section index; nil for a section not placed
}

// section is an allocated section of an object, and where it went.
type section struct {
	obj     *object
	header  *elf.Section
	index   int
	kind    sectionKind
	data    []byte // nil for bss
	size    uint32
	align   uint32
	address uint32 // where it runs
	load    uint32 // where its bytes are in ROM (differs from address for data)
}

type sectionKind int

const (
	kindVectors sectionKind = iota
	kindText
	kindRodata
	kindData
	kindBSS
)

func load(o Object) (*object, error) {
	f, err := elf.NewFile(bytes.NewReader(o.Bytes))
	if err != nil {
		return nil, fmt.Errorf("%s: not an ELF object: %v", o.Name, err)
	}
	if f.Class != elf.ELFCLASS32 || f.Data != elf.ELFDATA2MSB || f.Machine != elf.EM_68K || f.Type != elf.ET_REL {
		return nil, fmt.Errorf("%s: not a 68000 relocatable object (%s %s %s %s)", o.Name, f.Class, f.Data, f.Machine, f.Type)
	}
	symbols, err := f.Symbols()
	if err != nil && err != elf.ErrNoSymbols {
		return nil, fmt.Errorf("%s: %v", o.Name, err)
	}
	obj := &object{name: o.Name, file: f, raw: o.Bytes, symbols: symbols, sections: make([]*section, len(f.Sections))}
	for i, h := range f.Sections {
		if h.Flags&elf.SHF_ALLOC == 0 || h.Size == 0 {
			continue
		}
		s := &section{obj: obj, header: h, index: i, size: uint32(h.Size), align: uint32(max(h.Addralign, 1))}
		switch {
		case h.Name == ".vectors":
			s.kind = kindVectors
		case h.Type == elf.SHT_NOBITS:
			s.kind = kindBSS
		case h.Flags&elf.SHF_EXECINSTR != 0:
			s.kind = kindText
		case h.Flags&elf.SHF_WRITE != 0:
			s.kind = kindData
		default:
			s.kind = kindRodata
		}
		if s.kind != kindBSS {
			if s.data, err = h.Data(); err != nil {
				return nil, fmt.Errorf("%s: section %s: %v", o.Name, h.Name, err)
			}
			s.data = append([]byte(nil), s.data...)
		}
		obj.sections[i] = s
	}
	for _, sym := range symbols {
		if sym.Section == elf.SHN_COMMON {
			return nil, fmt.Errorf("%s: %s is a common symbol; compile with -fno-common", o.Name, sym.Name)
		}
	}
	return obj, nil
}

// defines reports the global symbols obj defines.
func (o *object) defines() []string {
	var out []string
	for _, s := range o.symbols {
		if elf.ST_BIND(s.Info) != elf.STB_LOCAL && s.Section != elf.SHN_UNDEF && s.Name != "" {
			out = append(out, s.Name)
		}
	}
	return out
}

// isWeak reports whether obj's definition of name is weak.
func (o *object) isWeak(name string) bool {
	for _, s := range o.symbols {
		if s.Name == name && s.Section != elf.SHN_UNDEF {
			return elf.ST_BIND(s.Info) == elf.STB_WEAK
		}
	}
	return false
}

// needs reports the global symbols obj uses and does not define.
func (o *object) needs() []string {
	var out []string
	for _, s := range o.symbols {
		if s.Section == elf.SHN_UNDEF && s.Name != "" {
			out = append(out, s.Name)
		}
	}
	return out
}

// pullLibrary adds each library object that defines a symbol the linked objects need, until
// nothing more is needed that the library has.
func pullLibrary(objs, lib []*object) ([]*object, error) {
	defined := map[string]*object{}
	for _, o := range objs {
		for _, name := range o.defines() {
			if prev, dup := defined[name]; dup {
				// A weak definition yields to a strong one — the runtime's do-nothing
				// interrupt handler to the program's `@interrupt` function.
				if o.isWeak(name) {
					continue
				}
				if !prev.isWeak(name) {
					return nil, fmt.Errorf("%s is defined twice: in %s and %s", name, prev.name, o.name)
				}
			}
			defined[name] = o
		}
	}
	linked := map[*object]bool{}
	for changed := true; changed; {
		changed = false
		for _, o := range objs {
			for _, name := range o.needs() {
				if defined[name] != nil {
					continue
				}
				for _, candidate := range lib {
					if linked[candidate] || !contains(candidate.defines(), name) {
						continue
					}
					linked[candidate] = true
					objs = append(objs, candidate)
					for _, n := range candidate.defines() {
						if defined[n] == nil {
							defined[n] = candidate
						}
					}
					changed = true
					break
				}
			}
		}
	}
	return objs, nil
}

func contains(names []string, name string) bool { return slices.Contains(names, name) }

// place assigns addresses, resolves symbols and applies relocations.
func place(objs []*object, layout Layout) (*Image, error) {
	var vectors *section
	var text, rodata, data, bss []*section
	for _, o := range objs {
		for _, s := range o.sections {
			if s == nil {
				continue
			}
			switch s.kind {
			case kindVectors:
				if vectors != nil {
					return nil, fmt.Errorf("two vector tables: %s and %s", vectors.obj.name, o.name)
				}
				vectors = s
			case kindText:
				text = append(text, s)
			case kindRodata:
				rodata = append(rodata, s)
			case kindData:
				data = append(data, s)
			case kindBSS:
				bss = append(bss, s)
			}
		}
	}
	if vectors == nil || vectors.size != layout.VectorsSize {
		return nil, fmt.Errorf("the runtime must supply a %d-byte %s section", layout.VectorsSize, layout.VectorsSection)
	}
	vectors.address, vectors.load = 0, 0

	at := layout.CodeStart
	for _, group := range [][]*section{text, rodata} {
		for _, s := range group {
			at = alignUp(at, s.align)
			s.address, s.load = at, at
			at += s.size
		}
	}
	// Initialized data runs in RAM and is loaded from ROM, and the start-up copies it as
	// one block — so each section's ROM copy sits at the same offset from __data_load as
	// the section does from __data_start. Aligning the two separately put the first
	// section two bytes off its copy when it wanted 4-byte alignment and ROM was at an odd
	// word, shifting every global (09/30, the fixture tests). The block starts 4-aligned in
	// both, and ends even, as the word copy wants.
	ram := layout.RAMStart
	dataLoad := alignUp(at, 4)
	for _, s := range data {
		ram = alignUp(ram, s.align)
		s.address = ram
		s.load = dataLoad + (ram - layout.RAMStart)
		ram += s.size
	}
	ram = alignUp(ram, 2)
	dataEnd := ram
	romEnd := dataLoad + (dataEnd - layout.RAMStart)
	bssStart := ram
	for _, s := range bss {
		ram = alignUp(ram, s.align)
		s.address = ram
		ram += s.size
	}
	ram = alignUp(ram, 2)
	if ram > layout.RAMEnd-layout.RAMReserve {
		return nil, fmt.Errorf("the program's data takes %d bytes of RAM; there are %d, less %d for the stack",
			ram-layout.RAMStart, layout.RAMEnd-layout.RAMStart, layout.RAMReserve)
	}

	globals := map[string]uint32{
		"__data_load":  dataLoad,
		"__data_start": layout.RAMStart,
		"__data_end":   dataEnd,
		"__bss_start":  bssStart,
		"__bss_end":    ram,
	}
	// Strong definitions first, then weak ones only where nothing strong defined the name:
	// a weak symbol is a default the program may replace.
	weakPass := false
	for pass := 0; pass < 2; pass++ {
		for _, o := range objs {
			for _, sym := range o.symbols {
				if elf.ST_BIND(sym.Info) == elf.STB_LOCAL || sym.Section == elf.SHN_UNDEF || sym.Name == "" {
					continue
				}
				if (elf.ST_BIND(sym.Info) == elf.STB_WEAK) != weakPass {
					continue
				}
				addr, err := o.symbolAddress(sym)
				if err != nil {
					return nil, err
				}
				if _, dup := globals[sym.Name]; dup {
					if weakPass {
						continue
					}
					return nil, fmt.Errorf("%s is defined twice (again in %s)", sym.Name, o.name)
				}
				globals[sym.Name] = addr
			}
		}
		weakPass = true
	}
	if _, ok := globals[layout.Entry]; !ok {
		return nil, fmt.Errorf("nothing defines %s, the program's entry", layout.Entry)
	}

	rom := make([]byte, romEnd)
	for _, o := range objs {
		for _, s := range o.sections {
			if s == nil || s.kind == kindBSS {
				continue
			}
			if err := o.relocate(s, globals); err != nil {
				return nil, err
			}
			copy(rom[s.load:], s.data)
		}
	}
	return &Image{ROM: rom, Symbols: globals, DataSize: dataEnd - layout.RAMStart, BSSSize: ram - bssStart}, nil
}

// symbolAddress is where sym is: its section's address plus its value, or its value for an
// absolute symbol.
func (o *object) symbolAddress(sym elf.Symbol) (uint32, error) {
	if sym.Section == elf.SHN_ABS {
		return uint32(sym.Value), nil
	}
	idx := int(sym.Section)
	if idx >= len(o.sections) || o.sections[idx] == nil {
		return 0, fmt.Errorf("%s: %s is in a section that is not linked", o.name, sym.Name)
	}
	return o.sections[idx].address + uint32(sym.Value), nil
}

// relocate applies the relocations aimed at s, in place in s.data.
func (o *object) relocate(s *section, globals map[string]uint32) error {
	for _, h := range o.file.Sections {
		if h.Type != elf.SHT_RELA || int(h.Info) != s.index {
			continue
		}
		raw, err := h.Data()
		if err != nil {
			return fmt.Errorf("%s: %s: %v", o.name, h.Name, err)
		}
		for i := 0; i+12 <= len(raw); i += 12 {
			offset := binary.BigEndian.Uint32(raw[i:])
			info := binary.BigEndian.Uint32(raw[i+4:])
			addend := int32(binary.BigEndian.Uint32(raw[i+8:]))
			kind, symIndex := info&0xFF, info>>8
			if kind == r68kNone {
				continue
			}
			target, name, err := o.relocationTarget(symIndex, globals)
			if err != nil {
				return err
			}
			value := int64(target) + int64(addend)
			place := int64(s.address) + int64(offset)
			if err := patch(s.data, offset, kind, value, place); err != nil {
				return fmt.Errorf("%s: %s+%#x → %s: %v", o.name, s.header.Name, offset, name, err)
			}
		}
	}
	return nil
}

// relocationTarget is the address a relocation's symbol resolves to.
func (o *object) relocationTarget(symIndex uint32, globals map[string]uint32) (uint32, string, error) {
	if symIndex == 0 || int(symIndex) > len(o.symbols) {
		return 0, "", fmt.Errorf("%s: a relocation names symbol %d, which does not exist", o.name, symIndex)
	}
	sym := o.symbols[symIndex-1]
	if elf.ST_TYPE(sym.Info) == elf.STT_SECTION || elf.ST_BIND(sym.Info) == elf.STB_LOCAL {
		addr, err := o.symbolAddress(sym)
		return addr, sym.Name, err
	}
	addr, ok := globals[sym.Name]
	if !ok {
		return 0, sym.Name, &UndefinedError{Symbol: sym.Name, Object: o.name}
	}
	return addr, sym.Name, nil
}

// UndefinedError is a symbol nothing linked defines.
type UndefinedError struct {
	Symbol, Object string
}

func (e *UndefinedError) Error() string {
	return fmt.Sprintf("%s needs %s, which nothing defines", e.Object, e.Symbol)
}

// patch writes one relocated field.
func patch(data []byte, offset, kind uint32, value, place int64) error {
	switch kind {
	case r68k32:
		binary.BigEndian.PutUint32(data[offset:], uint32(value))
	case r68k16:
		if value < -0x8000 || value > 0xFFFF {
			return fmt.Errorf("%#x does not fit 16 bits", value)
		}
		binary.BigEndian.PutUint16(data[offset:], uint16(value))
	case r68k8:
		if value < -0x80 || value > 0xFF {
			return fmt.Errorf("%#x does not fit 8 bits", value)
		}
		data[offset] = byte(value)
	case r68kPC32:
		binary.BigEndian.PutUint32(data[offset:], uint32(value-place))
	case r68kPC16:
		d := value - place
		if d < -0x8000 || d > 0x7FFF {
			return fmt.Errorf("a PC-relative distance of %d does not fit 16 bits", d)
		}
		binary.BigEndian.PutUint16(data[offset:], uint16(d))
	case r68kPC8:
		d := value - place
		if d < -0x80 || d > 0x7F {
			return fmt.Errorf("a PC-relative distance of %d does not fit 8 bits", d)
		}
		data[offset] = byte(d)
	default:
		return fmt.Errorf("relocation type %d is not one this linker knows", kind)
	}
	return nil
}

func alignUp(v, align uint32) uint32 {
	if align <= 1 {
		return v
	}
	return (v + align - 1) / align * align
}

// Undefined lists, sorted, the symbols the linked objects need and none defines — for a
// message that can say what they are before Link fails on the first.
func Undefined(required, library []Object) ([]string, error) {
	var objs, lib []*object
	for _, o := range required {
		obj, err := load(o)
		if err != nil {
			return nil, err
		}
		objs = append(objs, obj)
	}
	for _, o := range library {
		obj, err := load(o)
		if err != nil {
			return nil, err
		}
		lib = append(lib, obj)
	}
	objs, err := pullLibrary(objs, lib)
	if err != nil {
		return nil, err
	}
	defined := map[string]bool{"__data_load": true, "__data_start": true, "__data_end": true, "__bss_start": true, "__bss_end": true}
	for _, o := range objs {
		for _, n := range o.defines() {
			defined[n] = true
		}
	}
	missing := map[string]bool{}
	for _, o := range objs {
		for _, n := range o.needs() {
			if !defined[n] {
				missing[n] = true
			}
		}
	}
	var out []string
	for n := range missing {
		out = append(out, n)
	}
	sort.Strings(out)
	return out, nil
}

// quoteList is names as `a`, `b` and `c`.
func quoteList(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = "`" + n + "`"
	}
	if len(quoted) <= 1 {
		return strings.Join(quoted, "")
	}
	return strings.Join(quoted[:len(quoted)-1], ", ") + " and " + quoted[len(quoted)-1]
}
