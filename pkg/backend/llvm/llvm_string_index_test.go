package llvm

import "testing"

// `s[i]` yields the i-th **rune** of a string (UTF-8 decoded, not the i-th byte),
// found by walking runes from the front. A multibyte string proves it's rune-indexed
// (byte-indexing would land mid-sequence). Running past the end traps.
func TestExec_StringIndex(t *testing.T) {
	t.Parallel()
	// Each program returns 1 when the indexed rune matches the expected char literal.
	cases := []struct {
		name string
		src  string
	}{
		{
			"first rune",
			`let main = () -> u8 => {
  let s: string = "hello"
  if s[0] == 'h' { 1 } else { 0 }
}`,
		},
		{
			"third rune",
			`let main = () -> u8 => {
  let s: string = "hello"
  if s[2] == 'l' { 1 } else { 0 }
}`,
		},
		{
			"runtime index",
			`let main = () -> u8 => {
  let s: string = "hello"
  var i: i64 = 4
  if s[i] == 'o' { 1 } else { 0 }
}`,
		},
		{
			// "café": rune 3 is 'é' (a 2-byte sequence). Byte 3 would be é's lead byte.
			"rune index past a 2-byte character",
			`let main = () -> u8 => {
  let s: string = "café"
  if s[3] == 'é' { 1 } else { 0 }
}`,
		},
		{
			// "a😀b": rune 1 is the 4-byte emoji.
			"4-byte character by rune index",
			`let main = () -> u8 => {
  let s: string = "a😀b"
  if s[1] == '😀' { 1 } else { 0 }
}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != 1 {
				t.Errorf("expected 1 (rune matched), got %d", got)
			}
		})
	}
}

// Indexing past the last rune traps out-of-bounds (exit 101).
func TestExec_StringIndex_BoundsTrap(t *testing.T) {
	t.Parallel()
	src := `let at = (s: string, i: i64) -> u8 => if s[i] == 'a' { 1 } else { 0 }
let main = () -> u8 => {
  let s: string = "abc"
  at(s, 5)
}`
	if got := buildAndRun(t, src); got != 101 {
		t.Errorf("expected an out-of-bounds trap (exit 101), got %d", got)
	}
}
