package target

import (
	"fmt"
	"runtime"
	"sort"
	"strings"
)

// Platform is where a program with an operating system runs: what `lyrac build --target`
// names. A [Target] says what kind of program it is (`host`: it has an OS, a heap, libc);
// the platform says which OS and processor it is built for, and with what. One program
// builds for each — a game for the Mac, the iOS Simulator and Linux alike.
//
// Every platform here is LP64 — 64-bit pointers, `size_t` and `long` — which the backend
// assumes (`layout.go`'s pointer size, the libc declarations); one that is not needs a
// per-platform width first (todo.md).
type Platform struct {
	// Name is how `--target` spells it.
	Name string
	// Triple is LLVM's: the IR names it, and clang compiles for it.
	Triple string
	// OS is the operating system's family as the backend's OS facts are keyed:
	// "darwin" (macOS), "ios" (a device or the Simulator) or "linux".
	OS string
	// Arch is the processor: "arm64" or "x86_64".
	Arch string
	// SDK is the Apple SDK `xcrun --sdk` finds the sysroot by ("iphoneos",
	// "iphonesimulator"); empty for the machine's own and for Linux. The oldest OS
	// release a program runs on is the triple's (`ios17.0`).
	SDK string
	// Linker is what links the program for it.
	Linker Linker
	// Runner is how `lyrac run` runs it from here.
	Runner Runner
}

// Linker is the driver that links objects for a platform.
type Linker int

const (
	// MachineCC is the machine's own C compiler, as for every build before platforms:
	// one step from IR to program.
	MachineCC Linker = iota
	// AppleClang is Xcode's clang with the platform's SDK as the sysroot.
	AppleClang
	// ZigCC is `zig cc`, which carries glibc for any Linux, so no sysroot is needed.
	ZigCC
)

// Runner is how a built program is run from the machine.
type Runner int

const (
	// Local runs it here.
	Local Runner = iota
	// Simulator runs it in a booted iOS Simulator (`xcrun simctl spawn`).
	Simulator
	// Docker runs it in a Linux container.
	Docker
	// Device installs an app on a connected iPhone or iPad and launches it (`devicectl`); a
	// command-line program cannot run there.
	Device
	// NoRunner cannot run it from here.
	NoRunner
)

// platforms are the known ones, by the name `--target` uses.
var platforms = map[string]Platform{
	"macos-arm64":  {Name: "macos-arm64", Triple: "arm64-apple-macosx", OS: "darwin", Arch: "arm64", Linker: MachineCC, Runner: Local},
	"macos-x86_64": {Name: "macos-x86_64", Triple: "x86_64-apple-macosx", OS: "darwin", Arch: "x86_64", Linker: MachineCC, Runner: Local},
	"ios": {
		Name: "ios", Triple: "arm64-apple-ios17.0", OS: "ios", Arch: "arm64",
		SDK: "iphoneos", Linker: AppleClang, Runner: Device,
	},
	"ios-simulator": {
		Name: "ios-simulator", Triple: "arm64-apple-ios17.0-simulator", OS: "ios", Arch: "arm64",
		SDK: "iphonesimulator", Linker: AppleClang, Runner: Simulator,
	},
	"linux-arm64":  {Name: "linux-arm64", Triple: "aarch64-unknown-linux-gnu", OS: "linux", Arch: "arm64", Linker: ZigCC, Runner: Docker},
	"linux-x86_64": {Name: "linux-x86_64", Triple: "x86_64-unknown-linux-gnu", OS: "linux", Arch: "x86_64", Linker: ZigCC, Runner: Docker},
}

// aliases are names a person reaches for first: the Mac and Linux of this machine's
// processor.
var aliases = map[string]func() string{
	"macos": func() string { return "macos-" + machineArch() },
	"linux": func() string { return "linux-" + machineArch() },
}

// PlatformNames lists what `--target` accepts, sorted, for a diagnostic.
func PlatformNames() []string {
	names := make([]string, 0, len(platforms)+len(aliases))
	for name := range platforms {
		names = append(names, name)
	}
	for name := range aliases {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// PlatformNamed is the platform `--target` names; an unknown name is an error listing the
// known ones.
func PlatformNamed(name string) (Platform, error) {
	if alias, ok := aliases[name]; ok {
		name = alias()
	}
	if p, ok := platforms[name]; ok {
		return asMachine(p), nil
	}
	return Platform{}, fmt.Errorf("unknown platform %q; the platforms are %s", name, strings.Join(PlatformNames(), ", "))
}

// Machine is the platform lyrac itself runs on — what a program is built for when no
// `--target` is given, and the only place `runtime.GOOS` is asked. A machine this table
// does not know is an error, never a guess: its OS facts (a `struct dirent`'s layout, an
// ioctl's number) would be wrong in silence.
func Machine() (Platform, error) {
	p, ok := platforms[machineName()]
	if !ok {
		return Platform{}, fmt.Errorf("lyrac does not know this machine's platform (%s/%s)", runtime.GOOS, runtime.GOARCH)
	}
	return asMachine(p), nil
}

// machineName is the table's name for this machine, "" for an OS it has none for.
func machineName() string {
	switch runtime.GOOS {
	case "darwin":
		return "macos-" + machineArch()
	case "linux":
		return "linux-" + machineArch()
	}
	return ""
}

// asMachine is p as this machine builds and runs it: the table's Linux rows are for
// building from a Mac (zig, Docker), but on that Linux itself a program is linked by its own
// C compiler and run in place — as named by `--target` too. Found 10/13 by the suite run on
// Linux (`asan.sh ./...`), where every `lyrac run` asked for Docker.
func asMachine(p Platform) Platform {
	if p.Name == machineName() {
		p.Linker, p.Runner = MachineCC, Local
	}
	return p
}

// IsMachine reports whether p is the machine lyrac runs on: built in one step by the
// machine's C compiler, with its own libraries and pkg-config.
func (p Platform) IsMachine() bool {
	m, err := Machine()
	return err == nil && m.Name == p.Name
}

// machineArch is the machine's processor as platforms name it.
func machineArch() string {
	if runtime.GOARCH == "amd64" {
		return "x86_64"
	}
	return runtime.GOARCH
}
