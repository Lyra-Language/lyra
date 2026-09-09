package abi

import (
	"os/exec"
	"strings"
)

// **The C compiler decides the target, so the C compiler is who to ask.** lyrac emits IR
// and hands it to clang; whatever clang thinks it is compiling for is what the ABI must
// match, and `-print-target-triple` is that answer straight from the thing that will
// consume the file. Guessing from `runtime.GOARCH` would be a second opinion, and a wrong
// one under `--cc` naming a cross-compiler.
//
// A target this package cannot classify is **not an error here**. It becomes a refusal at
// the point an aggregate would cross (lyra-E063's backend half), which keeps a new
// platform a diagnostic rather than silent garbage — the whole reason struct-by-value was
// refused until there was a classifier.

// DetectTarget asks cc what it targets. An unusable answer is Unknown, never a guess.
func DetectTarget(cc string) Target {
	if cc == "" {
		return Unknown
	}
	out, err := exec.Command(cc, "-print-target-triple").Output()
	if err != nil {
		return Unknown
	}
	return FromTriple(strings.TrimSpace(string(out)))
}

// FromTriple maps an LLVM target triple onto the convention it implies.
//
// Only the architecture and the platform matter: two triples agreeing on those classify
// identically, which is why `arm64-apple-macosx14` and `aarch64-unknown-linux-gnu` are one
// Target here despite differing elsewhere in the IR (Linux marks an HFA parameter
// `alignstack(8)`, an attribute rather than a type — see abi_diff_test.go).
//
// **Windows is deliberately Unknown.** x86-64 Windows is neither of these: it passes
// aggregates that are not 1, 2, 4 or 8 bytes indirectly and has no SSE-eightbyte rule at
// all, so classifying it as SysV would produce exactly the silently-wrong code this
// package exists to prevent. Adding it means adding a classifier and a row to the
// differential test, not a case here.
func FromTriple(triple string) Target {
	parts := strings.Split(triple, "-")
	if len(parts) == 0 {
		return Unknown
	}
	arch := parts[0]
	isWindows := strings.Contains(triple, "windows") || strings.Contains(triple, "msvc") ||
		strings.Contains(triple, "mingw")
	if isWindows {
		return Unknown
	}
	switch arch {
	case "aarch64", "arm64", "aarch64_be", "arm64e":
		return AArch64
	case "x86_64", "amd64":
		return X86_64SysV
	}
	return Unknown
}
