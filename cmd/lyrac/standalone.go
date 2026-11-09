package main

// `lyrac build --standalone`: a program that runs on a machine with none of what built it — on
// macOS `<out>.app`, the shared libraries it links in Contents/Frameworks and its resources in
// Contents/Resources, where `SDL_GetBasePath` looks; on Linux the folder `<out>`, the program,
// `lib/` and its resources side by side. For the machine it is built on.

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Lyra-Language/lyra/pkg/driver"
	"github.com/Lyra-Language/lyra/pkg/target"
)

// buildStandalone builds o's program in a temporary folder and packages it at o's output.
func buildStandalone(o buildOptions, res *driver.Result, entry *driver.EntryPoint) int {
	p, err := platformOf(o)
	if err != nil {
		fmt.Fprintf(os.Stderr, "lyrac: %v\n", err)
		return 2
	}
	if !p.IsMachine() || (p.OS != "darwin" && p.OS != "linux") {
		fmt.Fprintf(os.Stderr, "lyrac: --standalone packages a program for the macOS or Linux machine it is built on, not %s\n", p.Name)
		return 2
	}
	dest := exePath(o)
	work, err := os.MkdirTemp("", "lyrac-standalone-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "lyrac: %v\n", err)
		return 1
	}
	defer os.RemoveAll(work)
	o.out = filepath.Join(work, filepath.Base(strings.TrimSuffix(dest, ".app")))
	exe, code := lowerAndEmit(o, res, entry)
	if code != 0 {
		return code
	}
	o.report(90, "packaging")
	var made string
	if p.OS == "darwin" {
		made, err = packageMac(exe, dest, bundleID(o, dest), res.Target.Resources)
	} else {
		made, err = packageLinux(exe, dest, res.Target.Resources)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "lyrac: %v\n", err)
		return 1
	}
	fmt.Printf("%s: wrote %s (standalone)\n", o.path, made)
	return 0
}

// standaloneLinkFlags are what a standalone build adds to the link on p: Linux's program finds
// its libraries beside itself. A Mac's are rewritten once they are copied.
func standaloneLinkFlags(p target.Platform) []string {
	if p.OS == "linux" {
		return []string{"-Wl,-rpath,$ORIGIN/lib"}
	}
	return nil
}

// clearOld removes a package made before at path; anything else there is refused, so a mistyped
// output never deletes someone's folder.
func clearOld(path, ownMark string) error {
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return nil
	}
	if _, err := os.Stat(filepath.Join(path, ownMark)); err != nil {
		return fmt.Errorf("%s is already there and is not a game lyrac packaged; choose another name", path)
	}
	return os.RemoveAll(path)
}

func packageMac(exe, dest, id, resources string) (string, error) {
	bundle := dest
	if !strings.HasSuffix(bundle, ".app") {
		bundle += ".app"
	}
	name := strings.TrimSuffix(filepath.Base(bundle), ".app")
	if err := clearOld(bundle, "Contents/Info.plist"); err != nil {
		return "", err
	}
	contents := filepath.Join(bundle, "Contents")
	frameworks := filepath.Join(contents, "Frameworks")
	resourcesDir := filepath.Join(contents, "Resources")
	macos := filepath.Join(contents, "MacOS")
	for _, dir := range []string{macos, frameworks, resourcesDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
	}
	program := filepath.Join(macos, name)
	if err := copyMode(exe, program, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(contents, "Info.plist"), []byte(macInfoPlist(name, id)), 0o644); err != nil {
		return "", err
	}
	if err := carryResources(resources, resourcesDir); err != nil {
		return "", err
	}
	if err := carryMacLibraries(program, exe, frameworks); err != nil {
		return "", err
	}
	entries, err := os.ReadDir(frameworks)
	if err != nil {
		return "", err
	}
	// Rewriting a library's load commands breaks its signature: each is signed again, inside out.
	for _, e := range entries {
		if err := adHocSign(filepath.Join(frameworks, e.Name())); err != nil {
			return "", err
		}
	}
	return bundle, adHocSign(bundle)
}

func adHocSign(path string) error {
	if out, err := exec.Command("codesign", "--force", "--sign", "-", "--timestamp=none", path).CombinedOutput(); err != nil {
		return fmt.Errorf("signing %s failed: %v\n%s", path, err, out)
	}
	return nil
}

// carryMacLibraries copies every library `program` needs that is not the system's into
// frameworks, following them transitively, and points each at its copy. `original` is where the
// program was linked, for what its rpaths meant.
func carryMacLibraries(program, original, frameworks string) error {
	done := map[string]bool{}
	// One file, what it was loaded as (for @loader_path), and how it reaches a neighbour.
	var visit func(file, origin, prefix string) error
	visit = func(file, origin, prefix string) error {
		deps, rpaths, err := macLoads(file)
		if err != nil {
			return err
		}
		for _, dep := range deps {
			if isMacSystem(dep) {
				continue
			}
			from, err := resolveMacLoad(dep, origin, rpaths)
			if err != nil {
				return fmt.Errorf("%s needs %s: %v", filepath.Base(file), dep, err)
			}
			base := filepath.Base(dep)
			copied := filepath.Join(frameworks, base)
			if !done[base] {
				done[base] = true
				if err := copyMode(from, copied, 0o755); err != nil {
					return err
				}
				if err := tool("install_name_tool", "-id", "@rpath/"+base, copied); err != nil {
					return err
				}
				if err := visit(copied, from, "@loader_path/"); err != nil {
					return err
				}
			}
			if err := tool("install_name_tool", "-change", dep, prefix+base, file); err != nil {
				return err
			}
		}
		// A package names no folder of the machine that built it.
		for _, r := range rpaths {
			if err := tool("install_name_tool", "-delete_rpath", r, file); err != nil {
				return err
			}
		}
		return nil
	}
	return visit(program, original, "@executable_path/../Frameworks/")
}

func tool(name string, args ...string) error {
	if out, err := exec.Command(name, args...).CombinedOutput(); err != nil {
		return fmt.Errorf("%s %s failed: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return nil
}

func isMacSystem(lib string) bool {
	return strings.HasPrefix(lib, "/usr/lib/") || strings.HasPrefix(lib, "/System/")
}

// macLoads is the libraries a Mach-O file loads, other than its own name, and its rpaths.
func macLoads(file string) (deps, rpaths []string, err error) {
	out, err := exec.Command("otool", "-l", file).Output()
	if err != nil {
		return nil, nil, fmt.Errorf("otool -l %s: %v", file, err)
	}
	command := ""
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch {
		case fields[0] == "cmd":
			command = fields[1]
		case fields[0] == "name" && (command == "LC_LOAD_DYLIB" || command == "LC_LOAD_WEAK_DYLIB" || command == "LC_REEXPORT_DYLIB"):
			deps = append(deps, fields[1])
		case fields[0] == "path" && command == "LC_RPATH":
			rpaths = append(rpaths, fields[1])
		}
	}
	return deps, rpaths, nil
}

// resolveMacLoad is the file a load command means for a file that was at origin.
func resolveMacLoad(dep, origin string, rpaths []string) (string, error) {
	expand := func(p string) string {
		dir := filepath.Dir(origin)
		p = strings.ReplaceAll(p, "@loader_path", dir)
		return strings.ReplaceAll(p, "@executable_path", dir)
	}
	if rest, ok := strings.CutPrefix(dep, "@rpath/"); ok {
		for _, r := range rpaths {
			candidate := filepath.Join(expand(r), rest)
			if _, err := os.Stat(candidate); err == nil {
				return candidate, nil
			}
		}
		return "", fmt.Errorf("no rpath holds it")
	}
	path := expand(dep)
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	return path, nil
}

func macInfoPlist(name, id string) string {
	var escaped bytes.Buffer
	xml.EscapeText(&escaped, []byte(name))
	text := escaped.String()
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleDevelopmentRegion</key><string>en</string>
	<key>CFBundleExecutable</key><string>` + text + `</string>
	<key>CFBundleIdentifier</key><string>` + id + `</string>
	<key>CFBundleInfoDictionaryVersion</key><string>6.0</string>
	<key>CFBundleName</key><string>` + text + `</string>
	<key>CFBundlePackageType</key><string>APPL</string>
	<key>CFBundleShortVersionString</key><string>1.0</string>
	<key>CFBundleVersion</key><string>1</string>
	<key>NSHighResolutionCapable</key><true/>
</dict>
</plist>
`
}

// linuxBase are the libraries every Linux has and a program must take from it.
var linuxBase = []string{"linux-vdso", "ld-linux", "libc.so", "libm.so", "libdl.so", "libpthread.so", "librt.so", "libgcc_s", "libstdc++"}

func packageLinux(exe, dest, resources string) (string, error) {
	name := filepath.Base(dest)
	if err := clearOld(dest, name); err != nil {
		return "", err
	}
	lib := filepath.Join(dest, "lib")
	if err := os.MkdirAll(lib, 0o755); err != nil {
		return "", err
	}
	if err := copyMode(exe, filepath.Join(dest, name), 0o755); err != nil {
		return "", err
	}
	if err := carryResources(resources, dest); err != nil {
		return "", err
	}
	out, err := exec.Command("ldd", exe).Output()
	if err != nil {
		return "", fmt.Errorf("ldd %s: %v", exe, err)
	}
	direct, err := exec.Command("readelf", "-d", exe).Output()
	if err != nil {
		return "", fmt.Errorf("readelf -d %s: %v", exe, err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[1] != "=>" {
			continue
		}
		soname := fields[0]
		if linuxIsBase(soname) || !strings.Contains(string(direct), "["+soname+"]") {
			continue
		}
		if fields[2] == "not" {
			return "", fmt.Errorf("the program needs %s, which is not found here", soname)
		}
		if err := copyMode(fields[2], filepath.Join(lib, soname), 0o755); err != nil {
			return "", err
		}
	}
	return dest, nil
}

func linuxIsBase(soname string) bool {
	for _, base := range linuxBase {
		if strings.HasPrefix(soname, base) {
			return true
		}
	}
	return false
}

func copyMode(from, to string, mode os.FileMode) error {
	data, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	return os.WriteFile(to, data, mode)
}
