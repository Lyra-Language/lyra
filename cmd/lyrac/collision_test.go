package main

import (
	"os"
	"path/filepath"
	"testing"
)

// std.genesis.collision's tests are plain Lyra, so they run on the host too: each case
// answers its own exit code. Boxes, a box against an ellipse at its side and at its
// curve, two ellipses, touching (never an overlap), mirroring, and a map whose solid tile
// is its right half — then mirrored by its cell, its left. A shape with no area meets
// nothing, even inside another: an entity that collides by no shape has one. Colliders
// meet one-sidedly — by the looker's mask against the other's layers — and a collider
// hits only the map's tiles on a layer it looks for.
func TestRun_CollisionShapesOverlap(t *testing.T) {
	root := repoRoot(t)
	t.Setenv("LYRA_STD", root)
	src := filepath.Join(t.TempDir(), "main.lyra")
	if err := os.WriteFile(src, []byte(`import std.genesis.collision
import std.genesis.collision.{ Shape, Collider }

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
  let none = Shape { x: 0, y: 0, width: 0, height: 0 }
  if none.overlaps(5, 5, box, 0, 0) { return 15 }
  if box.overlaps(0, 0, none, 5, 5) { return 16 }
  if none.hits_map(13, 2, cells, 3, 4, starts, shapes) { return 17 }
  let wall: u16 = 1
  let water: u16 = 2
  let player: u16 = 4
  let feet_c = Collider { shape: feet, layers: player, mask: wall }
  let star = Collider { shape: box, layers: 8, mask: player }
  if !star.meets(0, 0, feet_c, 2, 2) { return 18 }
  if feet_c.meets(2, 2, star, 0, 0) { return 19 }
  if star.meets(0, 0, feet_c, 20, 20) { return 20 }
  // The map's solid half-tile on the water layer: a hull looking for walls passes it.
  let tiles: [1]Collider = #[Collider { shape: shapes[0], layers: water, mask: 0 }]
  if feet_c.hits_map(10, 0, cells, 3, 4, starts, tiles) { return 21 }
  let swimmer = Collider { feet_c | mask: water }
  if !swimmer.hits_map(10, 0, cells, 3, 4, starts, tiles) { return 22 }
  if !swimmer.hits_map(8, 8, cells, 3, 4, starts, tiles) { return 23 }
  if swimmer.mirrored(10).shape.x != 6 { return 24 }
  0
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, stderr, code := captureRun(t, "run", src); code != 0 {
		t.Errorf("case %d failed; stderr: %s", code, stderr)
	}
}
