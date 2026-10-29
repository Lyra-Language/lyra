package main

import (
	"bufio"
	"bytes"
	"os/exec"
	"strings"
	"sync"

	"github.com/Lyra-Language/lyra/pkg/target"
)

// The IR lyrac compiles names the target's data layout, as clang reports it (10/29).
//
// **Without one, LLVM reads the IR under its default layout and only then takes the
// target's**, and what was decided while reading stays: an `alloca` written without an
// alignment is given the default layout's — where `i128` aligns to 8 — and keeps it, while
// the code using it is generated under the target's, where x86_64 aligns `i128` to 16 (since
// LLVM 18) and arm64 always has. A value holding an `i128` (std.temporal's nanoseconds) was
// then stored to an 8-aligned slot with a 16-byte `movaps`, which x86_64 faults on: a
// std.temporal program linked in parts by ThinLTO segfaulted on CI, the callee no longer
// inlined into the caller that had allocated the slot. arm64 and Rosetta tolerate the
// misaligned store, which is why nothing else showed it.
//
// **Asked of clang, not kept in a table**: the layout is the compiler's and changes with it
// (that `i128` rule changed in LLVM 18), and lyrac compiles with whatever clang it finds.

var dataLayouts sync.Map // compiler, platform → its `target datalayout` line, or ""

// dataLayoutLine is the `target datalayout = "…"` line clang compiles p's code under, or ""
// when it cannot say (the IR then goes as it did, under clang's default reading).
func dataLayoutLine(cc string, p target.Platform) string {
	key := cc + "\x00" + p.Triple
	if v, ok := dataLayouts.Load(key); ok {
		return v.(string)
	}
	args := []string{"-S", "-emit-llvm", "-x", "c", "/dev/null", "-o", "-"}
	if !p.IsMachine() {
		args = append([]string{"--target=" + p.Triple}, args...)
	}
	out, err := exec.Command(cc, args...).Output()
	line := ""
	if err == nil {
		scan := bufio.NewScanner(bytes.NewReader(out))
		for scan.Scan() {
			if strings.HasPrefix(scan.Text(), "target datalayout = ") {
				line = scan.Text()
				break
			}
		}
	}
	dataLayouts.Store(key, line)
	return line
}

// withDataLayout is ir with the target's data layout stated at its top.
func withDataLayout(cc string, p target.Platform, ir []byte) []byte {
	line := dataLayoutLine(cc, p)
	if line == "" || bytes.Contains(ir, []byte("\ntarget datalayout = ")) || bytes.HasPrefix(ir, []byte("target datalayout = ")) {
		return ir
	}
	return append([]byte(line+"\n"), ir...)
}
