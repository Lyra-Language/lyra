package llvm

import "testing"

// An inline-record variant — `data Shape = Square { side: i64 } | …` — is built with a
// struct literal under the constructor's name and matched with a struct pattern, as its
// payload is the anonymous struct `{ side: i64 }`. The typechecker accepted both and the
// backend refused both until 10/30: the literal was "struct instance lowering not
// implemented", and the pattern — `Square({ side })` spelled out too — "struct pattern on
// non-struct value".
func TestExec_AnInlineRecordVariantIsBuiltAndMatched(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{"built and matched, beside a positional and a nullary variant", `data Shape = Square { side: i64 } | Rect { w: i64, h: i64 } | Circle(i64) | Dot
let area = (s: Shape) -> i64 => match s {
  Square { side } => side * side,
  Rect { w, h } => w * h,
  Circle(r) => 3 * r * r,
  Dot => 0,
}
let main = () -> u8 => {
  var total = 0
  for s in [Square { side: 3 }, Rect { w: 2, h: 5 }, Circle(1), Dot] { total += area(s) }
  u8(total)
}`, 22},
		{"fields written out of order, and spelled with parentheses", `data Shape = Rect { w: i64, h: i64 } | Dot
let size = (s: Shape) -> i64 => match s {
  Rect({ h, w }) => w * 10 + h,
  Dot => 0,
}
let main = () -> u8 => u8(size(Rect { h: 4, w: 7 }) + size(Rect({ w: 1, h: 2 })))`, 86},
		{"a field tested by its pattern, and renamed", `data Shape = Rect { w: i64, h: i64 } | Dot
let kind = (s: Shape) -> i64 => match s {
  Rect { w: 0, h } => h,
  Rect { w: width, h: 1 } => 100 + width,
  Rect { w, h } => w * h,
  Dot => 0,
}
let main = () -> u8 => u8(kind(Rect { w: 0, h: 9 }) + kind(Rect { w: 5, h: 1 }) + kind(Rect { w: 2, h: 3 }))`, 120},
		{"if let and let-else", `data Msg = Move { dx: i64, dy: i64 } | Quit
let main = () -> u8 => {
  let m = Move { dx: 3, dy: 4 }
  var n = 0
  if let Move { dx, dy } = m { n = dx * dy }
  let Move { dx, dy: _ } = m else { return 1 }
  u8(n + dx)
}`, 15},
		{"a generic record", `data Box<t> = Wrap { value: t, count: i64 } | Empty
let total = (b: Box<i64>) -> i64 => match b {
  Wrap { value, count } => value * count,
  Empty => 0,
}
let main = () -> u8 => u8(total(Wrap { value: 6, count: 7 }) + total(Empty))`, 42},
		{"a recursive shared tree", `data Tree = Leaf | Node { left: shared Tree, value: i64, right: shared Tree }
let sum = (t: shared Tree) -> i64 => match t {
  Leaf => 0,
  Node { left, value, right } => sum(left) + value + sum(right),
}
let main = () -> u8 => {
  let t: shared Tree = Node { left: Node { left: Leaf, value: 1, right: Leaf }, value: 2, right: Node { left: Leaf, value: 3, right: Leaf } }
  u8(sum(t))
}`, 6},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRunASanWithPrelude(t, tc.src); got != tc.want {
				t.Errorf("exit %d, want %d", got, tc.want)
			}
		})
	}
}

// A record's managed fields are owned by the variant and released with it: built, matched,
// read and dropped, nothing leaks (LeakSanitizer, on Linux).
func TestExec_AnInlineRecordsManagedFieldsDoNotLeak(t *testing.T) {
	t.Parallel()
	got := buildAndRunLSanWithPrelude(t, `data Entry = Named { key: string, tags: []string } | Blank
let width = (e: Entry) -> i64 => match e {
  Named { key, tags } => "${key}".len() + tags.len(),
  Blank => 0,
}
let main = () -> u8 => {
  var n = 0
  for i in 0..<3 {
    let e = Named { key: "k" ++ "ey", tags: ["a" ++ "b", "c"] }
    n += width(e)
  }
  u8(n)
}`)
	if got != 15 {
		t.Errorf("exit %d, want 15", got)
	}
}
