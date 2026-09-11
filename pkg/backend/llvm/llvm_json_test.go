package llvm

import (
	"os/exec"
	"strings"
	"testing"
)

// `std.json` is ordinary Lyra: a recursive `data JsonValue` whose array and object variants
// hold arrays of themselves, parsed by pure functions that thread a byte offset. These pin
// the answers — a repeated key reads as its last value, numbers take the exact path, escapes
// and surrogate pairs decode — and that each malformed text is refused at the right byte.
func TestExec_JsonParse(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `module main
import std.json.{ parse_json, JsonNull, field, element, elements, members, as_number, as_int, as_text, as_bool, kind }
let parsed = (text: string) -> string => match parse_json(text) {
  Ok(v) => "ok ${kind(v)}",
  Err(e) => "err ${e.message} @${e.offset}",
}
let main = () -> void => {
  match parse_json("{\"a\": [1, -2.5, 3e2, 0.001, 1.5E-3, true, null], \"s\": \"h\\u00e9\\n\\ud83d\\ude00\", \"a\": 7}") {
    Ok(doc) => {
      println("${doc.field("a").unwrap_or(JsonNull).as_int().unwrap_or(0)} ${doc.members().len()}")
      for v in doc.members()[0].value.elements() { print("${kind(v)}:${v.as_number().unwrap_or(0.0)} ") }
      println("")
      let s = doc.field("s").unwrap_or(JsonNull).as_text().unwrap_or("?")
      println("${s.len()} ${s.byte_len()}")
      println("${doc.field("missing").is_none()} ${doc.members()[0].value.element(5).unwrap_or(JsonNull).as_bool().unwrap_or(false)}")
    },
    Err(e) => println("err ${e.message}"),
  }
  for t in ["[1, 2,]", "{\"x\" 1}", "\"abc", "[1] x", "", "01", "\"\\q\"", "{\"a\":1,}", " {\"d\": [[[{}]]]} "] {
    println(parsed(t))
  }
}
`, "")
	want := strings.Join([]string{
		"7 3",
		"number:1 number:-2.5 number:300 number:0.001 number:0.0015 bool:0 null:0 ",
		"4 8",
		"true true",
		"err trailing comma in an array @6",
		"err expected `:` after an object key @5",
		"err unterminated string @0",
		"err unexpected content after the value @4",
		"err unexpected end of input @0",
		"err a number may not have a leading zero @1",
		"err invalid escape @1",
		"err trailing comma in an object @7",
		"ok object",
	}, "\n")
	if got := strings.TrimSpace(out); got != want {
		t.Errorf("std.json results:\n%s\nwant:\n%s", got, want)
	}
}

// A document of nested arrays and objects with heap strings, parsed and walked under
// AddressSanitizer: every JsonValue is a tagged union holding boxes, and the parser moves
// them through tuples and `?` — a missing retain on any of those paths frees a string a
// later read still reaches.
func TestExec_JsonNestedStringsASan(t *testing.T) {
	t.Parallel()
	clang := lookClang(t)
	if !asanAvailable(t, clang) {
		t.Skip("ASan runtime not available; skipping")
	}
	src := `module main
import std.json.{ parse_json, JsonNull, field, elements, as_text }
let main = () -> u8 => {
  var text = "["
  for i in 0..<40 {
    if i > 0 { text = text ++ "," }
    text = text ++ "{\"name\": \"node-${i}-\\u00e9\", \"kids\": [\"a${i}\", \"b${i}\"]}"
  }
  text = text ++ "]"
  match parse_json(text) {
    Ok(doc) => {
      var total = 0
      for n in doc.elements() {
        total += n.field("name").unwrap_or(JsonNull).as_text().unwrap_or("").len()
        for k in n.field("kids").unwrap_or(JsonNull).elements() { total += k.as_text().unwrap_or("").len() }
      }
      if total == 570 { 3 } else { 1 }
    },
    Err(_) => 2,
  }
}`
	if got := exitCodeOf(t, exec.Command(compileCached(t, clang, instrumentForASan(emitWithPrelude(t, src)), "-fsanitize=address")).Run()); got != 3 {
		t.Errorf("under ASan: exited %d; want 3", got)
	}
}
