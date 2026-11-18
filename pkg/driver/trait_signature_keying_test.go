package driver_test

import "testing"

// A trait method's signature names types as the trait's module resolves them, wherever the
// method is implemented or called. It was resolved from the reader's module: an impl's
// unannotated parameters, typed from the signature, were "unknown type" where the impl's
// module imported none of them, and a call — through a bound or on a concrete receiver —
// was refused ("cannot assign draw.Color to Color") once a second module exported a type of
// the same name, the program-wide fallback finding two. Neither `host`, `any` nor `main`
// imports `draw.Color`; `other` exports a `Color` of its own (11/17).
func TestTrait_SignatureNamesTheTraitsTypes(t *testing.T) {
	files := map[string]string{
		"draw.lyra": `module draw
pub struct Color {
  r: u8 = 0,
}
`,
		"backend.lyra": `module backend
import draw.{ Color }
pub trait Renderer {
  draw: (mut Self, Color, i64) -> i64
}
`,
		"thing.lyra": `module thing
import draw.{ Color }
import backend.{ Renderer }
pub struct Blank {}
impl Renderer for Blank {
  draw = (self, background, n) => i64(background.r) + n
}
pub let grey = () -> Color => Color { r: 7 }
`,
		"other.lyra": `module other
pub struct Color {
  name: string = "",
}
pub let named = () -> Color => Color { name: "teal" }
`,
		"host.lyra": `module host
import backend.{ Renderer }
import thing.{ grey }
pub let paint_with<r> where r: Renderer = (renderer: mut r, n: i64) -> i64 => renderer.draw(grey(), n)
`,
		"any.lyra": `module any
import backend.{ Renderer }
import thing.{ Blank }
pub struct Any {
  inner: Blank,
}
impl Renderer for Any {
  draw = (self, background, n) => {
    var b = self.inner
    b.draw(background, n)
  }
}
`,
		"main.lyra": `module main
import any.{ Any }
import thing.{ Blank, grey }
import host.{ paint_with }
import other.{ named }
let main = () -> void => {
  var a = Any { inner: Blank {} }
  var b = Blank {}
  println("${paint_with(a, 3)} ${b.draw(grey(), 4)} ${named().name}")
}
`,
	}
	res := analyzeFiles(t, files)
	for _, d := range res.Errors() {
		t.Errorf("%s:%d: %s", d.Location.File, d.Location.StartLine, d.Message)
	}
}
