package main

// Building for a platform other than the machine's (`--target`, pkg/target's platforms).
//
// The machine's own build is one step — `clang -O2 prog.ll libs -o prog` — as it has always
// been. Any other platform is two: the IR compiled to an object by clang for the platform's
// triple, then linked by the platform's own driver — Xcode's clang with the platform's SDK
// as the sysroot (iOS, the Simulator), or `zig cc`, which carries glibc for any Linux, so
// Linux needs no sysroot here at all.
//
// **The triple rides the command line, not the IR.** clang normalises a module's triple
// (`ios17.0` becomes `ios17.0.0`, a bare `macosx` gains the SDK's version) and warns on every
// difference, so the IR names none and `--target` says it once.
//
// Running is the platform's too (`runCommand`): here, in a booted iOS Simulator
// (`xcrun simctl spawn`), or in a Linux container (`docker run`) — the Genesis's emulator is
// the same idea for a console.

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Lyra-Language/lyra/pkg/modules"
	"github.com/Lyra-Language/lyra/pkg/target"
)

// glibcVersion is the oldest glibc a Linux program built here runs on: Debian 12's, which
// is also the image `lyrac run` runs Linux programs in.
const glibcVersion = "2.36"

// platformOf is the platform o builds for: `--target`'s, else the machine's.
func platformOf(o buildOptions) (target.Platform, error) {
	if o.targetName != "" {
		return target.PlatformNamed(o.targetName)
	}
	return target.Machine()
}

// crossCompile compiles llFile to an object for p and links it into exe with libs: the
// two steps of any platform but the machine's. The error carries the failing tool's output.
func crossCompile(p target.Platform, cc, opt, llFile string, libs []string, exe string) error {
	flags, err := platformFlags(p)
	if err != nil {
		return err
	}
	obj := strings.TrimSuffix(llFile, ".ll") + ".o"
	compile := exec.Command(cc, append(append([]string{opt}, flags...), "-c", llFile, "-o", obj)...)
	if out, err := compile.CombinedOutput(); err != nil {
		return fmt.Errorf("%s failed to compile the emitted IR for %s: %v\n%s", cc, p.Name, err, out)
	}
	defer os.Remove(obj)
	var link *exec.Cmd
	switch p.Linker {
	case target.AppleClang:
		link = exec.Command(cc, append(append(append(flags, obj), libs...), "-o", exe)...)
	case target.ZigCC:
		zig, err := findZig()
		if err != nil {
			return err
		}
		args := []string{"cc", "-target", zigTarget(p), obj}
		link = exec.Command(zig, append(append(args, libs...), "-o", exe)...)
	default:
		return fmt.Errorf("%s has no linker lyrac knows", p.Name)
	}
	if out, err := link.CombinedOutput(); err != nil {
		return fmt.Errorf("linking for %s failed: %v\n%s", p.Name, err, out)
	}
	return nil
}

// platformFlags are clang's flags for compiling (and, on Apple platforms, linking) for p:
// its triple, and an Apple platform's SDK as the sysroot.
func platformFlags(p target.Platform) ([]string, error) {
	flags := []string{"--target=" + p.Triple}
	if p.SDK == "" {
		return flags, nil
	}
	sdk, err := sdkPath(p.SDK)
	if err != nil {
		return nil, err
	}
	return append(flags, "-isysroot", sdk), nil
}

// sdkPath is where Xcode keeps the SDK named sdk (`iphonesimulator`), or why it cannot say.
// A variable so the tests can skip where there is no Xcode.
var sdkPath = func(sdk string) (string, error) {
	out, err := exec.Command("xcrun", "--sdk", sdk, "--show-sdk-path").Output()
	if err != nil {
		return "", fmt.Errorf("no %s SDK: building for it needs Xcode (xcrun --sdk %s --show-sdk-path failed)", sdk, sdk)
	}
	return strings.TrimSpace(string(out)), nil
}

// findZig is `$LYRA_ZIG`, else `zig` on the PATH: what links a Linux program built here.
func findZig() (string, error) {
	zig := os.Getenv("LYRA_ZIG")
	if zig == "" {
		zig = "zig"
	}
	path, err := exec.LookPath(zig)
	if err != nil {
		return "", fmt.Errorf("linking for Linux needs zig (`brew install zig`, or set LYRA_ZIG): %v", err)
	}
	return path, nil
}

// zigTarget is p as `zig cc -target` spells it: `aarch64-linux-gnu.2.36`.
func zigTarget(p target.Platform) string {
	arch := map[string]string{"arm64": "aarch64", "x86_64": "x86_64"}[p.Arch]
	return arch + "-linux-gnu." + glibcVersion
}

// platformLibDir is the install's `lib/<platform>/` when it has one, else "": where the
// bindings' C halves and the libraries they wrap are built for a platform other than the
// machine's (whose own are in `lib/`, as `installLibDir` finds them).
func platformLibDir(p target.Platform) string {
	root := modules.StdRoot()
	if root == "" {
		return ""
	}
	dir := filepath.Join(root, "lib", p.Name)
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return dir
	}
	return ""
}

// platformPkgFlags is pkg-config's `--static --libs` for pkg on p, from the `.pc` files in the
// platform's `lib/pkgconfig/` alone — never this machine's, which describe libraries built for
// it — or why there is none. A variable so the link tests answer for it.
var platformPkgFlags = func(p target.Platform, pkg string) ([]string, error) {
	dir := platformLibDir(p)
	if dir == "" {
		return nil, fmt.Errorf("no libraries are built for %s (%s), so @link's pkg: %q is linked by name alone%s",
			p.Name, filepath.Join("lib", p.Name), pkg, buildHint(p, pkg))
	}
	cmd := exec.Command("pkg-config", "--static", "--libs", pkg)
	cmd.Env = append(os.Environ(), "PKG_CONFIG_LIBDIR="+filepath.Join(dir, "lib", "pkgconfig"), "PKG_CONFIG_PATH=")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s has no %q library in %s%s", p.Name, pkg, dir, buildHint(p, pkg))
	}
	return strings.Fields(string(out)), nil
}

// buildHint says how to build pkg for p, where lyra knows: SDL3 for iOS.
func buildHint(p target.Platform, pkg string) string {
	if pkg == "sdl3" && p.OS == "ios" {
		return "; tools/sdl3-ios.sh builds SDL3 for iOS"
	}
	return ""
}

// runCommand is the command running exe with args on p from here: exe itself on the
// machine, in an iOS Simulator, or in a Linux container. A platform that cannot be run
// from here — a device, whose program is installed, not run — is an error saying so.
func runCommand(p target.Platform, exe string, args []string) (*exec.Cmd, error) {
	switch p.Runner {
	case target.Local:
		return exec.Command(exe, args...), nil
	case target.Simulator:
		device, err := simulatorDevice()
		if err != nil {
			return nil, err
		}
		abs, err := filepath.Abs(exe)
		if err != nil {
			return nil, err
		}
		return exec.Command("xcrun", append([]string{"simctl", "spawn", device, abs}, args...)...), nil
	case target.Docker:
		abs, err := filepath.Abs(exe)
		if err != nil {
			return nil, err
		}
		docker := []string{"run", "--rm", "-i", "--platform", "linux/" + dockerArch(p),
			"-v", filepath.Dir(abs) + ":/lyra", linuxImage(), "/lyra/" + filepath.Base(abs)}
		return exec.Command("docker", append(docker, args...)...), nil
	default:
		return nil, fmt.Errorf("a program for %s is built here, not run: install it through Xcode (app bundles come with iOS support)", p.Name)
	}
}

// linuxImage is the container a Linux program runs in: `$LYRA_LINUX_IMAGE`, else Debian 12,
// whose glibc is the one programs are built against (glibcVersion).
func linuxImage() string {
	if image := os.Getenv("LYRA_LINUX_IMAGE"); image != "" {
		return image
	}
	return "debian:bookworm-slim"
}

// dockerArch is p's processor as Docker's `--platform` spells it.
func dockerArch(p target.Platform) string {
	if p.Arch == "x86_64" {
		return "amd64"
	}
	return p.Arch
}

// simulatorDevice is the iOS Simulator programs run in: `$LYRA_SIMULATOR` (a name or
// UDID), else the first one booted, else the first iPhone available, booted for the
// purpose and left booted, since booting takes seconds and the next run wants it too.
func simulatorDevice() (string, error) {
	if device := os.Getenv("LYRA_SIMULATOR"); device != "" {
		return device, bootSimulator(device)
	}
	devices, err := simulators()
	if err != nil {
		return "", err
	}
	for _, d := range devices {
		if d.State == "Booted" {
			return d.UDID, nil
		}
	}
	for _, d := range devices {
		if strings.HasPrefix(d.Name, "iPhone") {
			return d.UDID, bootSimulator(d.UDID)
		}
	}
	return "", fmt.Errorf("no iOS Simulator device is available (Xcode > Settings > Components adds a runtime)")
}

// simulator is a device as `simctl list -j` describes it.
type simulator struct {
	Name  string `json:"name"`
	UDID  string `json:"udid"`
	State string `json:"state"`
}

// simulators are the available iOS Simulator devices, in simctl's order.
func simulators() ([]simulator, error) {
	out, err := exec.Command("xcrun", "simctl", "list", "devices", "available", "-j").Output()
	if err != nil {
		return nil, fmt.Errorf("running in the iOS Simulator needs Xcode (xcrun simctl failed: %v)", err)
	}
	var listing struct {
		Devices map[string][]simulator `json:"devices"`
	}
	if err := json.Unmarshal(out, &listing); err != nil {
		return nil, fmt.Errorf("xcrun simctl's device list: %v", err)
	}
	// Newest runtime first, so a choice is the same every time.
	runtimes := make([]string, 0, len(listing.Devices))
	for runtime := range listing.Devices {
		if strings.Contains(runtime, "iOS") {
			runtimes = append(runtimes, runtime)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(runtimes)))
	var all []simulator
	for _, runtime := range runtimes {
		all = append(all, listing.Devices[runtime]...)
	}
	return all, nil
}

// bootSimulator boots device if it is not already, and waits until it is ready.
func bootSimulator(device string) error {
	// Booting a booted device is an error simctl reports and nothing else; the wait is
	// what matters.
	_ = exec.Command("xcrun", "simctl", "boot", device).Run()
	if out, err := exec.Command("xcrun", "simctl", "bootstatus", device, "-b").CombinedOutput(); err != nil {
		return fmt.Errorf("the iOS Simulator %s did not boot: %v\n%s", device, err, out)
	}
	return nil
}
