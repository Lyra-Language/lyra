package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// **`std.env`** (09/22) — `getenv`, and the distinction POSIX makes that most wrappers
// throw away: a variable that is **set and empty** is not the same as one that is unset.
// `TZ=` means something to the tools that read it; `Some("")` and `None` keep the two
// sayable, and a wrapper returning `""` for both would not.
func TestExample_EnvLookup(t *testing.T) {
	root := repoRoot(t)
	t.Setenv("LYRA_STD", root)
	probe := filepath.Join(t.TempDir(), "probe.lyra")
	if err := os.WriteFile(probe, []byte(`module main
import std.collections.{ parse_args }
import std.env.{ lookup }

let main = () -> u8 => {
  for name in parse_args([]).positional {
    match lookup(name) {
      Some(value) => println("set [${value}]"),
      None => println("unset"),
    }
  }
  0
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "probe")
	if _, stderr, code := captureRun(t, "build", "-o", bin, probe); code != 0 {
		t.Fatalf("building the probe exited %d\nstderr: %s", code, stderr)
	}
	cmd := exec.Command(bin, "LYRA_TEST_SET", "LYRA_TEST_EMPTY", "LYRA_TEST_MISSING")
	cmd.Env = append(os.Environ(), "LYRA_TEST_SET=a value", "LYRA_TEST_EMPTY=")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("probing failed: %v", err)
	}
	want := "set [a value]\nset []\nunset"
	if got := strings.TrimSpace(string(out)); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// **`TZDIR` decides where the zones are read from**, which is what makes a zone-dependent
// test pinnable: a country changing its rules changes the system database under every
// test that reads it, and pointing `TZDIR` at a fixed copy is the escape.
//
// The fixture here is deliberately a *lie* — one zone's file under another's name — so a
// pass cannot come from the loader quietly reading the system copy instead.
func TestExample_TZDIRRedirectsTheZoneDatabase(t *testing.T) {
	root := repoRoot(t)
	t.Setenv("LYRA_STD", root)
	source := "/usr/share/zoneinfo/Asia/Kathmandu"
	if _, err := os.Stat(source); err != nil {
		t.Skip("no system zone database to build a fixture from")
	}
	probe := filepath.Join(t.TempDir(), "probe.lyra")
	if err := os.WriteFile(probe, []byte(`module main
import std.temporal.{ load_time_zone, offset_at }

let main = () -> u8 => {
  match load_time_zone("America/New_York") {
    Some(zone) => println("${zone.offset_at(1773000000)}"),
    None => println("no zone"),
  }
  0
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "probe")
	if _, stderr, code := captureRun(t, "build", "-o", bin, probe); code != 0 {
		t.Fatalf("building the probe exited %d\nstderr: %s", code, stderr)
	}

	fixture := t.TempDir()
	if err := os.MkdirAll(filepath.Join(fixture, "America"), 0o755); err != nil {
		t.Fatal(err)
	}
	kathmandu, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture, "America", "New_York"), kathmandu, 0o644); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct{ name, tzdir, want string }{
		// Kathmandu's rules under New York's name: +05:45, which New York has never been.
		{"TZDIR is used", fixture, "20700"},
		// Set and empty says nothing: "look in the directory called empty-string" is not
		// a thing anybody means, so the usual place is used.
		{"an empty TZDIR is not a directory", "", "-14400"},
		{"a TZDIR with no zones finds none", filepath.Join(fixture, "nothing"), "no zone"},
	} {
		t.Run(c.name, func(t *testing.T) {
			cmd := exec.Command(bin)
			cmd.Env = append(os.Environ(), "TZDIR="+c.tzdir)
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("probing failed: %v", err)
			}
			if got := strings.TrimSpace(string(out)); got != c.want {
				t.Errorf("with TZDIR=%q got %q, want %q", c.tzdir, got, c.want)
			}
		})
	}
}
