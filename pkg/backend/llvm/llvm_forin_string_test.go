package llvm

import "testing"

// `for c in <string>` walks the string's runes (UTF-8 decoded), binding each code
// point as the loop variable. Counting proves the byte-advance is right (a wrong
// advance would miscount multibyte characters); a rune comparison proves the decoded
// code point is correct.
func TestExec_ForInString(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			// 5 ASCII runes.
			"counts ascii runes",
			`let main = () -> u8 => {
  var n: u8 = 0
  for c in "hello" {
    n += 1
  }
  n
}`,
			5,
		},
		{
			// "café" is 5 bytes but 4 runes (é is a 2-byte sequence).
			"counts runes not bytes (2-byte)",
			`let main = () -> u8 => {
  var n: u8 = 0
  for c in "café" {
    n += 1
  }
  n
}`,
			4,
		},
		{
			// "a😀b": 😀 is a 4-byte sequence, so 6 bytes but 3 runes.
			"counts runes with a 4-byte character",
			`let main = () -> u8 => {
  var n: u8 = 0
  for c in "a😀b" {
    n += 1
  }
  n
}`,
			3,
		},
		{
			// The loop variable binds each rune; the last is 'z'.
			"binds the last rune (ascii)",
			`let main = () -> u8 => {
  var last: rune = ' '
  for c in "xyz" {
    last = c
  }
  if last == 'z' { 1 } else { 0 }
}`,
			1,
		},
		{
			// A 2-byte rune decodes to the right code point (matches the char literal 'é').
			"decodes a 2-byte rune value",
			`let main = () -> u8 => {
  var last: rune = ' '
  for c in "é" {
    last = c
  }
  if last == 'é' { 1 } else { 0 }
}`,
			1,
		},
		{
			"break out of a string loop",
			`let main = () -> u8 => {
  var n: u8 = 0
  for c in "abcdef" {
    if c == 'c' { break }
    n += 1
  }
  n
}`,
			2, // 'a', 'b', then break at 'c'
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("expected exit %d, got %d", c.want, got)
			}
		})
	}
}
