package target

import (
	"runtime"
	"strings"
	"testing"
)

func TestPlatformNamed_KnowsEachAndRefusesTheRest(t *testing.T) {
	for name, want := range map[string]string{
		"ios":           "arm64-apple-ios17.0",
		"ios-simulator": "arm64-apple-ios17.0-simulator",
		"linux-arm64":   "aarch64-unknown-linux-gnu",
		"linux-x86_64":  "x86_64-unknown-linux-gnu",
	} {
		p, err := PlatformNamed(name)
		if err != nil || p.Triple != want {
			t.Errorf("%s: %+v, %v; want triple %s", name, p, err, want)
		}
	}
	if _, err := PlatformNamed("amiga"); err == nil || !strings.Contains(err.Error(), "ios-simulator") {
		t.Errorf("an unknown platform should be refused naming the known ones: %v", err)
	}
}

// `macos` and `linux` are this machine's processor's: what a person types first.
func TestPlatformNamed_MacOSAndLinuxAreThisProcessors(t *testing.T) {
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "x86_64"
	}
	for _, os := range []string{"macos", "linux"} {
		p, err := PlatformNamed(os)
		if err != nil || p.Name != os+"-"+arch || p.Arch != arch {
			t.Errorf("%s: %+v, %v", os, p, err)
		}
	}
}

// The machine is a platform of the table, and the only one built in one step.
func TestMachine_IsThisMachine(t *testing.T) {
	m, err := Machine()
	if err != nil {
		t.Skip(err)
	}
	wantOS := map[string]string{"darwin": "darwin", "linux": "linux"}[runtime.GOOS]
	if m.OS != wantOS || !m.IsMachine() || m.Runner != Local {
		t.Errorf("Machine() = %+v on %s", m, runtime.GOOS)
	}
	if named, err := PlatformNamed(m.Name); err != nil || named != m {
		t.Errorf("--target %s is not the machine as Machine() gives it: %+v", m.Name, named)
	}
	sim, _ := PlatformNamed("ios-simulator")
	if sim.IsMachine() {
		t.Error("the iOS Simulator is never the machine")
	}
}
