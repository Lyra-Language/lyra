package main

import (
	"os"
	"path/filepath"
	"testing"
)

// std.genesis.entity's tests are plain Lyra, so they run on the host: each case answers
// its own exit code. A push speeds an entity up by its acceleration to its top speed and
// friction stops it; gravity falls, at most MAX_FALL; moving in a map stops an axis at a
// wall on a layer its colliders look for and at the map's edge, sliding along the other;
// turning beside a wall leaves its colliders unturned until they fit, so it is not stuck
// (10/02: the hero, walking down after walking left into a rock, could not move); and
// meeting is one-sided. Pushed both ways at once it goes no faster than its top speed
// (10/02: the hero walked diagonally 1.4 times as fast).
func TestRun_EntitiesMoveCollideAndMeet(t *testing.T) {
	root := repoRoot(t)
	t.Setenv("LYRA_STD", root)
	src := filepath.Join(t.TempDir(), "main.lyra")
	if err := os.WriteFile(src, []byte(`import std.genesis.collision
import std.genesis.collision.{ Shape, Collider }
import std.genesis.entity.{ Animation, EntityKind, MAX_FALL, visible }

const STILL: Animation = Animation { sprite: 0, first: 0, count: 1, total: 1, looping: false }
const NONE: Collider = Collider { shape: Shape { x: 0, y: 0, width: 0, height: 0 }, layers: 0, mask: 0 }
// Feet off the sprite's middle, as the hero's are: x 2..12 of 16, 4..14 mirrored.
const FEET: Collider = Collider { shape: Shape { x: 2, y: 12, width: 10, height: 4 }, layers: 4, mask: 1 }
const HERO: EntityKind = EntityKind {
  width: 16,
  height: 16,
  animation: STILL,
  colliders: #[FEET, NONE, NONE, NONE],
  collider_count: 1,
  max_speed: 512,
  acceleration: 128,
  friction: 256,
  gravity: 0
}
const STAR: EntityKind = EntityKind {
  width: 8,
  height: 8,
  animation: STILL,
  colliders: #[Collider { shape: Shape { x: 0, y: 0, width: 8, height: 8 }, layers: 8, mask: 0 }, NONE, NONE, NONE],
  collider_count: 1,
  max_speed: 0,
  acceleration: 0,
  friction: 0,
  gravity: 0
}

let main = () -> u8 => {
  // Speeding up: 128 a frame to 512, then held there; friction 256 a frame to a stop.
  var e = HERO.spawn(10, 20)
  if e.x() != 10 || e.y() != 20 || e.vx != 0 { return 1 }
  e.push(1, 0)
  if e.vx != 128 { return 2 }
  for _ in 0..<10 { e.push(1, 0) }
  if e.vx != 512 { return 3 }
  e.push(0, 0)
  e.push(0, 0)
  if e.vx != 0 { return 4 }
  // Pushed both ways, each axis gains the acceleration over √2: 90.
  e.push(-1, -1)
  if e.vx != -90 || e.vy != -90 { return 5 }
  e.move()
  if e.fx != (10 << 8) - 90 || e.x() != 9 { return 6 }
  // Both ways at once: each axis's top speed and acceleration over √2 (181/256), so the
  // diagonal is the top speed — 362 a frame each way, about 512 along it.
  var diagonal = HERO.spawn(0, 0)
  diagonal.push(1, 1)
  if diagonal.vx != 90 || diagonal.vy != 90 { return 17 }
  for _ in 0..<10 { diagonal.push(1, -1) }
  if diagonal.vx != 362 || diagonal.vy != -362 { return 18 }
  // One axis let go: the other goes on to the full top speed.
  for _ in 0..<10 { diagonal.push(1, 0) }
  if diagonal.vx != 512 || diagonal.vy != 0 { return 19 }
  // Gravity: the vertical axis falls, whatever is pushed, at most MAX_FALL.
  var falling = EntityKind { HERO | gravity: 512 }.spawn(0, 0)
  for _ in 0..<10 { falling.push(0, -1) }
  if falling.vy != MAX_FALL { return 7 }

  // A 4×4 map of 8-pixel cells: a wall (the set's tile 1, its collider on layer 1) at
  // cell (2, 1), so pixels x 16..24, y 8..16.
  let cells: [16]u16 = #[0, 0, 0, 0, 0, 0, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0]
  let starts: [3]u16 = #[0, 0, 1]
  let tiles: [1]Collider = #[Collider { shape: Shape { x: 0, y: 0, width: 8, height: 8 }, layers: 1, mask: 0 }]
  // Walking right into it, the feet (y 12..16) at the wall's rows: stopped with the feet
  // touching it, x speed gone.
  var w = HERO.spawn(0, 0)
  for _ in 0..<20 {
    w.push(1, 0)
    w.move_in(cells, 4, 1, starts, tiles)
  }
  if w.x() != 4 || w.vx != 0 { return 8 }
  // Pushed right and down along it: the x axis stays stopped, the y axis slides on.
  let before = w.y()
  for _ in 0..<4 {
    w.push(1, 1)
    w.move_in(cells, 4, 1, starts, tiles)
  }
  if w.x() != 4 || w.y() <= before { return 9 }
  // The map's edge stops it as a wall does.
  var edge = HERO.spawn(0, 16)
  edge.push(-1, 0)
  edge.move_in(cells, 4, 1, starts, tiles)
  if edge.x() != 0 || edge.vx != 0 { return 10 }

  // Against the wall facing right (feet x 6..16), it turns left: mirrored, its feet would
  // be x 8..18, into the wall. They stay unturned, and walking down it gets past.
  var t = HERO.spawn(4, 0)
  t.mirrored = true
  for _ in 0..<30 {
    t.push(0, 1)
    t.move_in(cells, 4, 1, starts, tiles)
  }
  if t.y() < 10 { return 11 }
  if t.hit_mirrored != true { return 12 }

  // Meeting is one-sided: the star looks for nothing, the hero's feet look for layer 1 —
  // not the star's 8 — so neither meets the other until one looks for it.
  let star = STAR.spawn(4, 12)
  var hero = HERO.spawn(0, 0)
  if hero.meets(star) || star.meets(hero) { return 13 }
  var looking = EntityKind { HERO | colliders: #[Collider { FEET | mask: 8 }, NONE, NONE, NONE] }.spawn(0, 0)
  if !looking.meets(star) || star.meets(looking) { return 14 }
  // Facing the other way mirrors its colliders: feet x 4..14 still meet the star at 4..12.
  looking.mirrored = true
  if !looking.meets(star) { return 15 }
  if visible(-16, 0, 16, 16) || !visible(-15, 0, 16, 16) || visible(320, 0, 8, 8) { return 16 }

  // The map's own colliders (move_in_with): a box drawn on the map in its pixels, x 20..28,
  // on layer 1 — with no tile under it — stops the feet as a tile's would; one on layer 2,
  // which the feet do not look for, does not.
  let empty: [16]u16 = #[0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0]
  let wall: [1]Collider = #[Collider { shape: Shape { x: 20, y: 0, width: 8, height: 32 }, layers: 1, mask: 0 }]
  var m = HERO.spawn(0, 0)
  for _ in 0..<20 {
    m.push(1, 0)
    m.move_in_with(empty, 4, 1, starts, tiles, wall)
  }
  if m.x() != 8 || m.vx != 0 { return 20 }
  let other: [1]Collider = #[Collider { shape: Shape { x: 20, y: 0, width: 8, height: 32 }, layers: 2, mask: 0 }]
  var past = HERO.spawn(0, 0)
  for _ in 0..<20 {
    past.push(1, 0)
    past.move_in_with(empty, 4, 1, starts, tiles, other)
  }
  if past.x() != 16 { return 21 }
  // A large box is compared whole: a floor 300 pixels wide.
  if !FEET.hits_any(100, 0, #[Collider { shape: Shape { x: 0, y: 14, width: 300, height: 8 }, layers: 1, mask: 0 }]) { return 22 }
  0
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, stderr, code := captureRun(t, "run", src); code != 0 {
		t.Errorf("case %d failed; stderr: %s", code, stderr)
	}
}
