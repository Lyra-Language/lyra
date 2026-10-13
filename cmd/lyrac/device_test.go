package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A provisioning profile's plist, as `security cms -D` hands it over: what lyrac reads of one.
func TestParsePlist_ReadsAProfilesValues(t *testing.T) {
	v, err := parsePlist([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Name</key>
	<string>iOS Team Provisioning Profile: *</string>
	<key>CreationDate</key>
	<date>2026-09-23T05:25:39Z</date>
	<key>DeveloperCertificates</key>
	<array>
		<data>
		aGVs
		bG8=
		</data>
	</array>
	<key>Entitlements</key>
	<dict>
		<key>application-identifier</key>
		<string>TEAM123.*</string>
		<key>get-task-allow</key>
		<true/>
	</dict>
	<key>TeamIdentifier</key>
	<array><string>TEAM123</string></array>
	<key>Version</key>
	<integer>1</integer>
</dict>
</plist>
`))
	if err != nil {
		t.Fatal(err)
	}
	d := v.(map[string]any)
	if d["Name"] != "iOS Team Provisioning Profile: *" || d["Version"] != int64(1) {
		t.Errorf("strings and integers: %v, %v", d["Name"], d["Version"])
	}
	if got := d["CreationDate"].(time.Time); got.Year() != 2026 || got.Month() != 9 {
		t.Errorf("a date: %v", got)
	}
	if got := string(d["DeveloperCertificates"].([]any)[0].([]byte)); got != "hello" {
		t.Errorf("data, base64 across lines: %q", got)
	}
	ents := d["Entitlements"].(map[string]any)
	if ents["application-identifier"] != "TEAM123.*" || ents["get-task-allow"] != true {
		t.Errorf("a nested dict: %v", ents)
	}
	if d["TeamIdentifier"].([]any)[0] != "TEAM123" {
		t.Errorf("an array: %v", d["TeamIdentifier"])
	}
}

// A profile covers an app by its identifier exactly, or by a wildcard's prefix — and only
// its own team's.
func TestProfile_CoversItsAppsOnly(t *testing.T) {
	for _, c := range []struct {
		appID, id string
		want      bool
	}{
		{"T.*", "dev.lyra.hero", true},
		{"T.dev.lyra.*", "dev.lyra.hero", true},
		{"T.dev.lyra.hero", "dev.lyra.hero", true},
		{"T.dev.lyra.hero", "dev.lyra.wisps", false},
		{"T.com.other.*", "dev.lyra.hero", false},
	} {
		if got := (profile{appID: c.appID, team: "T"}).covers(c.id); got != c.want {
			t.Errorf("%s covers %s: %v, want %v", c.appID, c.id, got, c.want)
		}
	}
}

// A game built for a device is an app signed as a device runs one: by the developer
// identity a provisioning profile covering its identifier names, with that profile inside it
// and the entitlements narrowed to the app — which `codesign --verify --strict` accepts. Skips
// without SDL3 built for iOS, or a profile (one is Xcode's to make, for the developer's team).
func TestTarget_SignsAnAppForADevice(t *testing.T) {
	requireSimulator(t)
	build := filepath.Join(repoRoot(t), "build")
	if _, err := os.Stat(filepath.Join(build, "lib", "ios", "lib", "libSDL3.a")); err != nil {
		t.Skip("no SDL3 for iOS; tools/sdl3-ios.sh builds it")
	}
	if _, err := findProfile("dev.lyra.walker"); err != nil {
		t.Skip(err)
	}
	t.Setenv("LYRA_STD", build)
	exe := filepath.Join(t.TempDir(), "walker")
	src := filepath.Join(repoRoot(t), "examples", "game", "walker.lyra")
	if _, stderr, code := captureRun(t, "build", "--target", "ios", "-o", exe, src); code != 0 {
		t.Fatalf("build: exit %d; %s", code, stderr)
	}
	app := exe + ".app"
	if out, err := exec.Command("codesign", "--verify", "--strict", app).CombinedOutput(); err != nil {
		t.Fatalf("the app's signature does not verify: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(app, "embedded.mobileprovision")); err != nil {
		t.Errorf("no profile in the app: %v", err)
	}
	ents, err := exec.Command("codesign", "-d", "--entitlements", "-", "--xml", app).Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ents), ".dev.lyra.walker</string>") {
		t.Errorf("the entitlements do not name the app:\n%s", ents)
	}
	details, _ := exec.Command("codesign", "-dvv", app).CombinedOutput()
	if !strings.Contains(string(details), "Authority=Apple Development") {
		t.Errorf("not signed by a development identity:\n%s", details)
	}
}

// A launched app's output is the program's alone: the Simulator's and a device's launchers
// say what they did, and the start-up's last line is the status, not output.
func TestAppOutput_KeepsOnlyTheProgramsLines(t *testing.T) {
	for _, launcher := range [][]string{
		{"dev.lyra.walker: 718\n"},
		{"Launched application with dev.lyra.walker bundle identifier.\n", "Waiting for the application to terminate…\n"},
	} {
		var got strings.Builder
		out := newAppOutput(&got, "dev.lyra.walker")
		for _, line := range launcher {
			out.Write([]byte(line))
		}
		out.Write([]byte("hello from the game\nlyra-ex"))
		out.Write([]byte("it: 3\n"))
		out.flush()
		if got.String() != "hello from the game\n" || out.status != 3 {
			t.Errorf("after %q: output %q, status %d", launcher[0], got.String(), out.status)
		}
	}
}
