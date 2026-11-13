package llvm

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// `program_args()` is the prelude's `[]string` over the two builtins, and `std.io`'s
// `read_file` is ordinary Lyra over `open`/`read`/`close`. The binary is run with real
// arguments — a file this test writes, a path that does not exist, and an argument with
// a space and a non-ASCII rune — since nothing short of an actual argv exercises either.
func TestExec_ProgramArgsAndReadFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "in.txt")
	if err := os.WriteFile(path, []byte("héllo\nworld\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "missing.txt")
	src := `import std.io.{ read_file }
let main = () -> void => {
  let args = program_args()
  println(args.len())
  for i in 1..<args.len() { println(args[i]) }
  match read_file(args[1]) {
    Some(t) => { println(t.len()); print(t) },
    None => { println("none") },
  }
  match read_file(args[2]) {
    Some(_) => { println("some") },
    None => { println("none") },
  }
  println(args[3].len())
}`
	bin := preludeBinary(t, src)
	out, err := command(bin, path, missing, "two wörds").Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	want := "4\n" + path + "\n" + missing + "\ntwo wörds\n12\nhéllo\nworld\nnone\n9\n"
	if string(out) != want {
		t.Fatalf("stdout:\n%s\nwant:\n%s", out, want)
	}
}

// With no arguments there is exactly one, the program's name, and an index past the end
// traps as an array index does.
func TestExec_ProgramArgs_NoArgsAndOutOfRange(t *testing.T) {
	t.Parallel()
	if got := buildAndRun(t, `let main = () -> u8 => u8(program_arg_count())`); got != 1 {
		t.Fatalf("program_arg_count() with no arguments = %d, want 1", got)
	}
	if got := buildAndRun(t, `let main = () -> u8 => u8(program_arg(3).len())`); got != 101 {
		t.Fatalf("program_arg(3) with no arguments exited %d, want the trap's 101", got)
	}
}

// `std.collections.parse_command` parses the flags in front of a command and leaves the
// command and everything after it untouched — what a program running another program after
// its own flags (`termina --print ls -la`) or dispatching on a subcommand needs, and what
// `parse_args`, reading every `-`-argument as a flag, cannot give. The probe parses the top
// level, then a `build` subcommand's flags out of the `positional` it was left, as a
// dispatching program would.
func TestExec_ParseCommandStopsAtTheCommand(t *testing.T) {
	t.Parallel()
	src := `import std.collections.{ parse_command, ProgramArgs }
let main = () -> u8 => {
  let top = match parse_command(program_args(), ["-v", "--print"], ["-C"]) {
    Ok(a) => a,
    Err(why) => {
      println("error: ${why}")
      return 2
    },
  }
  println("top: ${show(top)}")
  if top.positional.len() > 0 && top.positional[0] == "build" {
    match parse_command(top.positional, ["--release"], ["-o"]) {
      Ok(sub) => { println("${sub.program}: ${show(sub)}") },
      Err(why) => {
        println("build: ${why}")
        return 2
      },
    }
  }
  0
}
let show = (a: ProgramArgs) -> string =>
  "switches=[${a.switches.join(",")}] C=${a.value("-C").unwrap_or("-")} o=${a.value("-o").unwrap_or("-")} rest=[${a.positional.join(" ")}]"`
	bin := preludeBinary(t, src)
	for _, c := range []struct {
		name string
		args []string
		want string
		code int
	}{
		{"no positional at all", []string{"-v", "-C", "dir"},
			"top: switches=[-v] C=dir o=- rest=[]\n", 0},
		{"nothing at all", nil,
			"top: switches=[] C=- o=- rest=[]\n", 0},
		// The command's own flags are its, however they look: `-la` is not ours.
		{"stops at the command", []string{"--print", "ls", "-la", "-v"},
			"top: switches=[--print] C=- o=- rest=[ls -la -v]\n", 0},
		// Options before a subcommand are the program's; the subcommand's after it are
		// parsed in turn from what was left, its name standing where the program's was.
		{"options before a subcommand", []string{"-v", "-C", "src", "build", "--release", "-o", "out", "main.lyra"},
			"top: switches=[-v] C=src o=- rest=[build --release -o out main.lyra]\n" +
				"build: switches=[--release] C=- o=out rest=[main.lyra]\n", 0},
		// `--` stops the parse and is not kept, so a command may begin with a dash; one
		// after the command is the command's.
		{"stops at --", []string{"-v", "--", "-C", "x", "--"},
			"top: switches=[-v] C=- o=- rest=[-C x --]\n", 0},
		{"a bare - is the command", []string{"-", "-v"},
			"top: switches=[] C=- o=- rest=[- -v]\n", 0},
		// Every flag is declared, so one that is not is an error rather than a switch:
		// the parser cannot know where the flags end without knowing them all.
		{"an unknown option", []string{"-v", "--bogus", "ls"},
			"error: unknown option --bogus\n", 2},
		{"an unknown option of the subcommand", []string{"build", "-v"},
			"top: switches=[] C=- o=- rest=[build -v]\nbuild: unknown option -v\n", 2},
		{"a valued option with no value", []string{"-C"},
			"error: -C needs a value\n", 2},
		// A valued option takes the next argument whatever it looks like.
		{"a value that looks like a flag", []string{"-C", "--", "ls"},
			"top: switches=[] C=-- o=- rest=[ls]\n", 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, err := command(bin, c.args...).Output()
			code := 0
			if exit, ok := err.(*exec.ExitError); ok {
				code = exit.ExitCode()
			} else if err != nil {
				t.Fatalf("run: %v", err)
			}
			if string(out) != c.want || code != c.code {
				t.Fatalf("args %q: exit %d, stdout:\n%s\nwant exit %d:\n%s", c.args, code, out, c.code, c.want)
			}
		})
	}
}

// `parse_args` reads the whole line, so a flag after an operand is still a flag, `--` ends
// the flags, and an undeclared flag is a switch — the behaviour `parse_command` leaves alone.
func TestExec_ParseArgsReadsTheWholeLine(t *testing.T) {
	t.Parallel()
	src := `import std.collections.{ parse_args }
let main = () -> void => {
  let a = parse_args(["-o"])
  println("switches=[${a.switches.join(",")}] o=${a.value("-o").unwrap_or("-")} rest=[${a.positional.join(" ")}]")
}`
	bin := preludeBinary(t, src)
	out, err := command(bin, "in.txt", "-v", "-o", "out", "--bogus", "-", "--", "-x").Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if want := "switches=[-v,--bogus] o=out rest=[in.txt - -x]\n"; string(out) != want {
		t.Fatalf("stdout:\n%s\nwant:\n%s", out, want)
	}
}

// `parse_strict` reads the whole line as `parse_args` does — a flag after an operand is a
// flag, in any order — and refuses what it does not know as `parse_command` does: what
// Sheliak's programs hand-rolled before it (11/13), and what neither of the others gives.
func TestExec_ParseStrictReadsTheWholeLineAndRefusesTheUnknown(t *testing.T) {
	t.Parallel()
	src := `import std.collections.{ parse_strict }
let main = () -> u8 => {
  let a = match parse_strict(program_args(), ["--save"], ["--frames", "--ppm"]) {
    Ok(a) => a,
    Err(why) => {
      println("error: ${why}")
      return 2
    },
  }
  println("switches=[${a.switches.join(",")}] frames=${a.value("--frames").unwrap_or("-")} ppm=${a.value("--ppm").unwrap_or("-")} rest=[${a.positional.join(" ")}]")
  0
}`
	bin := preludeBinary(t, src)
	for _, c := range []struct {
		name string
		args []string
		want string
		code int
	}{
		{"nothing at all", nil, "switches=[] frames=- ppm=- rest=[]\n", 0},
		// Flags before, between and after the operands, the operands kept in order.
		{"flags anywhere", []string{"a.bin", "--frames", "60", "b.bin", "--save", "--ppm", "out.ppm", "c.bin"},
			"switches=[--save] frames=60 ppm=out.ppm rest=[a.bin b.bin c.bin]\n", 0},
		{"a flag first", []string{"--frames", "40", "x.bin", "y.bin"},
			"switches=[] frames=40 ppm=- rest=[x.bin y.bin]\n", 0},
		// `--` ends the flags, unkept; everything after it is an operand.
		{"-- ends the flags", []string{"a.bin", "--", "--save", "-x"},
			"switches=[] frames=- ppm=- rest=[a.bin --save -x]\n", 0},
		{"a bare - is an operand", []string{"-", "--save"},
			"switches=[--save] frames=- ppm=- rest=[-]\n", 0},
		// A valued flag takes the next argument whatever it looks like.
		{"a value that looks like a flag", []string{"--ppm", "--save", "a.bin"},
			"switches=[] frames=- ppm=--save rest=[a.bin]\n", 0},
		{"the last value wins", []string{"--frames", "1", "--frames", "2"},
			"switches=[] frames=2 ppm=- rest=[]\n", 0},
		// Where parse_args would make these a switch and say nothing.
		{"an unknown option after an operand", []string{"a.bin", "--frams", "60"},
			"error: unknown option --frams\n", 2},
		{"an unknown short option", []string{"-v", "a.bin"},
			"error: unknown option -v\n", 2},
		{"a valued option with no value", []string{"a.bin", "--frames"},
			"error: --frames needs a value\n", 2},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, err := command(bin, c.args...).Output()
			code := 0
			if exit, ok := err.(*exec.ExitError); ok {
				code = exit.ExitCode()
			} else if err != nil {
				t.Fatalf("run: %v", err)
			}
			if string(out) != c.want || code != c.code {
				t.Fatalf("args %q: exit %d, stdout:\n%s\nwant exit %d:\n%s", c.args, code, out, c.code, c.want)
			}
		})
	}
}
