package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A program for another platform, run there (platform.go): the iOS Simulator and Linux in
// Docker. Each prints what only its own platform's facts get right — `dir_names` reads a
// `struct dirent` at the platform's `d_name` offset (dirent.go), so a name cut short or
// garbled is the platform's facts taken from the machine — and exits 3, which must come
// back through the runner.
const platformProgram = `import std.io.{ dir_names }

let main = () -> u8 => {
  let names = dir_names("/").unwrap_or([])
  let usr = names.filter((n) => n == "usr").len() == 1
  println("args ${program_args().len()}, /usr ${usr}, clock ${wall_clock_nanos() > 0}")
  3
}
`

const platformWant = "args 3, /usr true, clock true"

func writePlatformProgram(t *testing.T) string {
	t.Helper()
	t.Setenv("LYRA_STD", repoRoot(t))
	src := filepath.Join(t.TempDir(), "where.lyra")
	if err := os.WriteFile(src, []byte(platformProgram), 0o644); err != nil {
		t.Fatal(err)
	}
	return src
}

// runOn runs the platform program on name and checks its output and exit status.
func runOn(t *testing.T, name string) {
	t.Helper()
	src := writePlatformProgram(t)
	stdout, stderr, code := captureRun(t, "run", "--target", name, src, "--", "a", "b")
	if code != 3 {
		t.Fatalf("exit = %d, want the program's 3; stderr: %s", code, stderr)
	}
	if got := strings.TrimSpace(stdout); !strings.HasSuffix(got, platformWant) {
		t.Errorf("on %s the program printed %q; want %q", name, got, platformWant)
	}
}

func TestTarget_AnUnknownPlatformIsNamedWithTheKnownOnes(t *testing.T) {
	_, stderr, code := captureRun(t, "build", "--target", "amiga", fixture("hello.lyra"))
	if code == 0 {
		t.Fatal("an unknown platform should be refused")
	}
	if !strings.Contains(stderr, `unknown platform "amiga"`) || !strings.Contains(stderr, "ios-simulator") {
		t.Errorf("the refusal should name the platforms; got: %s", stderr)
	}
}

// A Genesis program has no OS to choose a platform for: `--target` is refused, not ignored.
func TestTarget_AProgramWithNoOSTakesNoPlatform(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "lyra.toml"), []byte(`target = "genesis"`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "rom.lyra")
	if err := os.WriteFile(src, []byte("let main = () -> void => {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := captureRun(t, "build", "--target", "ios", src)
	if code == 0 || !strings.Contains(stderr, "--target chooses a platform for a program with an OS") {
		t.Errorf("--target on a Genesis program should be refused; exit %d, stderr: %s", code, stderr)
	}
}

func TestTarget_RunsInTheIOSSimulator(t *testing.T) {
	requireSimulator(t)
	runOn(t, "ios-simulator")
}

// A device's program is built — a Mach-O for iOS, not the Mac — and not run from here.
func TestTarget_BuildsForAnIOSDevice(t *testing.T) {
	requireSimulator(t)
	src := writePlatformProgram(t)
	exe := filepath.Join(t.TempDir(), "where")
	if _, stderr, code := captureRun(t, "build", "--target", "ios", "-o", exe, src); code != 0 {
		t.Fatalf("build for ios: exit %d; %s", code, stderr)
	}
	out, err := exec.Command("otool", "-l", exe).Output()
	if err != nil {
		t.Fatalf("otool: %v", err)
	}
	// LC_BUILD_VERSION's platform 2 is iOS (1 is macOS, 7 the iOS Simulator).
	if !strings.Contains(string(out), "platform 2") || !strings.Contains(string(out), "minos 17.0") {
		t.Errorf("the program is not built for iOS 17:\n%s", out)
	}
	_, stderr, code := captureRun(t, "run", "--target", "ios", src)
	if code == 0 || !strings.Contains(stderr, "built here, not run") {
		t.Errorf("running a device's program here should be refused; exit %d, stderr: %s", code, stderr)
	}
}

func TestTarget_RunsOnLinux(t *testing.T) {
	requireLinuxCross(t)
	for _, name := range []string{"linux-arm64", "linux-x86_64"} {
		t.Run(name, func(t *testing.T) { runOn(t, name) })
	}
}

// requireSimulator skips where there is no Xcode with an iOS Simulator to run in.
func requireSimulator(t *testing.T) {
	t.Helper()
	requireCC(t)
	if runtime.GOOS != "darwin" {
		t.Skip("the iOS Simulator runs on a Mac")
	}
	if _, err := sdkPath("iphonesimulator"); err != nil {
		t.Skip(err)
	}
	if devices, err := simulators(); err != nil || len(devices) == 0 {
		t.Skip("no iOS Simulator device is available")
	}
}

// requireLinuxCross skips without zig to link a Linux program or Docker to run one.
func requireLinuxCross(t *testing.T) {
	t.Helper()
	requireCC(t)
	if _, err := findZig(); err != nil {
		t.Skip(err)
	}
	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skip("no Docker to run a Linux program in")
	}
}
