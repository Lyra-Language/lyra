package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// **`lyrac build` for the Master System makes a ROM that runs**: a Lyra program, compiled by
// our Z80 target (tools/llvm-z80), linked by pkg/rom with runtime/sms and run on Sheliak's
// smscheck, its Z80 core and a strict VDP. Skipped without the toolchain or smscheck: set
// $SMSCHECK, or have Sheliak built beside this repository.

func z80ToolchainOrSkip(t *testing.T) {
	t.Helper()
	if _, err := findZ80Toolchain(); err != nil {
		t.Skip(err)
	}
}

func smscheckOrSkip(t *testing.T, root string) string {
	t.Helper()
	path := os.Getenv("SMSCHECK")
	if path == "" {
		path = filepath.Join(root, "..", "sheliak", "build", "smscheck")
	}
	if _, err := os.Stat(path); err != nil {
		t.Skipf("no smscheck at %s: build Sheliak, or set SMSCHECK", path)
	}
	return path
}

// smsScreen runs romPath for frames frames with hold held, and answers the screen.
func smsScreen(t *testing.T, smscheck, romPath, frames, hold string) screen {
	t.Helper()
	ppm := romPath + "." + frames + hold + ".ppm"
	args := []string{romPath, "--frames", frames, "--ppm", ppm}
	if hold != "" {
		args = append(args, "--hold", hold)
	}
	if out, err := exec.Command(smscheck, args...).CombinedOutput(); err != nil {
		t.Fatalf("smscheck: %v\n%s", err, out)
	}
	data, err := os.ReadFile(ppm)
	if err != nil {
		t.Fatal(err)
	}
	var s screen
	var magic string
	var maxval int
	if n, err := fmt.Sscan(string(data), &magic, &s.width, &s.height, &maxval); err != nil || n != 4 {
		t.Fatalf("not a PPM: %v", err)
	}
	s.pixels = data[len(data)-s.width*s.height*3:]
	return s
}

// whiteLeft is the leftmost column with a white pixel, or -1.
func (s screen) whiteLeft() int {
	left := -1
	for k := 0; k+2 < len(s.pixels); k += 3 {
		if s.pixels[k] == 0xFF && s.pixels[k+1] == 0xFF && s.pixels[k+2] == 0xFF {
			if x := (k / 3) % s.width; left < 0 || x < left {
				left = x
			}
		}
	}
	return left
}

func TestSMS_TheWalkerRunsAndKeepsUpWithTheFrame(t *testing.T) {
	t.Parallel()
	z80ToolchainOrSkip(t)
	root := repoRoot(t)
	smscheck := smscheckOrSkip(t, root)
	setenv(t, "LYRA_STD", root)
	out := filepath.Join(t.TempDir(), "walker.sms")
	_, stderr, code := captureRun(t, "build", "-o", out, filepath.Join(root, "examples", "sms", "walker.lyra"))
	if code != 0 {
		t.Fatalf("build exited %d: %s", code, stderr)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 0x8000 || string(data[0x7FF0:0x7FF8]) != "TMR SEGA" {
		t.Fatalf("not a cartridge: %d bytes", len(data))
	}
	var sum uint16
	for _, b := range data[:0x7FF0] {
		sum += uint16(b)
	}
	if binary.LittleEndian.Uint16(data[0x7FFA:]) != sum {
		t.Errorf("the header's checksum is wrong")
	}
	if got := smsScreen(t, smscheck, out, "60", "").whiteLeft(); got != 124 {
		t.Errorf("with nothing held the square is at %d, want where it starts, 124", got)
	}
	// Two pixels a frame, every frame: a frame's work overrunning shows as half the distance.
	early := smsScreen(t, smscheck, out, "40", "right").whiteLeft()
	late := smsScreen(t, smscheck, out, "60", "right").whiteLeft()
	if early < 0 || late-early != 40 {
		t.Errorf("20 frames held moved the square from %d to %d, want 40 pixels", early, late)
	}
}

// The bouncers cleared their table with `memset`, and when the runtime's took the byte as C's int
// it read the length a slot too far and overwrote RAM: the game diverged from the host about
// frame 50 and trapped. Run long enough to see that, it must end with no trap's red.
func TestSMS_TheBouncersRunWithoutATrap(t *testing.T) {
	t.Parallel()
	z80ToolchainOrSkip(t)
	root := repoRoot(t)
	smscheck := smscheckOrSkip(t, root)
	setenv(t, "LYRA_STD", root)
	out := filepath.Join(t.TempDir(), "bouncers.sms")
	_, stderr, code := captureRun(t, "build", "-o", out, filepath.Join(root, "examples", "sms", "bouncers.lyra"))
	if code != 0 {
		t.Fatalf("build exited %d: %s", code, stderr)
	}
	if got := smsScreen(t, smscheck, out, "300", "").dominant(); got != "000000" {
		t.Errorf("after 300 frames the screen is %s, want black behind the squares", got)
	}
}

// A frame's work, held under what it takes now by a margin: a compiler change that loses a
// narrowed counter or a direct overflow jump shows here, and a game's frame is the budget.
func TestSMS_AFrameStaysWithinItsBudget(t *testing.T) {
	t.Parallel()
	z80ToolchainOrSkip(t)
	root := repoRoot(t)
	smscheck := smscheckOrSkip(t, root)
	setenv(t, "LYRA_STD", root)
	for _, c := range []struct {
		name string
		most int
	}{{"walker", 1500}, {"bouncers", 20000}} {
		out := filepath.Join(t.TempDir(), c.name+".sms")
		_, stderr, code := captureRun(t, "build", "-o", out, filepath.Join(root, "examples", "sms", c.name+".lyra"))
		if code != 0 {
			t.Fatalf("build exited %d: %s", code, stderr)
		}
		said, err := exec.Command(smscheck, out, "--frames", "100", "--ppm", out+".ppm", "--work", "on").CombinedOutput()
		if err != nil {
			t.Fatalf("smscheck: %v\n%s", err, said)
		}
		var work int
		if _, err := fmt.Sscanf(string(said), "most work in a frame: %d", &work); err != nil {
			t.Fatalf("smscheck said %q: %v", said, err)
		}
		if work <= 0 || work > c.most {
			t.Errorf("%s: a frame took %d T-states, want at most %d", c.name, work, c.most)
		}
	}
}

// Tables larger together than a cartridge holds without banks, read from main and from an
// interrupt between: each read answers from its own table's bank, a wrong bank being another
// table's number (or the window's fixed-ROM nothing), and the interrupted code's bank comes back.
func TestSMS_BankedTablesAreReadFromTheirBanks(t *testing.T) {
	t.Parallel()
	got := smsProgramScreen(t, "banked", `
@banked const FIRST: [12000]u8 = #[11; 12000]
@banked const SECOND: [12000]u8 = #[22; 12000]
@banked const THIRD: [12000]u8 = #[33; 12000]
@banked const FOURTH: [12000]u8 = #[44; 12000]
@banked const FIFTH: [6000]u16 = #[55; 6000]

var frames: u16 = 0
var wrong: u16 = 0

@interrupt(vblank)
let each_frame = () -> void => {
  frames += 1
  if FOURTH[11999] != 44 { wrong += 1 }
}

let add_up<const N: i64> = (table: ref [N]u8) -> u16 => {
  var total: u16 = 0
  var i: u16 = 0
  while i < 300 {
    total += u16(table[i])
    i += 1
  }
  total
}

let main = () -> void => {
  vdp.init()
  vdp.interrupt_each_frame()
  var passes: u8 = 0
  // Long enough that interrupts land inside the sums.
  while passes < 60 {
    if FIRST[0] != 11 || SECOND[6000] != 22 || THIRD[11999] != 33 || FIFTH[5999] != 55 { wrong += 1 }
    if add_up(SECOND) != 6600 || add_up(THIRD) != 9900 { wrong += 1 }
    passes += 1
  }
  answer(wrong == 0 && frames > 5)
}`)
	if got != smsGreen {
		t.Errorf("the screen is %s, want green (%s); red is a table read from the wrong bank", got, smsGreen)
	}
}

// The example: a cartridge larger than the Z80 sees, its size and checksum as the header says, and
// a table's own colour on the screen, red until a button brings the next table's.
func TestSMS_ABankedCartridgeIsWhatItsHeaderSays(t *testing.T) {
	t.Parallel()
	z80ToolchainOrSkip(t)
	root := repoRoot(t)
	smscheck := smscheckOrSkip(t, root)
	setenv(t, "LYRA_STD", root)
	out := filepath.Join(t.TempDir(), "banks.sms")
	_, stderr, code := captureRun(t, "build", "-o", out, filepath.Join(root, "examples", "sms", "banks.lyra"))
	if code != 0 {
		t.Fatalf("build exited %d: %s", code, stderr)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 0x20000 || data[0x7FFF]&0x0F != 0xF {
		t.Fatalf("%d bytes, the header's size code %#x; want 128 KB, 0xF", len(data), data[0x7FFF]&0x0F)
	}
	var sum uint16
	for i, b := range data {
		if i < 0x7FF0 || i >= 0x8000 {
			sum += uint16(b)
		}
	}
	if binary.LittleEndian.Uint16(data[0x7FFA:]) != sum {
		t.Errorf("the header's checksum is wrong")
	}
	if got := smsScreen(t, smscheck, out, "60", "").dominant(); got != "ff0000" {
		t.Errorf("the screen is %s, want the first table's red", got)
	}
	if got := smsScreen(t, smscheck, out, "60", "1").dominant(); got != "00ff00" {
		t.Errorf("with the button held the screen is %s, want the next table's green", got)
	}
}

// A table too large for a bank says so by name.
func TestSMS_ATableLargerThanABankIsRefused(t *testing.T) {
	t.Parallel()
	z80ToolchainOrSkip(t)
	root := repoRoot(t)
	setenv(t, "LYRA_STD", root)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "lyra.toml"), []byte("target = \"sms\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "large.lyra")
	code := `
@banked const HUGE: [17000]u8 = #[1; 17000]
let main = () -> void => { unsafe { port_write(0xBE, HUGE[5]) } }
`
	if err := os.WriteFile(src, []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	_, stderr, status := captureRun(t, "build", "-o", filepath.Join(dir, "large.sms"), src)
	if status == 0 || !strings.Contains(stderr, "HUGE") || !strings.Contains(stderr, "must fit") {
		t.Errorf("exited %d: %s", status, stderr)
	}
}

// std.sms.collision is std.genesis.collision's in 16 bits, and must answer as it does: random
// shapes and maps through both, natively, each map in its console's own cell words. The counts of
// hits say the trials were not all misses.
func TestSMS_CollisionAnswersAsTheGenesisDoes(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	setenv(t, "LYRA_STD", root)
	src := filepath.Join(t.TempDir(), "agree.lyra")
	if err := os.WriteFile(src, []byte(collisionAgreement), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := captureRun(t, "run", src)
	if code != 0 {
		t.Fatalf("run exited %d: %s", code, stderr)
	}
	var missed, hits, mapMissed, mapHits int
	if _, err := fmt.Sscanf(stdout, "%d %d %d %d", &missed, &hits, &mapMissed, &mapHits); err != nil {
		t.Fatalf("it said %q: %v", stdout, err)
	}
	if missed != 0 || mapMissed != 0 {
		t.Errorf("the modules disagree: %d of the shapes and %d of the maps\n%s", missed, mapMissed, stdout)
	}
	if hits < 10000 || mapHits < 2000 {
		t.Errorf("too few hits to mean anything: %d shapes, %d maps", hits, mapHits)
	}
}

// What a program does, as the screen says: green when every answer is right. A wrong answer
// sets red itself, and a trap the runtime's red.
const smsPrelude = `
import std.sms.vdp

const GREEN: [1]u8 = #[0x0C]
const WRONG: [1]u8 = #[0x03]

let answer = (right: bool) -> void => {
  vdp.load_palette(0, if right { GREEN } else { WRONG })
  vdp.show()
  loop { vdp.wait_vblank() }
}
`

func smsProgramScreen(t *testing.T, name, source string) string {
	t.Helper()
	z80ToolchainOrSkip(t)
	root := repoRoot(t)
	smscheck := smscheckOrSkip(t, root)
	setenv(t, "LYRA_STD", root)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "lyra.toml"), []byte("target = \"sms\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, name+".lyra")
	if err := os.WriteFile(src, []byte(smsPrelude+source), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, name+".sms")
	_, stderr, code := captureRun(t, "build", "-o", out, src)
	if code != 0 {
		t.Fatalf("build exited %d: %s", code, stderr)
	}
	// Clearing video memory takes the first half second.
	return smsScreen(t, smscheck, out, "400", "").dominant()
}

const (
	smsGreen = "00ff00"
	smsRed   = "ff0000"
)

func TestSMS_LanguageFeatures(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, source string }{
		{"integers", `
let double = (x: u16) -> u16 => x * 2
let main = () -> void => {
  vdp.init()
  var a: i16 = -300
  a = a / 7
  let b: u16 = double(1234)
  let c: u8 = u8(b >> 8)
  let d: i32 = i32(a) * 100000
  let e: u32 = 70000
  answer(a == -42 && b == 2468 && c == 9 && d == -4200000 && e / 7 == 10000 && e % 11 == 7)
}`},
		{"structs", `
struct Point { x: i16, y: i16 }
struct Line { from: Point, to: Point, width: u8 }
let moved = (p: Point, by: i16) -> Point => Point { x: p.x + by, y: p.y - by }
let span = (l: ref Line) -> i16 => (l.to.x - l.from.x) + (l.to.y - l.from.y)
let main = () -> void => {
  vdp.init()
  let start = Point { x: 10, y: 20 }
  let line = Line { from: start, to: moved(moved(start, 5), 3), width: 2 }
  answer(line.to.x == 18 && line.to.y == 12 && span(line) == 0 && line.width == 2)
}`},
		{"variants", `
data Shape = Dot | Square(u8) | Box(u8, u8)
let area = (s: Shape) -> u16 => match s {
  Dot => 1,
  Square(n) => u16(n) * u16(n),
  Box(w, h) => u16(w) * u16(h),
}
let main = () -> void => {
  vdp.init()
  let a: Maybe<u16> = Some(area(Box(6, 7)))
  answer(area(Dot) == 1 && area(Square(9)) == 81 && a.unwrap_or(0) == 42)
}`},
		{"tables", `
const STEPS: [5]u8 = #[3, 1, 4, 1, 5]
var counts: [4]u16 = #[0; 4]
let sum<const N: i64> = (xs: ref [N]u8) -> u16 => {
  var total: u16 = 0
  for x in xs { total += u16(x) }
  total
}
let main = () -> void => {
  vdp.init()
  var i: u8 = 0
  while i < 4 {
    counts[i] = u16(i) * 10
    i += 1
  }
  var local: [40]u16 = #[7; 40]
  local[39] = counts[3]
  answer(sum(STEPS) == 14 && local[39] == 30 && local[0] == 7 && STEPS[2] == 4)
}`},
		{"functions", `
let apply = (f: (u16) -> u16, x: u16) -> u16 => f(x)
let plus_one = (x: u16) -> u16 => x + 1
let fib = (n: u16) -> u16 => if n < 2 { n } else { fib(n - 1) + fib(n - 2) }
trait Scored { score: (Self) -> u16 }
struct Coin { value: u16 }
impl Scored for Coin { score = (self) => self.value * 2 }
let main = () -> void => {
  vdp.init()
  answer(apply(plus_one, 41) == 42 && fib(10) == 55 && Coin { value: 4 }.score() == 8)
}`},
		{"interrupt", `
var frames: u16 = 0
@interrupt(vblank)
let each_frame = () -> void => { frames += 1 }
let main = () -> void => {
  vdp.init()
  vdp.interrupt_each_frame()
  while frames < 5 {}
  answer(true)
}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := smsProgramScreen(t, c.name, c.source); got != smsGreen {
				t.Errorf("the screen is %s, want green (%s); red is a wrong answer or a trap", got, smsGreen)
			}
		})
	}
}

func TestSMS_ATrapTurnsTheBackdropRed(t *testing.T) {
	t.Parallel()
	got := smsProgramScreen(t, "trap", `
let bump = (x: u8) -> u8 => x + 200
let main = () -> void => {
  vdp.init()
  vdp.show()
  let a = bump(100)
  answer(a == 44)
}`)
	if got != smsRed {
		t.Errorf("the screen is %s, want the runtime's red (%s)", got, smsRed)
	}
}

const collisionAgreement = `// Random shapes and maps through std.genesis.collision and std.sms.collision, natively: any
// disagreement is printed. Map words are each console's own spelling of the same cell.
import std.genesis.collision as genesis
import std.sms.collision as sms

var seed: u32 = 12345
let next = () -> u32 => {
  seed = seed.wrapping_mul(1103515245).wrapping_add(12345)
  (seed >> 16) & 0x7FFF
}
let span = (n: i64) -> i16 => i16(i64(next()) % n)

const WIDTH: u16 = 8
var g_cells: [64]u16 = #[0; 64]
var s_cells: [64]u16 = #[0; 64]
const STARTS: [3]u16 = #[0, 1, 2]

let main = () -> void => {
  var bad = 0
  var hits = 0
  var map_hits = 0
  for trial in 0..<200000 {
    let ae = next() % 2 == 0
    let be = next() % 2 == 0
    let (ax, ay, ox, oy, aw, ah) = (span(60) - 30, span(60) - 30, span(20), span(20), span(40), span(40))
    let (bx, by, px, py, bw, bh) = (span(60) - 30, span(60) - 30, span(20), span(20), span(40), span(40))
    let ga = genesis.Shape { ellipse: ae, x: ox, y: oy, width: aw, height: ah }
    let gb = genesis.Shape { ellipse: be, x: px, y: py, width: bw, height: bh }
    let sa = sms.Shape { ellipse: ae, x: ox, y: oy, width: aw, height: ah }
    let sb = sms.Shape { ellipse: be, x: px, y: py, width: bw, height: bh }
    if ga.overlaps(ax, ay, gb, bx, by) { hits += 1 }
    if ga.overlaps(ax, ay, gb, bx, by) != sa.overlaps(ax, ay, sb, bx, by) {
      bad += 1
      if bad < 5 { println("overlaps trial ${trial}: ${ae} ${ax},${ay} +${ox},${oy} ${aw}x${ah} / ${be} ${bx},${by} +${px},${py} ${bw}x${bh}") }
    }
  }
  
  var map_bad = 0
  for trial in 0..<40000 {
    for i in 0..<64 {
      let kind = next() % 4
      let across = next() % 2 == 1
      let down = next() % 2 == 1
      let tile: u32 = if kind == 0 { 9 } else if kind == 1 { 10 } else { 0 }
      let g_flags: u32 = if across { 0x800 } else { 0 }
      let g_down: u32 = if down { 0x1000 } else { 0 }
      let s_flags: u32 = if across { 0x200 } else { 0 }
      let s_down: u32 = if down { 0x400 } else { 0 }
      g_cells[i] = u16(tile | g_flags | g_down)
      s_cells[i] = u16(tile | s_flags | s_down)
    }
    let ell = next() % 2 == 0
    let (sx, sy, sw, sh) = (span(4), span(4), span(4) + 1, span(4) + 1)
    let (tx, ty, tw, th) = (span(4), span(4), span(4) + 1, span(4) + 1)
    let te = next() % 2 == 0
    let one = next() % 2 == 0
    let g_shapes: [2]genesis.Collider = #[
      genesis.Collider { shape: genesis.Shape { ellipse: te, x: tx, y: ty, width: tw, height: th }, layers: 1, mask: 0, one_way: one },
      genesis.Collider { shape: genesis.Shape { x: 0, y: 0, width: 8, height: 8 }, layers: 2, mask: 0 }
    ]
    let s_shapes: [2]sms.Collider = #[
      sms.Collider { shape: sms.Shape { ellipse: te, x: tx, y: ty, width: tw, height: th }, layers: 1, mask: 0, one_way: one },
      sms.Collider { shape: sms.Shape { x: 0, y: 0, width: 8, height: 8 }, layers: 2, mask: 0 }
    ]
    let (ow, oh) = (span(16) + 1, span(16) + 1)
    let mask = u16(next() % 4)
    let g_me = genesis.Collider { shape: genesis.Shape { ellipse: ell, x: sx, y: sy, width: ow, height: oh }, layers: 0, mask: mask }
    let s_me = sms.Collider { shape: sms.Shape { ellipse: ell, x: sx, y: sy, width: ow, height: oh }, layers: 0, mask: mask }
    let x = span(90) - 10
    let y = span(90) - 10
    let from_y = y - span(12) + 4
    let g_hit = g_me.hits_map_moving(x, y, from_y, g_cells, WIDTH, 9, STARTS, g_shapes)
    let s_hit = s_me.hits_map_moving(x, y, from_y, s_cells, WIDTH, 9, STARTS, s_shapes)
    let g_any = g_me.hits_any_moving(x, y, from_y, g_shapes)
    let s_any = s_me.hits_any_moving(x, y, from_y, s_shapes)
    if g_hit { map_hits += 1 }
    if g_hit != s_hit || g_any != s_any {
      map_bad += 1
      if map_bad < 6 { println("map trial ${trial}: at ${x},${y} from ${from_y} ell ${ell} +${sx},${sy} ${ow}x${oh} mask ${mask} tile ${te} +${tx},${ty} ${tw}x${th} one ${one}: g ${g_hit}/${g_any} s ${s_hit}/${s_any}") }
    }
  }
  println("${bad} ${hits} ${map_bad} ${map_hits}")
}
`
