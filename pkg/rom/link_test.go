package rom

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// The fixtures are real M68k objects (testdata/*.c, rebuilt by testdata/make.sh), so the
// linker is tested against what llc and clang write — without the toolchain, which CI
// does not have.

func fixture(t *testing.T, name string) Object {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name+".o"))
	if err != nil {
		t.Fatal(err)
	}
	return Object{Name: name + ".o", Bytes: data}
}

func linkFixtures(t *testing.T) *Image {
	t.Helper()
	img, err := Link(
		[]Object{fixture(t, "start"), fixture(t, "program")},
		[]Object{fixture(t, "unused"), fixture(t, "helper")},
		GenesisLayout)
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	return img
}

func word32(rom []byte, at uint32) uint32 { return binary.BigEndian.Uint32(rom[at:]) }

// The layout the Genesis runtime relies on: vectors at 0 pointing at _start, code from
// 200, initialized data running in RAM from FF0000 with its bytes loaded from ROM, zeroed
// data after it.
func TestLink_LaysOutTheGenesisMap(t *testing.T) {
	img := linkFixtures(t)
	sym := img.Symbols
	if got := word32(img.ROM, 4); got != sym["_start"] {
		t.Errorf("reset vector %#x, want _start at %#x", got, sym["_start"])
	}
	if word32(img.ROM, 0) != 0x01000000 {
		t.Errorf("stack vector %#x, want 01000000", word32(img.ROM, 0))
	}
	for _, name := range []string{"_start", "main", "helper", "message"} {
		if sym[name] < 0x200 || sym[name] >= uint32(len(img.ROM)) {
			t.Errorf("%s at %#x, want in ROM after the header", name, sym[name])
		}
	}
	if sym["counter"] < 0xFF0000 || sym["where"] < 0xFF0000 {
		t.Errorf("data runs in RAM: counter %#x, where %#x", sym["counter"], sym["where"])
	}
	if sym["zeroed"] < sym["__bss_start"] || sym["zeroed"] >= sym["__bss_end"] || sym["__bss_start"] < sym["__data_end"] {
		t.Errorf("bss after data: zeroed %#x in [%#x, %#x), data ends %#x",
			sym["zeroed"], sym["__bss_start"], sym["__bss_end"], sym["__data_end"])
	}
	if !bytes.HasPrefix(img.ROM[sym["message"]:], []byte("LYRA\x00")) {
		t.Errorf("message's bytes are not at its address")
	}
}

// Initialized data is in ROM at __data_load, in RAM order, relocated: counter's 7, and
// where's pointer to message (an R_68K_32 inside .data, resolved to the ROM address).
func TestLink_LoadsInitializedDataFromROM(t *testing.T) {
	img := linkFixtures(t)
	sym := img.Symbols
	load := func(ramAddr uint32) uint32 { return sym["__data_load"] + (ramAddr - sym["__data_start"]) }
	if got := binary.BigEndian.Uint16(img.ROM[load(sym["counter"]):]); got != 7 {
		t.Errorf("counter's load image holds %d, want 7", got)
	}
	if got := word32(img.ROM, load(sym["where"])); got != sym["message"] {
		t.Errorf("where's load image holds %#x, want message at %#x", got, sym["message"])
	}
}

// A library object is linked because something needs it, and only then.
func TestLink_PullsOnlyTheLibraryObjectsNeeded(t *testing.T) {
	img := linkFixtures(t)
	if _, linked := img.Symbols["unused"]; linked {
		t.Errorf("unused.o was linked though nothing calls it")
	}
	missing, err := Undefined([]Object{fixture(t, "start"), fixture(t, "program")}, nil)
	if err != nil || len(missing) != 1 || missing[0] != "helper" {
		t.Errorf("without the library, helper is undefined; got %v, %v", missing, err)
	}
	_, err = Link([]Object{fixture(t, "start"), fixture(t, "program")}, nil, GenesisLayout)
	var undefined *UndefinedError
	if !errors.As(err, &undefined) || undefined.Symbol != "helper" {
		t.Errorf("expected helper to be undefined; got %v", err)
	}
}

// The PC-relative relocations llc writes for a branch or a PC-relative operand: the
// distance from the field, and a refusal when it does not fit.
func TestPatch_PCRelative(t *testing.T) {
	data := make([]byte, 8)
	if err := patch(data, 2, r68kPC16, 0x1000, 0x0FF0); err != nil || binary.BigEndian.Uint16(data[2:]) != 0x10 {
		t.Errorf("PC16 +0x10: %x, %v", data, err)
	}
	if err := patch(data, 4, r68kPC32, 0x100, 0x200); err != nil || int32(binary.BigEndian.Uint32(data[4:])) != -0x100 {
		t.Errorf("PC32 -0x100: %x, %v", data, err)
	}
	if err := patch(data, 2, r68kPC16, 0x20000, 0); err == nil {
		t.Errorf("a PC16 distance of 0x20000 must be refused")
	}
}

// The cartridge: whole 128 KB banks, the system name TMSS looks for, the title, and a
// checksum that matches.
func TestGenesisCartridge_Header(t *testing.T) {
	rom := GenesisCartridge(linkFixtures(t), "fixture game")
	if len(rom)%0x20000 != 0 {
		t.Errorf("ROM is %d bytes, want whole 128 KB banks", len(rom))
	}
	if string(rom[0x100:0x10F]) != "SEGA MEGA DRIVE" || string(rom[0x120:0x12C]) != "FIXTURE GAME" {
		t.Errorf("header fields: %q / %q", rom[0x100:0x110], rom[0x120:0x150])
	}
	if got := binary.BigEndian.Uint16(rom[0x18E:]); got != Checksum(rom) {
		t.Errorf("checksum %#x, want %#x", got, Checksum(rom))
	}
	if word32(rom, 0x1A4) != uint32(len(rom)-1) {
		t.Errorf("ROM end %#x, want %#x", word32(rom, 0x1A4), len(rom)-1)
	}
}

// A weak definition is a default: linked when nothing else defines the name, replaced —
// not a duplicate — when something does (the runtime's `__lyra_interrupt_vblank` and a
// program's `@interrupt(vblank)` function).
func TestLink_AStrongDefinitionReplacesAWeakOne(t *testing.T) {
	required := []Object{fixture(t, "start"), fixture(t, "program"), fixture(t, "weak")}
	library := []Object{fixture(t, "helper")}
	alone, err := Link(required, library, GenesisLayout)
	if err != nil {
		t.Fatalf("the weak default alone: %v", err)
	}
	both, err := Link(append(required, fixture(t, "strong")), library, GenesisLayout)
	if err != nil {
		t.Fatalf("a strong definition beside the weak one must not be a duplicate: %v", err)
	}
	if alone.Symbols["hook"] == both.Symbols["hook"] {
		t.Errorf("hook is at %#x either way; the strong definition should replace the weak one", both.Symbols["hook"])
	}
	if _, err := Link(append(required, fixture(t, "strong"), fixture(t, "strong")), library, GenesisLayout); err == nil {
		t.Errorf("two strong definitions are still a duplicate")
	}
}
