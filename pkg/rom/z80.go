package rom

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"fmt"
	"sort"
	"strings"
)

// The Z80 linker: tools/llvm-z80's objects into a Master System or Game Gear cartridge. Each
// function and each constant is its own section, and only what `main` and the interrupt
// handlers reach goes in: at most 48 KB fits without bank switching, and the runtime's
// helpers are most of a small game otherwise.

const (
	z80CodeStart  = 0x0180
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
	z80Otir       = "__z80_otir"
	z80OtirAt     = 0x006A
	z80Bank       = "__z80_bank"
	z80BankAt     = 0x0130
	// HL × DE, unsigned: the 32-bit product in DE:HL, so its low half in HL is __mulhi3's answer.
	z80Multiply   = "__z80_umul32"
	z80MulLow     = "__mulhi3"
	z80MultiplyAt = 0x0140

	// The mapper's register for the window at 8000, which reads as nothing, so what it was
	// set to is kept here too for an interrupt to put back.
	z80BankRegister = 0xFFFF
	z80BankShadow   = 0xDFF8
	z80BankWindow   = 0x8000
	z80BankSize     = 0x4000
	z80FirstBank    = 2
	z80MostBanks    = 64
	z80BankSection  = ".bank."
	z80BankSymbol   = "__lyra_bank."

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
	// Map is where each reached section went, `address size name` a line, with the bank in
	// front of a banked table's: for reading a profile.
	Map []string
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
	// bank is the ROM bank of a z80Banked section.
	bank int
}

type z80Kind int

const (
	z80ROM z80Kind = iota
	z80Data
	z80BSS
	z80Banked
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

	bankedByName := map[string]*z80Section{}
	for _, s := range sections {
		if s.kind == z80Banked {
			bankedByName[strings.TrimPrefix(s.name, z80BankSection)] = s
		}
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
		if t.name == z80CallBC || t.name == z80Otir || t.name == z80Bank || t.name == z80Multiply || t.name == z80MulLow {
			return
		}
		if strings.HasPrefix(t.name, z80BankSymbol) {
			if s := bankedByName[strings.TrimPrefix(t.name, z80BankSymbol)]; s != nil {
				visit(s)
			} else if !seen[t.name] {
				seen[t.name] = true
				missing = append(missing, t.name)
			}
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
	var fixedEnd int
	place := func(size int) int {
		at := len(rom)
		if at < z80HeaderEnd && at+size > z80HeaderAt {
			at = z80HeaderEnd
		}
		if at+size > fixedEnd {
			return -1
		}
		for len(rom) < at+size {
			rom = append(rom, 0)
		}
		return at
	}
	total := 0
	banked := false
	for _, s := range sections {
		if s.reached && s.kind == z80Banked {
			banked = true
		} else if s.reached && s.kind != z80BSS {
			total += s.size
		}
	}
	// With banks the window at 8000 is theirs, so what is not banked has the first 32 KB.
	fixedEnd = z80ROMEnd
	if banked {
		fixedEnd = z80HeaderAt
	}
	tooBig := func() error {
		if banked {
			return fmt.Errorf("code and constants not `@banked` take %d bytes; with banks they have %d", total, z80HeaderAt-z80CodeStart)
		}
		return fmt.Errorf("code and constants take %d bytes; a cartridge holds %d without banks: mark large tables `@banked`", total, z80ROMEnd-z80CodeStart-16)
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

	bankCount, err := placeZ80Banks(sections)
	if err != nil {
		return nil, err
	}
	if bankCount > 0 {
		image := make([]byte, (z80FirstBank+bankCount)*z80BankSize)
		copy(image, rom)
		rom = image
		for _, s := range sections {
			if s.reached && s.kind == z80Banked {
				copy(rom[s.bank*z80BankSize+s.address-z80BankWindow:], s.data)
			}
		}
	}

	resolve := func(t z80Target) int {
		if strings.HasPrefix(t.name, z80BankSymbol) {
			return bankedByName[strings.TrimPrefix(t.name, z80BankSymbol)].bank
		}
		if t.name == z80Bank {
			return z80BankAt
		}
		if t.name == z80Multiply || t.name == z80MulLow {
			return z80MultiplyAt
		}
		if t.section != nil {
			return t.section.address + t.value
		}
		if t.name == z80CallBC {
			return z80CallBCAt
		}
		if t.name == z80Otir {
			return z80OtirAt
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
		if s.kind == z80Banked {
			base = s.bank*z80BankSize + s.address - z80BankWindow
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
	var layout []string
	for _, s := range sections {
		if s.reached {
			layout = append(layout, fmt.Sprintf("%d %d %s", s.address, s.size, s.name))
		}
	}
	sort.Slice(layout, func(i, j int) bool {
		var a, b int
		fmt.Sscan(layout[i], &a)
		fmt.Sscan(layout[j], &b)
		return a < b
	})
	return &Z80Image{ROM: rom, DataSize: dataSize, BSSSize: bssSize, Used: total, Map: layout}, nil
}

// placeZ80Banks gives each reached banked table a bank and its address in the window, largest
// first, each in the first bank it fits, and answers how many banks that took.
func placeZ80Banks(sections []*z80Section) (int, error) {
	var tables []*z80Section
	for _, s := range sections {
		if s.reached && s.kind == z80Banked {
			tables = append(tables, s)
		}
	}
	sort.SliceStable(tables, func(i, j int) bool { return tables[i].size > tables[j].size })
	var used []int
	for _, s := range tables {
		name := strings.TrimPrefix(strings.TrimPrefix(s.name, z80BankSection), "lyra_const_")
		if s.size > z80BankSize {
			return 0, fmt.Errorf("%s is %d bytes, and a `@banked` table must fit a %d-byte bank: split it", name, s.size, z80BankSize)
		}
		at := -1
		for i, full := range used {
			if full+s.size <= z80BankSize {
				at = i
				break
			}
		}
		if at < 0 {
			used = append(used, 0)
			at = len(used) - 1
		}
		s.bank = z80FirstBank + at
		s.address = z80BankWindow + used[at]
		used[at] += s.size
	}
	if z80FirstBank+len(used) > z80MostBanks {
		return 0, fmt.Errorf("banked tables take %d banks; a 1 MB cartridge has %d", len(used), z80MostBanks-z80FirstBank)
	}
	return len(used), nil
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
		case strings.HasPrefix(sec.Name, z80BankSection):
			s.kind = z80Banked
			if s.data, err = sec.Data(); err != nil {
				return nil, err
			}
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
	put(0x38, 0xC3, lo, hi)                      // jp irq
	put(0x66, 0xED, 0x45)                        // retn
	put(z80CallBCAt, 0xC5, 0xC9)                 // push bc; ret
	put(z80OtirAt, 0x43, 0x4A, 0xED, 0xB3, 0xC9) // ld b, e; ld c, d; otir; ret
	shadowLo, shadowHi := word(z80BankShadow)
	registerLo, registerHi := word(z80BankRegister)
	put(z80MultiplyAt, z80MultiplyCode...)
	// __z80_bank(bank in HL, table in DE) -> table in HL.
	put(z80BankAt, 0x7D, 0x32, registerLo, registerHi, 0x32, shadowLo, shadowHi, 0xEB, 0xC9) // ld a, l; ld (reg), a; ld (shadow), a; ex de, hl; ret

	at := startAt
	lo, hi = word(z80StackTop)
	at = put(at, 0x31, lo, hi) // ld sp, top
	// The mapper's slots in their power-on order, whatever the BIOS left.
	for _, reg := range []struct{ value, low byte }{{0, 0xFC}, {0, 0xFD}, {1, 0xFE}, {2, 0xFF}} {
		at = put(at, 0x3E, reg.value, 0x32, reg.low, 0xFF) // ld a, n; ld (nn), a
	}
	at = put(at, 0x32, shadowLo, shadowHi) // ld (shadow), a: the bank the last line set
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
	at = put(at, 0x3A, shadowLo, shadowHi, 0xF5)        // ld a, (shadow); push af
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
	// The bank the interrupted code had, in case the handler used another.
	at = put(at, 0xF1, 0x32, shadowLo, shadowHi, 0x32, registerLo, registerHi) // pop af; ld (shadow), a; ld (reg), a
	at = put(at, 0xDD, 0xE1, 0xE1, 0xD1, 0xC1, 0xF1, 0xFB, 0xED, 0x4D)         // pop ix, hl, de, bc, af; ei; reti
	if at > z80BankAt {
		return fmt.Errorf("the interrupt overran its room (%#x)", at)
	}
	return nil
}

// z80MultiplyCode is shift and add, sixteen times, about 900 T-states: what the runtime's
// multiplies come down to, and every other way of writing it in IR took three times as long.
var z80MultiplyCode = []byte{
	0x44, 0x4D, // ld b, h; ld c, l: the multiplicand
	0x21, 0x00, 0x00, // ld hl, 0
	0x3E, 0x10, // ld a, 16
	0x29,       // again: add hl, hl: DE:HL one left, DE's top bit the multiplier's next
	0xCB, 0x13, // rl e
	0xCB, 0x12, // rl d
	0x30, 0x04, // jr nc, skip
	0x09,       // add hl, bc
	0x30, 0x01, // jr nc, skip
	0x13,       // inc de
	0x3D,       // skip: dec a
	0x20, 0xF2, // jr nz, again
	0xC9, // ret
}

// Z80Cartridge is the image as a cartridge, padded to the size its header says, with the
// `TMR SEGA` header and checksum. A Game Gear's region code is 7, a Master System's 4.
func Z80Cartridge(img *Z80Image, gameGear bool) []byte {
	size := 0x8000
	sizeCode := byte(0xC)
	switch {
	case len(img.ROM) <= z80HeaderAt:
	case len(img.ROM) <= z80ROMEnd:
		size, sizeCode = z80ROMEnd, 0xD
	case len(img.ROM) <= 0x10000:
		size, sizeCode = 0x10000, 0xE
	case len(img.ROM) <= 0x20000:
		size, sizeCode = 0x20000, 0xF
	case len(img.ROM) <= 0x40000:
		size, sizeCode = 0x40000, 0x0
	case len(img.ROM) <= 0x80000:
		size, sizeCode = 0x80000, 0x1
	default:
		size, sizeCode = 0x100000, 0x2
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
