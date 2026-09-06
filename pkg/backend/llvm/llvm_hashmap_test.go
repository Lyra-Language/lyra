package llvm

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// `std.collections`'s `HashMap<k, v>` is ordinary Lyra over `[]Maybe<Entry<k, v>>`, so
// what these tests pin is not a builtin but that the language can carry one: a generic
// struct holding a generic `Maybe` array, a `mut self` that reassigns that array, a
// bound-dispatched `hash()` on the key, and structural `==` on a bare type variable. Every
// one of those was a compiler gap at some point between 09/06 and the day the map was
// written, so the map doubles as the standing check that none has reopened.

// Growth and backward-shift deletion, checked against the answer rather than against
// the table's own bookkeeping: every key inserted is found with its value, every key
// removed is absent, and every key *not* removed is still found afterwards — the last
// being what a deletion that breaks a probe chain gets wrong while `len()` stays right.
func TestExec_HashMapInsertGrowRemove(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `module main
import std.collections.{ HashMap, hashmap_with_capacity, insert, get, remove, len }
let main = () -> void => {
  var m: HashMap<i64, i64> = hashmap_with_capacity(4)
  for i in 0..<1000 { m.insert(i * 7, i) }
  var all_found = true
  for i in 0..<1000 { if m.get(i * 7) != Some(i) { all_found = false } }
  for i in 0..<1000 { if i %% 3 == 0 { let _ = m.remove(i * 7) } }
  var rest_ok = true
  for i in 0..<1000 {
    let want: Maybe<i64> = if i %% 3 == 0 { None } else { Some(i) }
    if m.get(i * 7) != want { rest_ok = false }
  }
  print("${all_found} ${rest_ok} ${m.len()}")
}
`, "")
	if got := strings.TrimSpace(out); got != "true true 666" {
		t.Errorf("hash map after 1000 inserts and 334 removes = %q; want \"true true 666\"", got)
	}
}

// String keys and values through `replace` and `remove` churn, under ASan. A grow moves
// every entry into a fresh buffer and a backward shift copies entries between slots, so a
// missing retain on either path is a use-after-free the moment the old slot is released;
// the concatenated strings make sure every key and value is heap-allocated and can be.
func TestExec_HashMapStringChurnASan(t *testing.T) {
	t.Parallel()
	src := `module main
import std.collections.{ HashMap, hashmap_new, insert, replace, get_or, remove, len }
let main = () -> u8 => {
  var names: HashMap<string, string> = hashmap_new()
  for round in 0..<20 {
    for i in 0..<64 { names.insert("k" ++ "${i}", "v" ++ "${i}-${round}") }
    for i in 0..<64 { if i %% 2 == 0 { let _ = names.remove("k" ++ "${i}") } }
    let _ = names.replace("k" ++ "1", "again")
  }
  if names.len() == 32 && names.get_or("k1", "?") == "again" && names.get_or("k2", "?") == "?" { 3 } else { 1 }
}`
	if got := exitCode(t, exec.Command(preludeBinary(t, src)).Run()); got != 3 {
		t.Errorf("exited %d; want 3", got)
	}
	if got := buildAndRunASanWithPrelude(t, src); got != 3 {
		t.Errorf("under ASan: exited %d; want 3", got)
	}
}

// buildAndRunASanWithPrelude is buildAndRunASan over the resolving front half: the map
// lives in `std.collections`, and `emitSource` resolves no imports.
func buildAndRunASanWithPrelude(t *testing.T, src string) int {
	t.Helper()
	ir := instrumentForASan(emitWithPrelude(t, src))
	cmd := exec.Command(compileCached(t, lookClang(t), ir, "-fsanitize=address"))
	cmd.Env = append(os.Environ(), "ASAN_OPTIONS=detect_leaks=0")
	asanRunSlots <- struct{}{}
	defer func() { <-asanRunSlots }()
	return exitCode(t, cmd.Run())
}

// exitCode reads a process's exit status out of its Run error, failing the test on any
// error that is not an exit status.
func exitCode(t *testing.T, err error) int {
	t.Helper()
	if err == nil {
		return 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	t.Fatalf("running the binary failed: %v", err)
	return -1
}

// A user type is a key by implementing `Hash`, with `hash_combine` folding its fields.
// Two points with swapped coordinates are the case a careless combine (a plain xor, say)
// makes collide — they are distinct keys and must both be found.
func TestExec_HashMapStructKey(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `module main
import std.collections.{ HashMap, Hash, hashmap_new, insert, get_or, hash_combine }
struct Point { x: i64, y: i64 }
impl Hash for Point {
  hash = pure noalloc (self) => hash_combine(hash_combine(0, self.x.hash()), self.y.hash())
}
let main = () -> void => {
  var m: HashMap<Point, string> = hashmap_new()
  m.insert(Point { x: 1, y: 2 }, "a")
  m.insert(Point { x: 2, y: 1 }, "b")
  print(m.get_or(Point { x: 1, y: 2 }, "?") ++ m.get_or(Point { x: 2, y: 1 }, "?") ++ m.get_or(Point { x: 3, y: 3 }, "?"))
}
`, "")
	if got := strings.TrimSpace(out); got != "ab?" {
		t.Errorf("struct-keyed lookups = %q; want \"ab?\"", got)
	}
}
