package main

import (
	"os"
	"path/filepath"
	"testing"
)

// std.genesis.collision's tests are plain Lyra, so they run on the host too: each case
// answers its own exit code. Boxes, a box against an ellipse at its side and at its
// curve, two ellipses, touching (never an overlap), mirroring, and a map whose solid tile
// is its right half — then mirrored by its cell, its left.
func TestRun_CollisionShapesOverlap(t *testing.T) {
	root := repoRoot(t)
	t.Setenv("LYRA_STD", root)
	src := filepath.Join(t.TempDir(), "main.lyra")
	if err := os.WriteFile(src, []byte(`import std.genesis.collision
import std.genesis.collision.{ Shape }

let main = () -> u8 => {
  let box = Shape { x: 0, y: 0, width: 10, height: 10 }
  let disc = Shape { ellipse: true, x: 0, y: 0, width: 10, height: 10 }
  if !collision.overlaps(box, 0, 0, box, 9, 9) { return 1 }
  if collision.overlaps(box, 0, 0, box, 10, 0) { return 2 }
  if !collision.overlaps(box, 0, 0, disc, 9, 0) { return 3 }
  // The box's corner (10, 10) and the disc's centre (14, 14): 5.7 apart, past its 5.
  if collision.overlaps(box, 0, 0, disc, 9, 9) { return 4 }
  if !collision.overlaps(box, 0, 0, disc, 6, 6) { return 5 }
  if collision.overlaps(disc, 0, 0, disc, 8, 8) { return 6 }
  if !collision.overlaps(disc, 0, 0, disc, 6, 6) { return 7 }
  if collision.overlaps(disc, 0, 0, disc, 10, 0) { return 8 }
  if collision.mirrored(Shape { x: 2, y: 0, width: 3, height: 1 }, 10).x != 5 { return 9 }
  // A 3×2 map: tile 5 (the set's tile 1, whose shape is its right half) at (1, 0), and
  // at (1, 1) mirrored.
  let cells: [6]u16 = #[0, 5, 0, 0, 0x0800 | 5, 0]
  let starts: [3]u16 = #[0, 0, 1]
  let shapes: [1]Shape = #[Shape { x: 4, y: 0, width: 4, height: 8 }]
  let feet = Shape { x: 0, y: 0, width: 4, height: 4 }
  if collision.hits_map(feet, 8, 0, cells, 3, 4, starts, shapes) { return 10 }
  if !collision.hits_map(feet, 10, 0, cells, 3, 4, starts, shapes) { return 11 }
  if !collision.hits_map(feet, 8, 8, cells, 3, 4, starts, shapes) { return 12 }
  if collision.hits_map(feet, 12, 8, cells, 3, 4, starts, shapes) { return 13 }
  if collision.hits_map(feet, -20, -20, cells, 3, 4, starts, shapes) { return 14 }
  0
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, stderr, code := captureRun(t, "run", src); code != 0 {
		t.Errorf("case %d failed; stderr: %s", code, stderr)
	}
}
