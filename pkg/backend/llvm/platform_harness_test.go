package llvm

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/target"
)

// The behavioural suite on another platform: `LYRA_TEST_TARGET=ios-simulator` (or
// `linux-arm64`, `linux-x86_64`) emits every test program for that platform, compiles it
// with clang for its triple, links it with the platform's driver, and runs it there — in
// the booted iOS Simulator, or in one Linux container started for the run. Unset, nothing
// changes: the machine builds and runs as it always has.
//
// It is the proof the platform work owes: the whole suite on another OS's libc, where an
// OS fact the backend still takes from the machine (a struct's layout, a constant, a
// symbol's name) fails a test instead of passing here. ASan's tests skip there — the
// sanitizer runtimes are the machine's.

var platformUnderTest struct {
	once  sync.Once
	p     target.Platform
	cross bool  // a platform other than the machine's
	err   error // LYRA_TEST_TARGET names no platform
	sdk   string
	// container is the Linux container commands run in (`docker exec`), started once.
	container string
}

// testPlatform is the platform the suite builds for, and whether it is another than the
// machine's; a bad LYRA_TEST_TARGET fails the test that asks.
func testPlatform(t *testing.T) (target.Platform, bool) {
	t.Helper()
	u := &platformUnderTest
	u.once.Do(func() {
		name := os.Getenv("LYRA_TEST_TARGET")
		if name == "" {
			u.p, u.err = target.Machine()
			return
		}
		u.p, u.err = target.PlatformNamed(name)
		u.cross = u.err == nil && !u.p.IsMachine()
		if u.cross && u.p.SDK != "" {
			out, err := exec.Command("xcrun", "--sdk", u.p.SDK, "--show-sdk-path").Output()
			if err != nil {
				u.err = err
				return
			}
			u.sdk = strings.TrimSpace(string(out))
		}
	})
	if u.err != nil {
		t.Fatalf("LYRA_TEST_TARGET: %v", u.err)
	}
	return u.p, u.cross
}

// testBackend is the backend for the platform under test.
func testBackend() *Backend {
	if os.Getenv("LYRA_TEST_TARGET") == "" {
		return New()
	}
	p, err := target.PlatformNamed(os.Getenv("LYRA_TEST_TARGET"))
	if err != nil {
		return New()
	}
	return NewForPlatform(p)
}

// compileForPlatform compiles the IR at llPath (and any C in extraArgs) into binPath for
// p: clang for the triple, then the platform's linker — clang with the SDK on Apple
// platforms, `zig cc` for Linux.
func compileForPlatform(t *testing.T, clang string, p target.Platform, llPath, binPath string, extraArgs []string) ([]byte, error) {
	t.Helper()
	for _, a := range extraArgs {
		if strings.HasPrefix(a, "-fsanitize") {
			t.Skipf("the sanitizers run on the machine, not %s", p.Name)
		}
		// A library of this machine's — raylib, SDL3, libjpeg from Homebrew — is not
		// built for the platform; the bindings' platform libraries are the iOS plan's.
		if (strings.HasPrefix(a, "-l") && a != "-lm") || strings.HasPrefix(a, "-L") {
			t.Skipf("links this machine's library (%s), which is not built for %s", a, p.Name)
		}
	}
	flags := []string{"--target=" + p.Triple}
	if platformUnderTest.sdk != "" {
		flags = append(flags, "-isysroot", platformUnderTest.sdk)
	}
	obj := strings.TrimSuffix(llPath, ".ll") + ".o"
	if out, err := exec.Command(clang, append(append(flags, "-O2", "-c", llPath, "-o"), obj)...).CombinedOutput(); err != nil {
		return out, err
	}
	switch p.Linker {
	case target.ZigCC:
		zig := os.Getenv("LYRA_ZIG")
		if zig == "" {
			zig = "zig"
		}
		arch := map[string]string{"arm64": "aarch64", "x86_64": "x86_64"}[p.Arch]
		args := append([]string{"cc", "-target", arch + "-linux-gnu.2.36"}, extraArgs...)
		return exec.Command(zig, append(args, obj, "-lm", "-o", binPath)...).CombinedOutput()
	default:
		args := append(append(flags, extraArgs...), obj, "-lm", "-o", binPath)
		return exec.Command(clang, args...).CombinedOutput()
	}
}

// compileC compiles and links C (args: flags and sources) into bin for the platform under
// test — a test's own C beside a Lyra program, which must run where the program does.
func compileC(t *testing.T, clang, bin string, args ...string) ([]byte, error) {
	t.Helper()
	p, cross := testPlatform(t)
	if !cross {
		return exec.Command(clang, append(args, "-o", bin)...).CombinedOutput()
	}
	if p.Linker == target.ZigCC {
		zig := os.Getenv("LYRA_ZIG")
		if zig == "" {
			zig = "zig"
		}
		arch := map[string]string{"arm64": "aarch64", "x86_64": "x86_64"}[p.Arch]
		return exec.Command(zig, append(append([]string{"cc", "-target", arch + "-linux-gnu.2.36"}, args...), "-o", bin)...).CombinedOutput()
	}
	flags := []string{"--target=" + p.Triple}
	if platformUnderTest.sdk != "" {
		flags = append(flags, "-isysroot", platformUnderTest.sdk)
	}
	return exec.Command(clang, append(append(flags, args...), "-o", bin)...).CombinedOutput()
}

// command is exec.Command for a test program: run here, or where the platform under test
// runs it — `xcrun simctl spawn booted` (one Simulator booted beforehand), or `docker
// exec` in a container sharing this machine's temporary and cache directories at the same
// paths, so a test's file arguments mean the same there.
func command(bin string, args ...string) *exec.Cmd {
	name := os.Getenv("LYRA_TEST_TARGET")
	p, err := target.PlatformNamed(name)
	if name == "" || err != nil || p.IsMachine() {
		return exec.Command(bin, args...)
	}
	// A test program reads its fixtures relative to the test's directory (`testdata/…`),
	// so it starts there, as it does run here.
	dir, _ := os.Getwd()
	switch p.Runner {
	case target.Simulator:
		// A spawned program starts in the device's data directory, and simctl has no way
		// to say another: a shell moves it. The shell drops `DYLD_ROOT_PATH`, which a
		// simulator program cannot load without — dyld aborts — so it is put back from
		// `SIMULATOR_ROOT`, which the shell keeps.
		shell := []string{"simctl", "spawn", "booted", "/bin/sh", "-c",
			`cd "$1" && shift && DYLD_ROOT_PATH="$SIMULATOR_ROOT" exec "$@"`, "sh", dir, bin}
		return exec.Command("xcrun", append(shell, args...)...)
	case target.Docker:
		// `-e TZ` forwards the variable from docker's own environment (withEnv).
		exe := []string{"exec", "-i", "-w", dir, "-e", "TZ", linuxContainer(p), bin}
		return exec.Command("docker", append(exe, args...)...)
	default:
		return exec.Command(bin, args...)
	}
}

// withEnv runs cmd with kv (`TZ=Pacific/Kiritimati`) set, wherever it runs: the Simulator
// passes a variable to its program only as `SIMCTL_CHILD_<name>`, and Docker forwards the
// ones `command` names from its own environment.
func withEnv(cmd *exec.Cmd, kv ...string) {
	env := append(os.Environ(), kv...)
	for _, v := range kv {
		env = append(env, "SIMCTL_CHILD_"+v)
	}
	cmd.Env = env
}

var containerOnce sync.Once

// linuxContainer is the container Linux test programs run in, started on first use and
// stopped with the test binary (TestMain).
func linuxContainer(p target.Platform) string {
	containerOnce.Do(func() {
		mounts := []string{}
		cwd, _ := os.Getwd()
		for _, dir := range []string{os.TempDir(), binCacheDir(), cwd} {
			if dir == "" {
				continue
			}
			real, err := filepath.EvalSymlinks(dir)
			if err != nil {
				real = dir
			}
			mounts = append(mounts, "-v", real+":"+real)
			if real != dir {
				mounts = append(mounts, "-v", real+":"+dir)
			}
		}
		arch := "arm64"
		if p.Arch == "x86_64" {
			arch = "amd64"
		}
		args := append([]string{"run", "-d", "--rm", "--platform", "linux/" + arch}, mounts...)
		image := os.Getenv("LYRA_LINUX_IMAGE")
		if image == "" {
			image = "debian:bookworm-slim"
		}
		out, err := exec.Command("docker", append(args, image, "sleep", "infinity")...).Output()
		if err == nil {
			platformUnderTest.container = strings.TrimSpace(string(out))
		}
	})
	return platformUnderTest.container
}

// TestMain stops the Linux container a run started.
func TestMain(m *testing.M) {
	code := m.Run()
	if c := platformUnderTest.container; c != "" {
		_ = exec.Command("docker", "kill", c).Run()
	}
	os.Exit(code)
}
