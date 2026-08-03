package modules

import (
	"os"
	"path/filepath"
)

// This file answers "where does a compile look for source?" once, for every front-end
// consumer. It lived in cmd/lyrac until the language server needed the same answer —
// and, not having it, analyzed each buffer as a lone unit with no prelude, so every use
// of `Maybe`/`Some`/`Result` was reported undefined in the editor while `lyrac check`
// on the same file was clean.

// DefaultRoots are the search roots for a compile entered at entryFile: the file's own
// directory first — a program's modules sit alongside it — then the standard library,
// if there is one.
func DefaultRoots(entryFile string) []string {
	roots := []string{filepath.Dir(entryFile)}
	if std := StdRoot(); std != "" {
		roots = append(roots, std)
	}
	return roots
}

// DefaultOptions is the resolution configuration a user-facing tool wants: the prelude
// on, unless LYRA_NO_PRELUDE says otherwise.
//
// LYRA_NO_PRELUDE exists for bootstrapping and for tests: the prelude is ordinary Lyra,
// so it has to be compilable by a compiler that is not yet handing it to everything
// else.
func DefaultOptions() Options {
	if os.Getenv("LYRA_NO_PRELUDE") != "" {
		return Options{}
	}
	return Options{Prelude: PreludeModule}
}

// StdRoot locates the standard library: the directory that *contains* `std/`, since a
// module path resolves beneath a root (`std.prelude` → `<root>/std/prelude.lyra`).
//
// LYRA_STD wins so a build can point at a working copy; otherwise the executable's own
// directory is used, which is where ./build.sh puts `std` beside the binary — the same
// beside-the-executable convention Rust, Zig and Go use for a sysroot. An absent
// standard library is not an error: a program that uses nothing from it still builds.
func StdRoot() string {
	if root := os.Getenv("LYRA_STD"); root != "" {
		return root
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	// Resolve symlinks before taking the directory. os.Executable does not do it
	// consistently — on Linux it reads /proc/self/exe, which is already resolved, but on
	// macOS it can hand back the symlink's own path. So a compiler symlinked onto PATH
	// (`ln -s .../build/lyrac /usr/local/bin/lyrac`) would look for the standard library
	// beside the *link* rather than beside the real binary, and find nothing — a
	// platform split that shows up as "the prelude works on my machine". The language
	// server is normally reached through exactly such a symlink.
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	// The *root* is the directory holding `std/`, not `std/` itself: a module path is
	// resolved beneath a root, so `std.prelude` becomes `<root>/std/prelude.lyra`.
	// Returning the `std` directory here instead looked for `std/std/prelude.lyra` and
	// silently found no prelude.
	root := filepath.Dir(exe)
	if info, err := os.Stat(filepath.Join(root, "std")); err == nil && info.IsDir() {
		return root
	}
	return ""
}
