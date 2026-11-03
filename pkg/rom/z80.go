package rom

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"fmt"
	"sort"
)

// The Z80 linker: tools/llvm-z80's objects into a Master System or Game Gear cartridge. Each
// function and each constant is its own section, and only what `main` and the interrupt
// handlers reach goes in: at most 48 KB fits without bank switching, and the runtime's
// helpers are most of a small game otherwise.

const (
	z80CodeStart  = 0x0100
	z80HeaderAt   = 0x7FF0
	z80HeaderEnd  = 0x8000
	z80ROMEnd     = 0xC000
	z80RAMStart   = 0xC000
	z80StackTop   = 0xDFF0
	z80StackSpare = 0x0400 // what the stack is left, at the least
	z80Vblank     = "__lyra_interrupt_vblank"
	z80Hblank     = "__lyra_interrupt_hblank"
	z80CallBC     = "__z80_callbc"
	z80CallBCAt   = 0x0068

	emZ80 = elf.Machine(220)

	rZ80_8  = 1
	rZ80_16 = 4
	rZ80_32 = 6
)

// Z80Image is what the linker made: the ROM up to its last used byte, and the RAM it asks.
type Z80Image struct {
	ROM      []byte
	DataSize int
	BSSSize  int
	// Used is how many bytes of ROM hold code and constants.
	Used int
}

type z80Section struct {
	object  int
	name    string
	data    []byte
	size    int
	kind    z80Kind
	relocs  []z80Reloc
	address int
	load    int
	reached bool
}

type z80Kind int

const (
	z80ROM z80Kind = iota
	z80Data
	z80BSS
)

type z80Reloc struct {
	offset int
	kind   uint32
	addend int64
	target z80Target
}

// z80Target is a relocation's symbol: a section and offset for a local one, a name otherwise.
type z80Target struct {
	name    string
	section *z80Section
	value   int
}

type z80Symbol struct {
	section *z80Section
	value   int
	weak    bool
}

// UndefinedZ80 is the linker's error for symbols nothing defines.
type UndefinedZ80 struct{ Names []string }

func (e *UndefinedZ80) Error() string { return fmt.Sprintf("nothing defines %v", e.Names) }

// LinkZ80 links objects into an image: the start-up in the low page, then what is reached.
func LinkZ80(objects []Object) (*Z80Image, error) {
	var sections []*z80Section
	globals := map[string]z80Symbol{}
	for index, object := range objects {
		found, err := readZ80Object(index, object, globals)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", object.Name, err)
		}
		sections = append(sections, found...)
	}

	// What is reached, from the roots.
	var missing []string
	seen := map[string]bool{}
	var reach func(t z80Target)
	var visit func(s *z80Section)
	visit = func(s *z80Section) {
		if s.reached {
			return
		}
		s.reached = true
		for _, r := range s.relocs {
			reach(r.target)
		}
	}
	reach = func(t z80Target) {
		if t.section != nil {
			visit(t.section)
			return
		}
		if t.name == z80CallBC {
			return
		}
		if sym, ok := globals[t.name]; ok {
			visit(sym.section)
		} else if !seen[t.name] {
			seen[t.name] = true
			missing = append(missing, t.name)
		}
	}
	reach(z80Target{name: "main"})
	for _, root := range []string{z80Vblank, z80Hblank} {
		if _, ok := globals[root]; ok {
			reach(z80Target{name: root})
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, &UndefinedZ80{Names: missing}
	}

	// ROM, skipping the header: code and constants, then what .data starts as.
	rom := make([]byte, z80CodeStart)
	place := func(size int) int {
		at := len(rom)
		if at < z80HeaderEnd && at+size > z80HeaderAt {
			at = z80HeaderEnd
		}
		if at+size > z80ROMEnd {
			return -1
		}
		for len(rom) < at+size {
			rom = append(rom, 0)
		}
		return at
	}
	total := 0
	for _, s := range sections {
		if s.reached && s.kind != z80BSS {
			total += s.size
		}
	}
	tooBig := func() error {
		return fmt.Errorf("code and constants take %d bytes; a cartridge without bank switching holds %d", total, z80ROMEnd-z80CodeStart-16)
	}
	for _, s := range sections {
		if s.reached && s.kind == z80ROM {
			if s.address = place(s.size); s.address < 0 {
				return nil, tooBig()
			}
			copy(rom[s.address:], s.data)
		}
	}

	ram := z80RAMStart
	dataSize := 0
	for _, s := range sections {
		if s.reached && s.kind == z80Data {
			s.address = ram
			ram += s.size
			dataSize += s.size
		}
	}
	dataLoad := 0
	if dataSize > 0 {
		if dataLoad = place(dataSize); dataLoad < 0 {
			return nil, tooBig()
		}
		at := dataLoad
		for _, s := range sections {
			if s.reached && s.kind == z80Data {
				s.load = at
				copy(rom[at:], s.data)
				at += s.size
			}
		}
	}
	bssStart := ram
	bssSize := 0
	for _, s := range sections {
		if s.reached && s.kind == z80BSS {
			s.address = ram
			ram += s.size
			bssSize += s.size
		}
	}
	if ram > z80StackTop-z80StackSpare {
		return nil, fmt.Errorf("globals take %d bytes of RAM; %d is the most that leaves the stack room", ram-z80RAMStart, z80StackTop-z80StackSpare-z80RAMStart)
	}

	resolve := func(t z80Target) int {
		if t.section != nil {
			return t.section.address + t.value
		}
		if t.name == z80CallBC {
			return z80CallBCAt
		}
		sym := globals[t.name]
		return sym.section.address + sym.value
	}
	for _, s := range sections {
		if !s.reached || s.kind == z80BSS {
			continue
		}
		base := s.address
		if s.kind == z80Data {
			base = s.load
		}
		for _, r := range s.relocs {
			value := int64(resolve(r.target)) + r.addend
			switch r.kind {
			case rZ80_8:
				rom[base+r.offset] = byte(value)
			case rZ80_16:
				binary.LittleEndian.PutUint16(rom[base+r.offset:], uint16(value))
			case rZ80_32:
				binary.LittleEndian.PutUint32(rom[base+r.offset:], uint32(value))
			default:
				return nil, fmt.Errorf("%s: relocation type %d", s.name, r.kind)
			}
		}
	}

	address := func(name string) (int, bool) {
		if _, ok := globals[name]; !ok {
			return 0, false
		}
		return resolve(z80Target{name: name}), true
	}
	if err := writeZ80Startup(rom, dataLoad, dataSize, bssStart, bssSize, address); err != nil {
		return nil, err
	}
	return &Z80Image{ROM: rom, DataSize: dataSize, BSSSize: bssSize, Used: total}, nil
}

// readZ80Object adds an object's global symbols to globals, a strong one over a weak one,
// and answers its sections that take up room in the program.
func readZ80Object(index int, object Object, globals map[string]z80Symbol) ([]*z80Section, error) {
	f, err := elf.NewFile(bytes.NewReader(object.Bytes))
	if err != nil {
		return nil, err
	}
	if f.Machine != emZ80 || f.Class != elf.ELFCLASS32 || f.Data != elf.ELFDATA2LSB {
		return nil, fmt.Errorf("not a Z80 object (machine %v)", f.Machine)
	}
	byIndex := map[int]*z80Section{}
	var out []*z80Section
	for i, sec := range f.Sections {
		if sec.Flags&elf.SHF_ALLOC == 0 || sec.Size == 0 {
			continue
		}
		s := &z80Section{object: index, name: sec.Name, size: int(sec.Size)}
		switch {
		case sec.Type == elf.SHT_NOBITS:
			s.kind = z80BSS
		case sec.Type != elf.SHT_PROGBITS:
			return nil, fmt.Errorf("section %s of a type the console has no use for (%v)", sec.Name, sec.Type)
		default:
			if sec.Flags&elf.SHF_WRITE != 0 {
				s.kind = z80Data
			}
			if s.data, err = sec.Data(); err != nil {
				return nil, err
			}
		}
		byIndex[i] = s
		out = append(out, s)
	}
	symbols, err := f.Symbols()
	if err != nil && len(f.Sections) > 0 {
		symbols = nil
	}
	for _, sym := range symbols {
		bind := elf.ST_BIND(sym.Info)
		if bind == elf.STB_LOCAL || sym.Section == elf.SHN_UNDEF {
			continue
		}
		s := byIndex[int(sym.Section)]
		if s == nil {
			continue
		}
		weak := bind == elf.STB_WEAK
		if have, exists := globals[sym.Name]; exists {
			if !have.weak && !weak {
				return nil, fmt.Errorf("%s is defined twice", sym.Name)
			}
			if weak || !have.weak {
				continue
			}
		}
		globals[sym.Name] = z80Symbol{section: s, value: int(sym.Value), weak: weak}
	}
	for _, sec := range f.Sections {
		if sec.Type != elf.SHT_RELA {
			continue
		}
		s := byIndex[int(sec.Info)]
		if s == nil {
			continue
		}
		data, err := sec.Data()
		if err != nil {
			return nil, err
		}
		for at := 0; at+12 <= len(data); at += 12 {
			info := binary.LittleEndian.Uint32(data[at+4:])
			r := z80Reloc{
				offset: int(binary.LittleEndian.Uint32(data[at:])),
				kind:   info & 0xFF,
				addend: int64(int32(binary.LittleEndian.Uint32(data[at+8:]))),
			}
			which := int(info >> 8)
			if which == 0 || which > len(symbols) {
				return nil, fmt.Errorf("%s: a relocation with no symbol", s.name)
			}
			sym := symbols[which-1]
			if elf.ST_BIND(sym.Info) == elf.STB_LOCAL {
				r.target = z80Target{section: byIndex[int(sym.Section)], value: int(sym.Value)}
			} else {
				r.target = z80Target{name: sym.Name}
			}
			s.relocs = append(s.relocs, r)
		}
	}
	return out, nil
}

// writeZ80Startup writes the low page: reset, the interrupt, the NMI, and the start-up that
// sets up the memory and calls `main`. Hand-assembled; `main`'s arguments are zero.
func writeZ80Startup(rom []byte, dataLoad, dataSize, bssStart, bssSize int, address func(string) (int, bool)) error {
	const startAt, irqAt = 0x0070, 0x00C0
	word := func(n int) (byte, byte) { return byte(n), byte(n >> 8) }
	put := func(at int, bytes ...byte) int {
		copy(rom[at:], bytes)
		return at + len(bytes)
	}
	lo, hi := word(startAt)
	put(0, 0xF3, 0xED, 0x56, 0xC3, lo, hi) // di; im 1; jp start
	lo, hi = word(irqAt)
	put(0x38, 0xC3, lo, hi)      // jp irq
	put(0x66, 0xED, 0x45)        // retn
	put(z80CallBCAt, 0xC5, 0xC9) // push bc; ret

	at := startAt
	lo, hi = word(z80StackTop)
	at = put(at, 0x31, lo, hi) // ld sp, top
	// The mapper's slots in their power-on order, whatever the BIOS left.
	for _, reg := range []struct{ value, low byte }{{0, 0xFC}, {0, 0xFD}, {1, 0xFE}, {2, 0xFF}} {
		at = put(at, 0x3E, reg.value, 0x32, reg.low, 0xFF) // ld a, n; ld (nn), a
	}
	if dataSize > 0 {
		a, b := word(dataLoad)
		c, d := word(z80RAMStart)
		e, f := word(dataSize)
		at = put(at, 0x21, a, b, 0x11, c, d, 0x01, e, f, 0xED, 0xB0) // ld hl; ld de; ld bc; ldir
	}
	if bssSize > 0 {
		a, b := word(bssStart)
		e, f := word(bssSize)
		at = put(at, 0x21, a, b, 0x01, e, f)                         // ld hl; ld bc
		at = put(at, 0x36, 0x00, 0x23, 0x0B, 0x78, 0xB1, 0x20, 0xF8) // ld (hl), 0; inc hl; dec bc; ld a, b; or c; jr nz
	}
	main, ok := address("main")
	if !ok {
		return &UndefinedZ80{Names: []string{"main"}}
	}
	lo, hi = word(main)
	at = put(at, 0xFB, 0x21, 0, 0, 0x11, 0, 0, 0xCD, lo, hi) // ei; ld hl, 0; ld de, 0; call main
	at = put(at, 0x76, 0x18, 0xFD)                           // halt; jr back
	if at > irqAt {
		return fmt.Errorf("the start-up overran its room (%#x)", at)
	}

	// The interrupt saves what a compiled handler may use. Reading the status acknowledges
	// it, and bit 7 says a frame's rather than a line's.
	at = put(irqAt, 0xF5, 0xC5, 0xD5, 0xE5, 0xDD, 0xE5) // push af, bc, de, hl, ix
	at = put(at, 0xDB, 0xBF, 0xB7)                      // in a, (0xBF); or a
	vblank, hasVblank := address(z80Vblank)
	hblank, hasHblank := address(z80Hblank)
	if hasVblank || hasHblank {
		lineAt := at + 3 + 6
		a, b := word(lineAt)
		at = put(at, 0xF2, a, b) // jp p, line
		if hasVblank {
			c, d := word(vblank)
			at = put(at, 0xCD, c, d) // call vblank
		} else {
			at = put(at, 0, 0, 0)
		}
		end := at + 3 + 3
		e, f := word(end)
		at = put(at, 0xC3, e, f) // jp done
		if hasHblank {
			c, d := word(hblank)
			at = put(at, 0xCD, c, d) // line: call hblank
		} else {
			at = put(at, 0, 0, 0)
		}
	}
	at = put(at, 0xDD, 0xE1, 0xE1, 0xD1, 0xC1, 0xF1, 0xFB, 0xED, 0x4D) // pop ix, hl, de, bc, af; ei; reti
	if at > z80CodeStart {
		return fmt.Errorf("the interrupt overran its room (%#x)", at)
	}
	return nil
}

// Z80Cartridge is the image as a cartridge, padded to the size its header says, with the
// `TMR SEGA` header and checksum. A Game Gear's region code is 7, a Master System's 4.
func Z80Cartridge(img *Z80Image, gameGear bool) []byte {
	size := 0x8000
	sizeCode := byte(0xC)
	if len(img.ROM) > z80HeaderAt {
		size = z80ROMEnd
		sizeCode = 0xD
	}
	rom := make([]byte, size)
	copy(rom, img.ROM)
	copy(rom[z80HeaderAt:], "TMR SEGA")
	region := byte(4)
	if gameGear {
		region = 7
	}
	rom[z80HeaderAt+0xF] = region<<4 | sizeCode
	var sum uint16
	for i, b := range rom {
		if i < z80HeaderAt || i >= z80HeaderEnd {
			sum += uint16(b)
		}
	}
	binary.LittleEndian.PutUint16(rom[z80HeaderAt+0xA:], sum)
	return rom
}
