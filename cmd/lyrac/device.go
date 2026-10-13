package main

// An iOS app for a device (`--target ios`): signed so the device will run it, installed and
// launched with `devicectl`.
//
// **Signing is what a device asks and the Simulator does not.** A device runs only an app
// signed by a developer it trusts, for an identifier and a device a **provisioning profile**
// names. Xcode keeps the profiles it has made in `~/Library/Developer/Xcode/UserData/
// Provisioning Profiles/`; lyrac takes `$LYRA_IOS_PROFILE`, else the newest unexpired one
// there whose app identifier covers the app's — `TEAM.dev.lyra.hero` exactly, or a wildcard
// `TEAM.*` (Xcode's team profile) — and signs with the identity in the keychain whose
// certificate the profile carries. The app's entitlements are the profile's, narrowed to the
// app; the profile itself goes into the app as `embedded.mobileprovision`. With no profile,
// the build says how to get one (any Xcode project for the team, built once for a device,
// leaves a wildcard one).
//
// **Running** is `devicectl device install app`, then `devicectl device process launch
// --console`, which streams the app's output and waits for it to end, as `simctl launch
// --console-pty` does; the status is the app's last line, as there (app.go's appOutput).
// The device is `$LYRA_IOS_DEVICE` (a name or identifier), else the first iPhone or iPad
// connected.

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Lyra-Language/lyra/pkg/target"
)

// profile is what lyrac reads of a provisioning profile.
type profile struct {
	path         string
	name         string
	appID        string // `TEAM.dev.lyra.hero`, or a wildcard `TEAM.*`
	team         string
	created      time.Time
	expires      time.Time
	certificates []string // the SHA-1 of each developer certificate, upper-case hex
	entitlements map[string]any
}

// signForDevice signs the app at bundle, identified id, for a device: the profile and identity
// found, the entitlements written, the profile embedded.
func signForDevice(bundle, id string) error {
	prof, err := findProfile(id)
	if err != nil {
		return err
	}
	identity, err := identityFor(prof)
	if err != nil {
		return err
	}
	if err := copyFile(prof.path, filepath.Join(bundle, "embedded.mobileprovision")); err != nil {
		return err
	}
	entitlements := filepath.Join(filepath.Dir(bundle), filepath.Base(bundle)+".entitlements")
	if err := os.WriteFile(entitlements, []byte(appEntitlements(prof, id)), 0o644); err != nil {
		return err
	}
	defer os.Remove(entitlements)
	out, err := exec.Command("codesign", "--force", "--sign", identity, "--entitlements", entitlements,
		"--timestamp=none", "--generate-entitlement-der", bundle).CombinedOutput()
	if err != nil {
		return fmt.Errorf("signing %s with %s failed: %v\n%s", bundle, identity, err, out)
	}
	return nil
}

// findProfile is the provisioning profile to sign the app id with: `$LYRA_IOS_PROFILE`, else
// the best of Xcode's — unexpired, its app identifier covering id, an exact one before a
// wildcard, the newest made first.
func findProfile(id string) (profile, error) {
	if path := os.Getenv("LYRA_IOS_PROFILE"); path != "" {
		p, err := readProfile(path)
		if err != nil {
			return profile{}, fmt.Errorf("LYRA_IOS_PROFILE: %v", err)
		}
		if !p.covers(id) {
			return profile{}, fmt.Errorf("LYRA_IOS_PROFILE (%s) is for %s, not %s", p.name, p.appID, id)
		}
		return p, nil
	}
	home, _ := os.UserHomeDir()
	var found []profile
	for _, dir := range []string{
		filepath.Join(home, "Library", "Developer", "Xcode", "UserData", "Provisioning Profiles"),
		filepath.Join(home, "Library", "MobileDevice", "Provisioning Profiles"),
	} {
		paths, _ := filepath.Glob(filepath.Join(dir, "*.mobileprovision"))
		for _, path := range paths {
			if p, err := readProfile(path); err == nil && p.covers(id) && p.expires.After(time.Now()) {
				found = append(found, p)
			}
		}
	}
	if len(found) == 0 {
		return profile{}, fmt.Errorf("no provisioning profile covers %s: build any Xcode project for your team "+
			"on a device once (it leaves a wildcard profile), or set LYRA_IOS_PROFILE to one", id)
	}
	sort.SliceStable(found, func(i, j int) bool {
		if exact := found[i].appID == found[i].team+"."+id; exact != (found[j].appID == found[j].team+"."+id) {
			return exact
		}
		return found[i].created.After(found[j].created)
	})
	return found[0], nil
}

// covers reports whether the profile's app identifier covers id: exactly, or by a trailing
// `*`.
func (p profile) covers(id string) bool {
	want := p.team + "." + id
	if prefix, wild := strings.CutSuffix(p.appID, "*"); wild {
		return strings.HasPrefix(want, prefix)
	}
	return p.appID == want
}

// identityFor is the keychain's signing identity whose certificate prof carries, by its SHA-1.
func identityFor(prof profile) (string, error) {
	out, err := exec.Command("security", "find-identity", "-v", "-p", "codesigning").Output()
	if err != nil {
		return "", fmt.Errorf("could not list the keychain's signing identities: %v", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		for _, cert := range prof.certificates {
			if fields[1] == cert {
				return cert, nil
			}
		}
	}
	return "", fmt.Errorf("no signing identity in the keychain has a certificate %s (%s) names; "+
		"Xcode > Settings > Accounts makes one", prof.name, filepath.Base(prof.path))
}

// appEntitlements are the entitlements the app is signed with: the profile's, its app
// identifier narrowed from a wildcard to the app's own.
func appEntitlements(prof profile, id string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
`)
	fmt.Fprintf(&b, "\t<key>application-identifier</key><string>%s.%s</string>\n", prof.team, id)
	fmt.Fprintf(&b, "\t<key>com.apple.developer.team-identifier</key><string>%s</string>\n", prof.team)
	if allow, _ := prof.entitlements["get-task-allow"].(bool); allow {
		b.WriteString("\t<key>get-task-allow</key><true/>\n")
	}
	b.WriteString("</dict>\n</plist>\n")
	return b.String()
}

// readProfile decodes the provisioning profile at path: a plist inside a CMS signature,
// which `security cms -D` takes off.
func readProfile(path string) (profile, error) {
	out, err := exec.Command("security", "cms", "-D", "-i", path).Output()
	if err != nil {
		return profile{}, fmt.Errorf("%s is not a provisioning profile: %v", path, err)
	}
	v, err := parsePlist(out)
	if err != nil {
		return profile{}, fmt.Errorf("%s: %v", path, err)
	}
	dict, _ := v.(map[string]any)
	p := profile{path: path, entitlements: map[string]any{}}
	p.name, _ = dict["Name"].(string)
	if ents, ok := dict["Entitlements"].(map[string]any); ok {
		p.entitlements = ents
		p.appID, _ = ents["application-identifier"].(string)
	}
	if teams, ok := dict["TeamIdentifier"].([]any); ok && len(teams) > 0 {
		p.team, _ = teams[0].(string)
	}
	p.created, _ = dict["CreationDate"].(time.Time)
	p.expires, _ = dict["ExpirationDate"].(time.Time)
	if certs, ok := dict["DeveloperCertificates"].([]any); ok {
		for _, c := range certs {
			if der, ok := c.([]byte); ok {
				sum := sha1.Sum(der)
				p.certificates = append(p.certificates, strings.ToUpper(hex.EncodeToString(sum[:])))
			}
		}
	}
	if p.appID == "" || p.team == "" {
		return profile{}, fmt.Errorf("%s names no app identifier or team", path)
	}
	return p, nil
}

// parsePlist reads an XML property list into Go values: a dict a map, an array a slice,
// strings, integers (int64), reals (float64), booleans, dates (time.Time) and data ([]byte).
func parsePlist(data []byte) (any, error) {
	d := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := d.Token()
		if err != nil {
			return nil, fmt.Errorf("not a property list: %v", err)
		}
		if start, ok := tok.(xml.StartElement); ok && start.Name.Local != "plist" {
			return plistValue(d, start)
		}
	}
}

func plistValue(d *xml.Decoder, start xml.StartElement) (any, error) {
	switch start.Name.Local {
	case "dict":
		dict := map[string]any{}
		key := ""
		for {
			tok, err := d.Token()
			if err != nil {
				return nil, err
			}
			switch t := tok.(type) {
			case xml.StartElement:
				if t.Name.Local == "key" {
					var k string
					if err := d.DecodeElement(&k, &t); err != nil {
						return nil, err
					}
					key = k
					continue
				}
				v, err := plistValue(d, t)
				if err != nil {
					return nil, err
				}
				dict[key] = v
			case xml.EndElement:
				return dict, nil
			}
		}
	case "array":
		var list []any
		for {
			tok, err := d.Token()
			if err != nil {
				return nil, err
			}
			switch t := tok.(type) {
			case xml.StartElement:
				v, err := plistValue(d, t)
				if err != nil {
					return nil, err
				}
				list = append(list, v)
			case xml.EndElement:
				return list, nil
			}
		}
	case "true", "false":
		if err := d.Skip(); err != nil {
			return nil, err
		}
		return start.Name.Local == "true", nil
	}
	var text string
	if err := d.DecodeElement(&text, &start); err != nil {
		return nil, err
	}
	text = strings.TrimSpace(text)
	switch start.Name.Local {
	case "integer":
		return strconv.ParseInt(text, 10, 64)
	case "real":
		return strconv.ParseFloat(text, 64)
	case "date":
		return time.Parse(time.RFC3339, text)
	case "data":
		return base64.StdEncoding.DecodeString(strings.Join(strings.Fields(text), ""))
	}
	return text, nil
}

// copyFile copies from to to.
func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(to)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// deviceRunCommand installs the app at bundle on a connected device and returns the command
// that launches it with args, waiting for it to end with its output on lyrac's. lyrac's `SDL_`
// variables and `TZ` reach the app as `DEVICECTL_CHILD_…`.
func deviceRunCommand(bundle, id string, args []string) (*exec.Cmd, error) {
	device, err := connectedDevice()
	if err != nil {
		return nil, err
	}
	if out, err := exec.Command("xcrun", "devicectl", "device", "install", "app", "--device", device, bundle).CombinedOutput(); err != nil {
		// What a person can do something about, said in a line rather than devicectl's page.
		for _, known := range []struct{ says, means string }{
			{"Developer mode is not enabled", "Developer Mode is off on the device: Settings > Privacy & Security > " +
				"Developer Mode, turn it on, restart, and confirm"},
			{"is locked", "the device is locked: unlock it and run again"},
			{"not trusted", "the device does not trust this Mac: unlock it and tap Trust"},
		} {
			if strings.Contains(string(out), known.says) {
				return nil, fmt.Errorf("installing on the device failed: %s", known.means)
			}
		}
		return nil, fmt.Errorf("installing %s on %s failed: %v\n%s", bundle, device, err, out)
	}
	launch := append([]string{"devicectl", "device", "process", "launch", "--device", device,
		"--console", "--terminate-existing", id}, args...)
	cmd := exec.Command("xcrun", launch...)
	cmd.Env = os.Environ()
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "SDL_") || strings.HasPrefix(kv, "TZ=") {
			cmd.Env = append(cmd.Env, "DEVICECTL_CHILD_"+kv)
		}
	}
	// Ended by its process: devicectl terminates by process identifier, found among the
	// device's by the app's executable.
	executable := "/" + filepath.Base(bundle) + "/" + strings.TrimSuffix(filepath.Base(bundle), ".app")
	stopApp = func() {
		out, err := exec.Command("xcrun", "devicectl", "device", "info", "processes", "--device", device,
			"--json-output", "-").Output()
		if err != nil {
			return
		}
		var listing struct {
			Result struct {
				RunningProcesses []struct {
					ProcessIdentifier int    `json:"processIdentifier"`
					Executable        string `json:"executable"`
				} `json:"runningProcesses"`
			} `json:"result"`
		}
		if json.Unmarshal(out, &listing) != nil {
			return
		}
		for _, p := range listing.Result.RunningProcesses {
			if strings.Contains(p.Executable, executable) {
				_ = exec.Command("xcrun", "devicectl", "device", "process", "terminate", "--device", device,
					"--pid", strconv.Itoa(p.ProcessIdentifier)).Run()
			}
		}
	}
	launchedID = id
	return cmd, nil
}

// connectedDevice is the device an app is run on: `$LYRA_IOS_DEVICE`, else the first iPhone or
// iPad devicectl lists as connected.
func connectedDevice() (string, error) {
	if device := os.Getenv("LYRA_IOS_DEVICE"); device != "" {
		return device, nil
	}
	listing := filepath.Join(os.TempDir(), fmt.Sprintf("lyrac-devices-%d.json", os.Getpid()))
	defer os.Remove(listing)
	if out, err := exec.Command("xcrun", "devicectl", "list", "devices", "--json-output", listing).CombinedOutput(); err != nil {
		return "", fmt.Errorf("running on a device needs Xcode (devicectl failed: %v)\n%s", err, out)
	}
	data, err := os.ReadFile(listing)
	if err != nil {
		return "", err
	}
	var devices struct {
		Result struct {
			Devices []struct {
				Identifier         string `json:"identifier"`
				HardwareProperties struct {
					Reality  string `json:"reality"`
					Platform string `json:"platform"`
				} `json:"hardwareProperties"`
				DeviceProperties struct {
					Name string `json:"name"`
				} `json:"deviceProperties"`
				ConnectionProperties struct {
					PairingState string `json:"pairingState"`
				} `json:"connectionProperties"`
			} `json:"devices"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &devices); err != nil {
		return "", fmt.Errorf("devicectl's device list: %v", err)
	}
	for _, d := range devices.Result.Devices {
		// A real device says no `reality` (a simulator says "simulated"), and its tunnel is
		// "disconnected" until devicectl reaches it: paired is what matters.
		if d.HardwareProperties.Reality != "simulated" && d.HardwareProperties.Platform == "iOS" &&
			d.ConnectionProperties.PairingState == "paired" {
			return d.Identifier, nil
		}
	}
	return "", fmt.Errorf("no iPhone or iPad is connected: plug one in (or join it to this Mac's network, " +
		"paired in Xcode > Devices) and trust this Mac, or set LYRA_IOS_DEVICE")
}

// devicePlatform reports whether p is a device, which runs only what a profile signs.
func devicePlatform(p target.Platform) bool { return p.SDK == "iphoneos" }
