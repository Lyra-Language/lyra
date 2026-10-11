package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// std.game's pure modules are plain Lyra, so they run on the host: each case answers its own
// exit code. These are std.genesis's collision and entity tests case for case (collision_test.go,
// entity_test.go), in f32 pixels — the two runtimes are one design, and a game ported between
// them must find its collisions where it left them.

// std.genesis.collision's cases, in pixels: boxes, a box against an ellipse at its side and its
// curve, two ellipses, touching, mirroring, a map whose solid tile is its right half and, mirrored
// by its cell, its left; shapes with no area; colliders one-sided by layer.
func TestRun_GameCollisionShapesOverlap(t *testing.T) {
	root := repoRoot(t)
	t.Setenv("LYRA_STD", root)
	src := filepath.Join(t.TempDir(), "main.lyra")
	if err := os.WriteFile(src, []byte(`import std.game.collision
import std.game.collision.{ Shape, Collider }
import std.game.planes.{ FLIP_X }

let main = () -> u8 => {
  let box = Shape { x: 0.0, y: 0.0, width: 10.0, height: 10.0 }
  let disc = Shape { ellipse: true, x: 0.0, y: 0.0, width: 10.0, height: 10.0 }
  if !collision.overlaps(box, 0.0, 0.0, box, 9.0, 9.0) { return 1 }
  if collision.overlaps(box, 0.0, 0.0, box, 10.0, 0.0) { return 2 }
  if !collision.overlaps(box, 0.0, 0.0, disc, 9.0, 0.0) { return 3 }
  // The box's corner (10, 10) and the disc's centre (14, 14): 5.7 apart, past its 5.
  if collision.overlaps(box, 0.0, 0.0, disc, 9.0, 9.0) { return 4 }
  if !collision.overlaps(box, 0.0, 0.0, disc, 6.0, 6.0) { return 5 }
  if collision.overlaps(disc, 0.0, 0.0, disc, 8.0, 8.0) { return 6 }
  if !collision.overlaps(disc, 0.0, 0.0, disc, 6.0, 6.0) { return 7 }
  if collision.overlaps(disc, 0.0, 0.0, disc, 10.0, 0.0) { return 8 }
  if collision.mirrored(Shape { x: 2.0, y: 0.0, width: 3.0, height: 1.0 }, 10.0).x < 4.5 { return 9 }
  // A 3×2 map: tile 5 (the set's tile 1, whose shape is its right half) at (1, 0), and at
  // (1, 1) mirrored.
  let cells: [6]u32 = #[0, 5, 0, 0, FLIP_X | 5, 0]
  let starts: [3]u32 = #[0, 0, 1]
  let shapes: [1]Shape = #[Shape { x: 4.0, y: 0.0, width: 4.0, height: 8.0 }]
  let feet = Shape { x: 0.0, y: 0.0, width: 4.0, height: 4.0 }
  if collision.hits_map(feet, 8.0, 0.0, cells, 3, 4, starts, shapes) { return 10 }
  if !collision.hits_map(feet, 10.0, 0.0, cells, 3, 4, starts, shapes) { return 11 }
  if !collision.hits_map(feet, 8.0, 8.0, cells, 3, 4, starts, shapes) { return 12 }
  if collision.hits_map(feet, 12.0, 8.0, cells, 3, 4, starts, shapes) { return 13 }
  if collision.hits_map(feet, -20.0, -20.0, cells, 3, 4, starts, shapes) { return 14 }
  let none = Shape { x: 0.0, y: 0.0, width: 0.0, height: 0.0 }
  if none.overlaps(5.0, 5.0, box, 0.0, 0.0) { return 15 }
  if box.overlaps(0.0, 0.0, none, 5.0, 5.0) { return 16 }
  if none.hits_map(13.0, 2.0, cells, 3, 4, starts, shapes) { return 17 }
  let wall: u32 = 1
  let water: u32 = 2
  let player: u32 = 4
  let feet_c = Collider { shape: feet, layers: player, mask: wall }
  let star = Collider { shape: box, layers: 8, mask: player }
  if !star.meets(0.0, 0.0, feet_c, 2.0, 2.0) { return 18 }
  if feet_c.meets(2.0, 2.0, star, 0.0, 0.0) { return 19 }
  if star.meets(0.0, 0.0, feet_c, 20.0, 20.0) { return 20 }
  // The map's solid half-tile on the water layer: a hull looking for walls passes it.
  let tiles: [1]Collider = #[Collider { shape: shapes[0], layers: water, mask: 0 }]
  if feet_c.hits_map(10.0, 0.0, cells, 3, 4, starts, tiles) { return 21 }
  let swimmer = Collider { feet_c | mask: water }
  if !swimmer.hits_map(10.0, 0.0, cells, 3, 4, starts, tiles) { return 22 }
  if !swimmer.hits_map(8.0, 8.0, cells, 3, 4, starts, tiles) { return 23 }
  if swimmer.mirrored(10.0).shape.x < 5.5 { return 24 }
  // A fraction of a pixel is a place too: touching at 10 is clear, a hair past it is not.
  if collision.overlaps(box, 0.0, 0.0, box, 10.0, 0.0) || !collision.overlaps(box, 0.0, 0.0, box, 9.75, 0.0) { return 25 }
  0
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, stderr, code := captureRun(t, "run", src); code != 0 {
		t.Errorf("case %d failed; stderr: %s", code, stderr)
	}
}

// std.genesis.entity's cases, in pixels an update: speeding up and friction, gravity to
// MAX_FALL, walls on a layer its colliders look for and the map's edge, sliding, turning
// beside a wall, one-sided meeting, the diagonal no faster than the top speed, one-way
// colliders, banded map colliders, and open edges — plus a stick's lean, which the Genesis
// pad cannot give.
func TestRun_GameEntitiesMoveCollideAndMeet(t *testing.T) {
	root := repoRoot(t)
	t.Setenv("LYRA_STD", root)
	src := filepath.Join(t.TempDir(), "main.lyra")
	if err := os.WriteFile(src, []byte(`import std.game.collision.{ Shape, Collider }
import std.game.entity.{ Animation, EntityKind, MAX_FALL }

const STILL: Animation = Animation { sprite: 0, first: 0, count: 1, total: 1, looping: false }
const NONE: Collider = Collider { shape: Shape { x: 0.0, y: 0.0, width: 0.0, height: 0.0 }, layers: 0, mask: 0 }
// Feet off the sprite's middle, as the hero's are: x 2..12 of 16, 4..14 mirrored.
const FEET: Collider = Collider { shape: Shape { x: 2.0, y: 12.0, width: 10.0, height: 4.0 }, layers: 4, mask: 1 }
const HERO: EntityKind = EntityKind {
  width: 16.0,
  height: 16.0,
  animation: STILL,
  colliders: #[FEET, NONE, NONE, NONE],
  collider_count: 1,
  max_speed: 2.0,
  acceleration: 0.5,
  friction: 1.0,
  gravity: 0.0
}
const STAR: EntityKind = EntityKind {
  width: 8.0,
  height: 8.0,
  animation: STILL,
  colliders: #[Collider { shape: Shape { x: 0.0, y: 0.0, width: 8.0, height: 8.0 }, layers: 8, mask: 0 }, NONE, NONE, NONE],
  collider_count: 1,
  max_speed: 0.0,
  acceleration: 0.0,
  friction: 0.0,
  gravity: 0.0
}

let near = pure (a: f32, b: f32) -> bool => (a - b).abs() < 0.001

let main = () -> u8 => {
  // Speeding up: 0.5 an update to 2, then held there; friction 1 an update to a stop.
  var e = HERO.spawn(10.0, 20.0)
  if !near(e.x(), 10.0) || !near(e.y(), 20.0) || !near(e.vx, 0.0) { return 1 }
  e.push(1.0, 0.0)
  if !near(e.vx, 0.5) { return 2 }
  for _ in 0..<10 { e.push(1.0, 0.0) }
  if !near(e.vx, 2.0) { return 3 }
  e.push(0.0, 0.0)
  e.push(0.0, 0.0)
  if !near(e.vx, 0.0) { return 4 }
  // Pushed both ways, each axis gains the acceleration over √2.
  e.push(-1.0, -1.0)
  if !near(e.vx, -0.353553) || !near(e.vy, -0.353553) { return 5 }
  e.move()
  if !near(e.x(), 9.646447) { return 6 }
  // Both ways at once: each axis's top speed over √2, so the diagonal is the top speed.
  var diagonal = HERO.spawn(0.0, 0.0)
  for _ in 0..<10 { diagonal.push(1.0, -1.0) }
  if !near(diagonal.vx, 1.414214) || !near(diagonal.vy, -1.414214) { return 18 }
  // One axis let go: the other goes on to the full top speed.
  for _ in 0..<10 { diagonal.push(1.0, 0.0) }
  if !near(diagonal.vx, 2.0) || !near(diagonal.vy, 0.0) { return 19 }
  // Gravity: the vertical axis falls, whatever is pushed, at most MAX_FALL.
  var falling = EntityKind { HERO | gravity: 2.0 }.spawn(0.0, 0.0)
  for _ in 0..<10 { falling.push(0.0, -1.0) }
  if !near(falling.vy, MAX_FALL) { return 7 }

  // A 4×4 map of 8-pixel cells: a wall (the set's tile 1, its collider on layer 1) at cell
  // (2, 1), so pixels x 16..24, y 8..16.
  let cells: [16]u32 = #[0, 0, 0, 0, 0, 0, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0]
  let starts: [3]u32 = #[0, 0, 1]
  let tiles: [1]Collider = #[Collider { shape: Shape { x: 0.0, y: 0.0, width: 8.0, height: 8.0 }, layers: 1, mask: 0 }]
  // Walking right into it, the feet (y 12..16) at the wall's rows: stopped with the feet
  // touching it, x speed gone.
  var w = HERO.spawn(0.0, 0.0)
  for _ in 0..<20 {
    w.push(1.0, 0.0)
    w.move_in(cells, 4, 1, starts, tiles)
  }
  if !near(w.x(), 4.0) || !near(w.vx, 0.0) { return 8 }
  // Pushed right and down along it: the x axis stays stopped, the y axis slides on.
  let before = w.y()
  for _ in 0..<4 {
    w.push(1.0, 1.0)
    w.move_in(cells, 4, 1, starts, tiles)
  }
  if !near(w.x(), 4.0) || w.y() <= before { return 9 }
  // The map's edge stops it as a wall does.
  var edge = HERO.spawn(0.0, 16.0)
  edge.push(-1.0, 0.0)
  edge.move_in(cells, 4, 1, starts, tiles)
  if !near(edge.x(), 0.0) || !near(edge.vx, 0.0) { return 10 }

  // Against the wall facing right (feet x 6..16), it turns left: mirrored, its feet would be
  // x 8..18, into the wall. They stay unturned, and walking down it gets past.
  var t = HERO.spawn(4.0, 0.0)
  t.mirrored = true
  for _ in 0..<30 {
    t.push(0.0, 1.0)
    t.move_in(cells, 4, 1, starts, tiles)
  }
  if t.y() < 10.0 { return 11 }
  if t.hit_mirrored != true { return 12 }

  // Meeting is one-sided: the star looks for nothing, the hero's feet look for layer 1 —
  // not the star's 8 — so neither meets the other until one looks for it.
  let star = STAR.spawn(4.0, 12.0)
  var hero = HERO.spawn(0.0, 0.0)
  if hero.meets(star) || star.meets(hero) { return 13 }
  var looking = EntityKind { HERO | colliders: #[Collider { FEET | mask: 8 }, NONE, NONE, NONE] }.spawn(0.0, 0.0)
  if !looking.meets(star) || star.meets(looking) { return 14 }
  // Facing the other way mirrors its colliders: feet x 4..14 still meet the star at 4..12.
  looking.mirrored = true
  if !looking.meets(star) { return 15 }

  // The map's own colliders (move_in_with): a box drawn on the map in its pixels, x 20..28,
  // on layer 1, stops the feet as a tile's would; one on layer 2 does not.
  let empty: [16]u32 = #[0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0]
  let one_band: [2]u32 = #[0, 1]
  let none_here: [1]Collider = #[NONE]
  let wall: [1]Collider = #[Collider { shape: Shape { x: 20.0, y: 0.0, width: 8.0, height: 32.0 }, layers: 1, mask: 0 }]
  var m = HERO.spawn(0.0, 0.0)
  for _ in 0..<20 {
    m.push(1.0, 0.0)
    m.move_in_with(empty, 4, 1, starts, tiles, one_band, wall)
  }
  if !near(m.x(), 8.0) || !near(m.vx, 0.0) { return 20 }
  let other: [1]Collider = #[Collider { shape: Shape { x: 20.0, y: 0.0, width: 8.0, height: 32.0 }, layers: 2, mask: 0 }]
  var past = HERO.spawn(0.0, 0.0)
  for _ in 0..<20 {
    past.push(1.0, 0.0)
    past.move_in_with(empty, 4, 1, starts, tiles, one_band, other)
  }
  if !near(past.x(), 16.0) { return 21 }
  if !FEET.hits_any(100.0, 0.0, #[Collider { shape: Shape { x: 0.0, y: 14.0, width: 300.0, height: 8.0 }, layers: 1, mask: 0 }]) { return 22 }

  // A one-way platform on the map, y 20..24 across it: falling, the feet land on it,
  // touching; jumping up from below into it passes; falling from inside it goes on
  // through; walking across a one-way wall passes.
  let platform: [1]Collider = #[Collider { shape: Shape { x: 0.0, y: 20.0, width: 32.0, height: 4.0 }, layers: 1, mask: 0, one_way: true }]
  let faller = EntityKind { HERO | gravity: 0.25 }
  var lands = faller.spawn(0.0, 0.0)
  for _ in 0..<60 {
    lands.push(0.0, 0.0)
    lands.move_in_with(empty, 4, 1, starts, tiles, one_band, platform)
  }
  if !near(lands.y(), 4.0) || !near(lands.vy, 0.0) { return 23 }
  var jumps = faller.spawn(0.0, 14.0)
  jumps.vy = -4.0
  jumps.move_in_with(empty, 4, 1, starts, tiles, one_band, platform)
  if !near(jumps.y(), 10.0) { return 24 }
  var drops = faller.spawn(0.0, 10.0)
  drops.vy = 1.0
  drops.move_in_with(empty, 4, 1, starts, tiles, one_band, platform)
  if !near(drops.y(), 11.0) { return 25 }
  let gate: [1]Collider = #[Collider { shape: Shape { x: 20.0, y: 15.0, width: 8.0, height: 17.0 }, layers: 1, mask: 0, one_way: true }]
  var walks = HERO.spawn(0.0, 0.0)
  for _ in 0..<20 {
    walks.push(1.0, 0.0)
    walks.move_in_with(empty, 4, 1, starts, tiles, one_band, gate)
  }
  if !near(walks.x(), 16.0) { return 26 }
  // A tile's collider one-way too, at cell (2, 2), pixels x 16..24, y 16..24: jumped up into
  // from below, then landed on from above.
  let low: [16]u32 = #[0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2, 0, 0, 0, 0, 0]
  let ledge: [1]Collider = #[Collider { shape: Shape { x: 0.0, y: 0.0, width: 8.0, height: 8.0 }, layers: 1, mask: 0, one_way: true }]
  var under = faller.spawn(12.0, 14.0)
  under.vy = -4.0
  under.move_in(low, 4, 1, starts, ledge)
  if !near(under.y(), 10.0) { return 27 }
  var over = faller.spawn(12.0, 0.0)
  for _ in 0..<60 {
    over.push(0.0, 0.0)
    over.move_in(low, 4, 1, starts, ledge)
  }
  if !near(over.y(), 0.0) || !near(over.vy, 0.0) { return 28 }
  if !FEET.hits_any(0.0, 8.0, platform) || FEET.hits_any_moving(0.0, 8.0, 8.0, platform) { return 29 }

  // Banded: three 64-pixel bands. A post at x 140..148 is band 2's; a beam x 40..100
  // reaches bands 0 and 1, so it is in both. Only the bands a collider reaches are searched.
  let post = Collider { shape: Shape { x: 140.0, y: 0.0, width: 8.0, height: 32.0 }, layers: 1, mask: 0 }
  let beam = Collider { shape: Shape { x: 40.0, y: 12.0, width: 60.0, height: 4.0 }, layers: 1, mask: 0 }
  let bands: [4]u32 = #[0, 1, 2, 3]
  let banded: [3]Collider = #[beam, beam, post]
  if !FEET.hits_banded(132.0, 0.0, bands, banded) { return 30 }
  if FEET.hits_banded(100.0, 0.0, bands, banded) { return 31 }
  if !FEET.hits_banded(50.0, 0.0, bands, banded) || !FEET.hits_banded(60.0, 0.0, bands, banded) { return 32 }
  let only_post: [4]u32 = #[0, 0, 0, 1]
  let post_alone: [1]Collider = #[post]
  if !FEET.hits_banded(130.0, 0.0, only_post, post_alone) { return 33 }
  let edges: [3]u32 = #[0, 1, 2]
  let left_wall = Collider { shape: Shape { x: -20.0, y: 0.0, width: 24.0, height: 32.0 }, layers: 1, mask: 0 }
  let right_wall = Collider { shape: Shape { x: 120.0, y: 0.0, width: 40.0, height: 32.0 }, layers: 1, mask: 0 }
  let walls: [2]Collider = #[left_wall, right_wall]
  if !FEET.hits_banded(-6.0, 0.0, edges, walls) || !FEET.hits_banded(140.0, 0.0, edges, walls) { return 34 }
  let wide: [64]u32 = #[0; 64]
  var walker = HERO.spawn(100.0, 0.0)
  for _ in 0..<40 {
    walker.push(1.0, 0.0)
    walker.move_in_with(wide, 32, 1, starts, tiles, only_post, post_alone)
  }
  if !near(walker.x(), 128.0) || !near(walker.vx, 0.0) { return 35 }
  let second: [1]Collider = #[Collider { shape: Shape { x: 128.0, y: 0.0, width: 8.0, height: 32.0 }, layers: 1, mask: 0 }]
  if !FEET.hits_banded(118.0, 0.0, only_post, second) { return 36 }

  // Open edges (not kept in): the map's edges stop nothing.
  var drop = faller.spawn(0.0, 0.0)
  for _ in 0..<40 {
    drop.push(0.0, 0.0)
    drop.move_in_with(empty, 4, 1, starts, tiles, one_band, none_here, false)
  }
  if drop.y() <= 16.0 { return 37 }
  var out = HERO.spawn(0.0, 0.0)
  for _ in 0..<20 {
    out.push(1.0, 0.0)
    out.move_in_with(empty, 4, 1, starts, tiles, one_band, none_here, false)
  }
  if out.x() <= 16.0 || near(out.vx, 0.0) { return 38 }
  var kept = HERO.spawn(0.0, 0.0)
  for _ in 0..<20 {
    kept.push(1.0, 0.0)
    kept.move_in(empty, 4, 1, starts, tiles)
  }
  if !near(kept.x(), 16.0) { return 39 }
  var back = HERO.spawn(0.0, 0.0)
  for _ in 0..<20 {
    back.push(-1.0, 0.0)
    back.move_in(empty, 4, 1, starts, tiles, false)
  }
  if back.x() >= 0.0 { return 40 }
  var walled = HERO.spawn(0.0, 0.0)
  for _ in 0..<20 {
    walled.push(1.0, 0.0)
    walled.move_in_with(empty, 4, 1, starts, tiles, one_band, wall, false)
  }
  if !near(walled.x(), 8.0) { return 41 }

  // A stick's lean: half a push heads for half the top speed, and easing off from full
  // speed slows to it by friction.
  var stick = HERO.spawn(0.0, 0.0)
  for _ in 0..<10 { stick.push(0.5, 0.0) }
  if !near(stick.vx, 1.0) { return 42 }
  for _ in 0..<10 { stick.push(1.0, 0.0) }
  stick.push(0.5, 0.0)
  if !near(stick.vx, 1.0) { return 43 }
  0
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, stderr, code := captureRun(t, "run", src); code != 0 {
		t.Errorf("case %d failed; stderr: %s", code, stderr)
	}
}

// std.game.camera: the view moves only when what it follows leaves the zone, by just enough,
// never past the world, at whole pixels; `sees` is the screen's edges; show_map and
// scroll_map write and scroll the plane.
func TestRun_GameCameraFollows(t *testing.T) {
	root := repoRoot(t)
	t.Setenv("LYRA_STD", root)
	src := filepath.Join(t.TempDir(), "main.lyra")
	if err := os.WriteFile(src, []byte(`import std.game.camera.{ Camera }
import std.game.planes

let main = () -> u8 => {
  var view = Camera { world_width: 1000.0, world_height: 400.0, view_width: 300.0, view_height: 200.0 }
  view.zone = view.thirds()
  if view.zone.left < 99.5 || view.zone.right > 200.5 { return 1 }
  // Inside the zone: no move.
  view.follow(120.0, 80.0, 16.0, 16.0)
  if view.x > 0.5 || view.y > 0.5 { return 2 }
  // Past its right edge by 30.5: moved just enough, to a whole pixel.
  view.follow(214.5, 80.0, 16.0, 16.0)
  if view.x < 29.5 || view.x > 30.5 { return 3 }
  // Never past the world's edges.
  view.follow(990.0, 390.0, 16.0, 16.0)
  if view.x > 700.5 || view.x < 699.5 || view.y > 200.5 || view.y < 199.5 { return 4 }
  view.follow(0.0, 0.0, 16.0, 16.0)
  if view.x > 0.5 || view.y > 0.5 { return 5 }
  if view.sees(-16.0, 0.0, 16.0, 16.0) || !view.sees(-15.0, 0.0, 16.0, 16.0) || view.sees(300.0, 0.0, 8.0, 8.0) { return 6 }
  let cells: [4]u32 = #[1, 2, 3, 4]
  view.follow(500.0, 0.0, 16.0, 16.0)
  view.show_map(planes.PLANE_B, cells, 2)
  let shown = planes.plane(planes.PLANE_B)
  if shown.width != 2 || shown.cells.len() != 4 || shown.cells[3] != 4 { return 7 }
  if shown.x < 1.0 { return 8 }
  0
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, stderr, code := captureRun(t, "run", src); code != 0 {
		t.Errorf("case %d failed; stderr: %s", code, stderr)
	}
}

// examples/game/walker.lyra played as Vega's checks play a game: headless (SDL's dummy
// driver, the software renderer), 30 frames holding right, the last written as a PPM. The
// backdrop shows in the field's border, grass in plane B, and the hero — the screen's
// middle, x 152 — has walked 60 pixels: 2 an update, one update a frame under --frames.
func TestRun_GameWalkerPlaysItself(t *testing.T) {
	frame := playGame(t, filepath.Join(repoRoot(t), "examples", "game", "walker.lyra"),
		"--frames", "30", "--hold", "right")
	if frame.w != 320 || frame.h != 224 {
		t.Fatalf("the frame is %dx%d, want 320x224 (the screen at scale 1)", frame.w, frame.h)
	}
	for _, c := range []struct {
		x, y int
		want [3]byte
		what string
	}{
		{2, 2, [3]byte{32, 64, 160}, "the backdrop, in the border"},
		{212 + 5, 104 + 4, [3]byte{0xE0, 0x30, 0x20}, "the hero, 60 pixels right of the middle"},
		{212 + 9, 104 + 5, [3]byte{0xFF, 0xFF, 0xFF}, "the hero's eye"},
	} {
		if got := frame.at(c.x, c.y); got != c.want {
			t.Errorf("%s: (%d, %d) is %v, want %v", c.what, c.x, c.y, got, c.want)
		}
	}
	if got := frame.at(152+5, 104+4); got == [3]byte{0xE0, 0x30, 0x20} {
		t.Errorf("the hero is still in the middle: he did not walk")
	}
}

// std.game.screen composes a frame in the Genesis's order: plane B, plane A, the sprites,
// then the planes' front cells, then the front sprites. A sprite covers a cell behind it; a
// front cell covers the sprite; a front sprite covers the front cell. A cell's FLIP_X
// mirrors its tile, and its palette picks which art its tile is drawn from. Four 8×8 cells a
// row, two rows; tile 2 is black on its left half, white on its right; tile 1 is green in
// palette 0's art and blue in palette 1's.
func TestRun_GameScreenComposesInPriorityOrder(t *testing.T) {
	src := filepath.Join(t.TempDir(), "main.lyra")
	if err := os.WriteFile(src, []byte(`import std.game.screen
import std.game.planes
import std.game.planes.{ FRONT, FLIP_X, palette }
import std.game.sprites
import std.game.sprites.{ Sprite }

let main = () -> u8 => {
  if !screen.open("layers", 32, 16) { return 2 }
  var tiles: [192]u32 = #[0; 192]
  for row in 0..<8 {
    for column in 0..<8 {
      tiles[row * 24 + 8 + column] = 0xFF00FF00
      tiles[row * 24 + 16 + column] = if column < 4 { 0xFF000000 } else { 0xFFFFFFFF }
    }
  }
  var art: [64]u32 = #[0xFFFF0000; 64]
  screen.load_tiles(3, tiles)
  var blue = tiles
  for row in 0..<8 {
    for column in 0..<8 { blue[row * 24 + 8 + column] = 0xFF0000FF }
  }
  screen.load_tiles(3, blue, 1)
  screen.load_sprites(8, art)
  let behind: [8]u32 = #[1, 1, 1, palette(1) | 1, 0, 2, FLIP_X | 2, 0]
  let front: [8]u32 = #[0, FRONT | 2, FRONT | 2, 0, 0, 0, 0, 0]
  planes.write_plane(planes.PLANE_B, behind, 4)
  planes.write_plane(planes.PLANE_A, front, 4)
  while screen.running() {
    sprites.clear()
    sprites.add(Sprite { x: 0.0, y: 0.0, width: 8.0, height: 8.0, sheet_x: 0.0, sheet_y: 0.0 })
    sprites.add(Sprite { x: 8.0, y: 0.0, width: 8.0, height: 8.0, sheet_x: 0.0, sheet_y: 0.0 })
    sprites.add(Sprite { x: 16.0, y: 0.0, width: 8.0, height: 8.0, sheet_x: 0.0, sheet_y: 0.0, front: true })
    screen.show()
  }
  screen.close()
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	frame := playGame(t, src, "--frames", "1")
	red, white, black, blue := [3]byte{255, 0, 0}, [3]byte{255, 255, 255}, [3]byte{0, 0, 0}, [3]byte{0, 0, 255}
	for _, c := range []struct {
		x, y int
		want [3]byte
		what string
	}{
		{2, 2, red, "a sprite over plane B's grass"},
		{9, 2, black, "a front cell over a sprite (its left half)"},
		{13, 2, white, "a front cell over a sprite (its right half)"},
		{21, 2, red, "a front sprite over a front cell"},
		{26, 2, blue, "plane B in palette 1, where nothing is in front"},
		{9, 10, black, "tile 2 as drawn"},
		{13, 10, white, "tile 2 as drawn"},
		{17, 10, white, "tile 2 mirrored"},
		{21, 10, black, "tile 2 mirrored"},
	} {
		if got := frame.at(c.x, c.y); got != c.want {
			t.Errorf("%s: (%d, %d) is %v, want %v", c.what, c.x, c.y, got, c.want)
		}
	}
}

// ppm is a frame a game wrote with --ppm.
type ppm struct {
	w, h int
	rgb  []byte
}

func (p ppm) at(x, y int) [3]byte {
	i := (y*p.w + x) * 3
	return [3]byte{p.rgb[i], p.rgb[i+1], p.rgb[i+2]}
}

// playGame builds the std.game program at src and plays it headless with args, answering
// the last frame — skipping without SDL3, or where SDL cannot open its dummy window.
func playGame(t *testing.T, src string, args ...string) ppm {
	t.Helper()
	if err := exec.Command("pkg-config", "--exists", "sdl3").Run(); err != nil {
		t.Skip("no SDL3")
	}
	t.Setenv("LYRA_STD", repoRoot(t))
	dir := t.TempDir()
	bin := filepath.Join(dir, "game")
	if _, stderr, code := captureRun(t, "build", "-o", bin, src); code != 0 {
		t.Fatalf("building exited %d\nstderr: %s", code, stderr)
	}
	shot := filepath.Join(dir, "frame.ppm")
	cmd := exec.Command(bin, append(args, "--ppm", shot)...)
	cmd.Env = append(os.Environ(), "SDL_VIDEODRIVER=dummy", "SDL_RENDER_DRIVER=software")
	out, err := cmd.CombinedOutput()
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 2 {
		t.Skipf("SDL could not open a dummy window here: %s", out)
	} else if err != nil {
		t.Fatalf("playing: %v\n%s", err, out)
	}
	data, err := os.ReadFile(shot)
	if err != nil {
		t.Fatal(err)
	}
	var p ppm
	var maxval int
	n, err := fmt.Sscanf(string(data), "P6\n%d %d\n%d\n", &p.w, &p.h, &maxval)
	if err != nil || n != 3 || maxval != 255 {
		t.Fatalf("not a PPM: %v", err)
	}
	header := []byte(fmt.Sprintf("P6\n%d %d\n255\n", p.w, p.h))
	if !bytes.HasPrefix(data, header) || len(data) != len(header)+p.w*p.h*3 {
		t.Fatalf("a PPM of %d bytes for %dx%d", len(data), p.w, p.h)
	}
	p.rgb = data[len(header):]
	return p
}
