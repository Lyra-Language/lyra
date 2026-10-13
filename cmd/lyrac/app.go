package main

// An iOS app: what a program importing SDL3 becomes for `--target ios` or `ios-simulator`
// (a program that does not stays a command-line one, run by `simctl spawn`).
//
// **Why an app**: SDL starts on iOS only inside UIKit's application, which needs a bundle —
// a directory `<name>.app` holding the program and an `Info.plist` — installed and launched
// as an app is (`simctl install`, `simctl launch`). The program's entry is emitted as
// `lyra_main` and `runtime/ios/main.c` is linked beside it: its `main` hands `lyra_main` to
// `SDL_RunApp`, and exits with its status when it returns.
//
// **The Info.plist** says what iOS must be told: the executable, an identifier
// (`--bundle-id`, else `dev.lyra.<name>`), iPhone and iPad, iOS 17, landscape — and
// `UILaunchScreen`, without which iOS runs the app in a compatibility mode at a lower
// resolution than the screen's (SDL's docs/README-ios.md). The Simulator's app is signed ad
// hoc; a device's needs a provisioning profile.

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/Lyra-Language/lyra/pkg/driver"
	"github.com/Lyra-Language/lyra/pkg/modules"
	"github.com/Lyra-Language/lyra/pkg/target"
)

// appEntry is the C symbol an app's program is emitted under: runtime/ios/main.c calls it.
const appEntry = "lyra_main"

// isApp reports whether res, built for p, is an app: a program over SDL3 for iOS.
func isApp(p target.Platform, res *driver.Result) bool {
	return p.OS == "ios" && slices.Contains(res.Links, "SDL3")
}

// bundlePaths are the app's directory for the executable path exe — `<exe>.app`, or exe
// itself when `-o` already names one — and the program inside it.
func bundlePaths(exe string) (bundle, program string) {
	bundle = exe
	if !strings.HasSuffix(bundle, ".app") {
		bundle += ".app"
	}
	return bundle, filepath.Join(bundle, strings.TrimSuffix(filepath.Base(bundle), ".app"))
}

// bundleID is the app's identifier: `--bundle-id`, else `dev.lyra.<name>` with the name
// reduced to what an identifier may hold (letters, digits, hyphens).
func bundleID(o buildOptions, bundle string) string {
	if o.bundleID != "" {
		return o.bundleID
	}
	name := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' {
			return r
		}
		return '-'
	}, strings.TrimSuffix(filepath.Base(bundle), ".app"))
	return "dev.lyra." + name
}

// buildApp compiles llFile with libs into the app at bundle for p: the program, the start-up
// shim, the Info.plist, the signature.
func buildApp(p target.Platform, cc, opt, llFile string, libs []string, bundle, id string) error {
	shim := filepath.Join(modules.StdRoot(), "runtime", "ios", "main.c")
	if _, err := os.Stat(shim); err != nil {
		return fmt.Errorf("no iOS start-up at %s: %v", shim, err)
	}
	flags, err := platformFlags(p)
	if err != nil {
		return err
	}
	shimObj := strings.TrimSuffix(llFile, ".ll") + "-start.o"
	compile := exec.Command(cc, append(append([]string{"-O2"}, flags...), "-c", shim, "-o", shimObj)...)
	if out, err := compile.CombinedOutput(); err != nil {
		return fmt.Errorf("compiling the iOS start-up failed: %v\n%s", err, out)
	}
	defer os.Remove(shimObj)
	_, program := bundlePaths(bundle)
	if err := os.RemoveAll(bundle); err != nil {
		return err
	}
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		return err
	}
	if err := crossCompile(p, cc, opt, llFile, append([]string{shimObj}, libs...), program); err != nil {
		return err
	}
	name := filepath.Base(program)
	if err := os.WriteFile(filepath.Join(bundle, "Info.plist"), []byte(infoPlist(p, name, id)), 0o644); err != nil {
		return err
	}
	return signApp(p, bundle)
}

// infoPlist is the app's Info.plist: what iOS reads before it runs a line of it.
func infoPlist(p target.Platform, name, id string) string {
	platform := "iPhoneOS"
	if p.SDK == "iphonesimulator" {
		platform = "iPhoneSimulator"
	}
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleDevelopmentRegion</key><string>en</string>
	<key>CFBundleExecutable</key><string>` + name + `</string>
	<key>CFBundleIdentifier</key><string>` + id + `</string>
	<key>CFBundleInfoDictionaryVersion</key><string>6.0</string>
	<key>CFBundleName</key><string>` + name + `</string>
	<key>CFBundleDisplayName</key><string>` + name + `</string>
	<key>CFBundlePackageType</key><string>APPL</string>
	<key>CFBundleShortVersionString</key><string>1.0</string>
	<key>CFBundleVersion</key><string>1</string>
	<key>CFBundleSupportedPlatforms</key><array><string>` + platform + `</string></array>
	<key>MinimumOSVersion</key><string>` + iosMinimum(p) + `</string>
	<key>UIDeviceFamily</key><array><integer>1</integer><integer>2</integer></array>
	<key>UILaunchScreen</key><dict/>
	<key>UIApplicationSupportsIndirectInputEvents</key><true/>
	<key>UIRequiresFullScreen</key><true/>
	<key>UIStatusBarHidden</key><true/>
	<key>UISupportedInterfaceOrientations</key>
	<array><string>UIInterfaceOrientationLandscapeLeft</string><string>UIInterfaceOrientationLandscapeRight</string></array>
	<key>UISupportedInterfaceOrientations~ipad</key>
	<array><string>UIInterfaceOrientationLandscapeLeft</string><string>UIInterfaceOrientationLandscapeRight</string></array>
</dict>
</plist>
`
}

// iosMinimum is the oldest iOS the platform's triple names (`arm64-apple-ios17.0…` → 17.0).
func iosMinimum(p target.Platform) string {
	_, rest, _ := strings.Cut(p.Triple, "-ios")
	version, _, _ := strings.Cut(rest, "-")
	if version == "" {
		return "17.0"
	}
	return version
}

// signApp signs the app: ad hoc for the Simulator, which needs no more.
func signApp(p target.Platform, bundle string) error {
	out, err := exec.Command("codesign", "--force", "--sign", "-", "--timestamp=none", bundle).CombinedOutput()
	if err != nil {
		return fmt.Errorf("signing %s failed: %v\n%s", bundle, err, out)
	}
	return nil
}

// stopApp, when a run has launched an app, terminates it: an interrupted `lyrac run` (Vega's
// Stop) ends the app as it would end a program run here, which the interrupt reaches by
// itself.
var stopApp func()

// launchedID is the identifier of the app a run launched, whose launcher's output appOutput
// reads; "" for none.
var launchedID string

// appOutput passes a launched app's output on to w but for two lines that are the launch's,
// not the program's: `simctl launch`'s own first line (`<bundle id>: <pid>`), and the
// `lyra-exit: N` the start-up says last (runtime/ios/main.c), which is the status — `simctl`
// answers 0 whatever the app did. A line is passed on whole, when it ends.
type appOutput struct {
	w        io.Writer
	id       string
	pending  []byte
	status   int // -1 until the app has said
	launched bool
}

func newAppOutput(w io.Writer, id string) *appOutput {
	return &appOutput{w: w, id: id, status: -1}
}

func (a *appOutput) Write(p []byte) (int, error) {
	a.pending = append(a.pending, p...)
	for {
		i := bytes.IndexByte(a.pending, '\n')
		if i < 0 {
			return len(p), nil
		}
		line := a.pending[:i+1]
		a.pending = a.pending[i+1:]
		text := strings.TrimRight(string(line), "\r\n")
		if code, ok := strings.CutPrefix(text, "lyra-exit: "); ok {
			if n, err := strconv.Atoi(code); err == nil {
				a.status = n
				continue
			}
		}
		if !a.launched && strings.HasPrefix(text, a.id+": ") {
			a.launched = true
			continue
		}
		if _, err := a.w.Write(line); err != nil {
			return len(p), err
		}
	}
}

// flush passes on what is left of a last line with no end.
func (a *appOutput) flush() {
	if len(a.pending) > 0 {
		_, _ = a.w.Write(a.pending)
		a.pending = nil
	}
}

// appRunCommand installs the app at bundle in the Simulator and returns the command that
// launches it with args, its output on lyrac's: `simctl launch --console-pty` waits for the
// app to end. lyrac's `SDL_` variables and `TZ` reach the app (as `SIMCTL_CHILD_…`, the only
// way the Simulator passes one).
func appRunCommand(p target.Platform, bundle, id string, args []string) (*exec.Cmd, error) {
	if p.Runner != target.Simulator {
		return nil, fmt.Errorf("an app for %s is built here, not run (devices come next)", p.Name)
	}
	device, err := simulatorDevice()
	if err != nil {
		return nil, err
	}
	if out, err := exec.Command("xcrun", "simctl", "install", device, bundle).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("installing %s in the iOS Simulator failed: %v\n%s", bundle, err, out)
	}
	launch := append([]string{"simctl", "launch", "--console-pty", "--terminate-running-process", device, id}, args...)
	cmd := exec.Command("xcrun", launch...)
	cmd.Env = os.Environ()
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "SDL_") || strings.HasPrefix(kv, "TZ=") {
			cmd.Env = append(cmd.Env, "SIMCTL_CHILD_"+kv)
		}
	}
	stopApp = func() { _ = exec.Command("xcrun", "simctl", "terminate", device, id).Run() }
	launchedID = id
	return cmd, nil
}
