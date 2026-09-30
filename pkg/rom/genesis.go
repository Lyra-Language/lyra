package rom

import (
	"encoding/binary"
	"fmt"
	"strings"
)

// GenesisLayout is the Genesis's memory map as a linked program sees it: the 68000's 64
// vectors at 0, the cartridge header at 100–1FF, code from 200; 64 KB of RAM at FF0000,
// the top 4 KB left for the stack. The runtime (runtime/genesis) supplies the vectors and
// `_start`.
var GenesisLayout = Layout{
	VectorsSection: ".vectors",
	VectorsSize:    0x100,
	CodeStart:      0x200,
	RAMStart:       0xFF0000,
	RAMEnd:         0x1000000,
	RAMReserve:     0x1000,
	Entry:          "_start",
}

// GenesisCartridge is the ROM file for a linked image: the header written at 100 with
// `title` as the game's name, padded to a whole number of 128 KB banks, and the header's
// checksum. "SEGA MEGA DRIVE" is the system field TMSS consoles look for, not a name.
func GenesisCartridge(img *Image, title string) []byte {
	size := (len(img.ROM) + 0x1FFFF) / 0x20000 * 0x20000
	rom := make([]byte, size)
	copy(rom, img.ROM)
	putText(rom, 0x100, 16, "SEGA MEGA DRIVE")
	putText(rom, 0x110, 16, "(C)LYRA")
	putText(rom, 0x120, 48, title)
	putText(rom, 0x150, 48, title)
	putText(rom, 0x180, 14, "GM 00000000-00")
	putText(rom, 0x190, 16, "J")
	binary.BigEndian.PutUint32(rom[0x1A0:], 0)
	binary.BigEndian.PutUint32(rom[0x1A4:], uint32(size-1))
	binary.BigEndian.PutUint32(rom[0x1A8:], 0xFF0000)
	binary.BigEndian.PutUint32(rom[0x1AC:], 0xFFFFFF)
	putText(rom, 0x1B0, 64, "")
	putText(rom, 0x1F0, 16, "JUE")
	binary.BigEndian.PutUint16(rom[0x18E:], Checksum(rom))
	return rom
}

// Checksum is the header's: the sum of the words from 200 to the end.
func Checksum(rom []byte) uint16 {
	var sum uint16
	for i := 0x200; i+1 < len(rom); i += 2 {
		sum += binary.BigEndian.Uint16(rom[i:])
	}
	return sum
}

// putText writes text into width bytes at at: upper case, anything but printable ASCII a
// space, padded with spaces.
func putText(rom []byte, at, width int, text string) {
	text = strings.ToUpper(text)
	for i := 0; i < width; i++ {
		c := byte(' ')
		if i < len(text) && text[i] >= 0x20 && text[i] < 0x7F {
			c = text[i]
		}
		rom[at+i] = c
	}
}

// ExplainUndefined says what a program needing these symbols is asking for that the
// Genesis does not have — the front end's lyra-E084/E085 should have caught each, so
// reaching here means a form they do not see yet.
func ExplainUndefined(names []string) string {
	var heap, host, other []string
	for _, n := range names {
		switch n {
		case "malloc", "free", "realloc", "calloc":
			heap = append(heap, n)
		case "snprintf", "strtod", "fmod", "floor", "round", "time", "clock_gettime",
			"getentropy", "read", "getchar", "opendir", "closedir", "ioctl", "poll",
			"tcgetattr", "tcsetattr", "cfmakeraw", "memchr", "strlen":
			host = append(host, n)
		default:
			other = append(other, n)
		}
	}
	var parts []string
	if len(heap) > 0 {
		parts = append(parts, fmt.Sprintf("the program allocates on the heap (%s), and the Genesis has none", quoteList(heap)))
	}
	if len(host) > 0 {
		parts = append(parts, fmt.Sprintf("it calls the host's C library (%s), which the Genesis does not have", quoteList(host)))
	}
	if len(other) > 0 {
		parts = append(parts, fmt.Sprintf("nothing defines %s", quoteList(other)))
	}
	return strings.Join(parts, "; ")
}
