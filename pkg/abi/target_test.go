package abi_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/abi"
)

// The triples lyrac can actually meet, and the ones it must refuse.
func TestFromTriple(t *testing.T) {
	cases := []struct {
		triple string
		want   abi.Target
		why    string
	}{
		{"arm64-apple-macosx14.0.0", abi.AArch64, "the development host"},
		{"arm64-apple-darwin24.0.0", abi.AArch64, "the same, spelled darwin"},
		{"aarch64-unknown-linux-gnu", abi.AArch64, "the ASan container"},
		{"x86_64-unknown-linux-gnu", abi.X86_64SysV, "CI"},
		{"x86_64-apple-macosx13.0.0", abi.X86_64SysV, "an Intel Mac is SysV too"},
		{"arm64e-apple-ios17.0.0", abi.AArch64, "arm64e is AAPCS64 for these purposes"},

		// **Windows is refused rather than approximated.** Its x64 convention is
		// neither of the two here — an aggregate that is not 1, 2, 4 or 8 bytes goes
		// indirectly, and there is no SSE-eightbyte rule — so calling it SysV would
		// emit exactly the silently-wrong code this package exists to prevent.
		{"x86_64-pc-windows-msvc", abi.Unknown, "MSVC x64 has its own rules"},
		{"aarch64-pc-windows-msvc", abi.Unknown, "and so does ARM64 Windows"},
		{"x86_64-w64-mingw32", abi.Unknown, "mingw targets the same convention"},

		{"riscv64-unknown-linux-gnu", abi.Unknown, "no classifier written"},
		{"i386-unknown-linux-gnu", abi.Unknown, "32-bit is a different ABI entirely"},
		{"", abi.Unknown, "no answer at all"},
	}
	for _, c := range cases {
		if got := abi.FromTriple(c.triple); got != c.want {
			t.Errorf("FromTriple(%q) = %v; want %v (%s)", c.triple, got, c.want, c.why)
		}
	}
}

// The host must classify, or every test below this one in the suite is vacuous.
func TestDetectTargetFindsTheHost(t *testing.T) {
	clang := lookClang(t)
	if got := abi.DetectTarget(clang); got == abi.Unknown {
		t.Errorf("DetectTarget(%q) = Unknown; the development host must classify", clang)
	}
}
