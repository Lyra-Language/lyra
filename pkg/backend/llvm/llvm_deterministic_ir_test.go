package llvm

import (
	"strings"
	"testing"
)

// Emitting one program twice gives the same IR. A parameter with a default was named after
// `Parameter.GetName()`, which appended a Go dump of the default expression — heap
// addresses included — so `%"p.size = &{… 0xc000356940 …}"` changed from run to run. That
// made builds irreproducible and defeated this suite's binary cache, which is keyed on the
// emitted IR, for every program that touched such a function.
func TestEmit_DefaultedParametersAreNamedDeterministically(t *testing.T) {
	t.Parallel()
	src := `struct Vec2 { x: f32, y: f32 }
let place = pure (at: Vec2 = Vec2 { x: 1.0, y: 2.0 }, scale: f32 = 0.5, n: i64 = 3) -> i64 =>
  if at.x * scale > 1.0 { n + 10 } else { n }
let main = () -> u8 => u8(place() + place(Vec2 { x: 4.0, y: 0.0 }, 1.0, 1))`
	first, err := emitSource(t, src)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	for i := 0; i < 3; i++ {
		again, err := emitSource(t, src)
		if err != nil {
			t.Fatalf("emit: %v", err)
		}
		if again != first {
			t.Fatalf("emit %d differs from the first", i+2)
		}
	}
	for _, want := range []string{"%p.at", "%p.scale", "%p.n"} {
		if !strings.Contains(first, want) {
			t.Errorf("IR has no parameter named %s", want)
		}
	}
	if strings.Contains(first, "0xc0") {
		t.Error("IR still contains a Go heap address")
	}
}
