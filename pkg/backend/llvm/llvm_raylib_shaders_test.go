package llvm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `bindings/raylib/shaders.lyra` and the per-material half of `models.lyra`, under a hidden
// window like TestExec_RaylibModels.
//
// Each field of the report pins one claim: a generated cube has normals and is drawn with
// material 0; its albedo slot holds raylib's default texture and its normal slot nothing,
// until a texture is set there; a shader that fails to compile is `None` rather than the
// default shader raylib quietly substitutes; `bind_shader_map` finds a sampler the shader
// reads and reports one it does not have; and a material can be put on a shader and back.
// The run ending cleanly is the rest of the claim — `unload_model` frees the texture the
// model was handed, and a double free of it would abort.
func TestExec_RaylibShadersAndMaterials(t *testing.T) {
	t.Parallel()
	libdir := pkgConfigLibDir(t, "raylib")
	src := `
module main
import bindings.raylib.{ set_config_flags, FLAG_WINDOW_HIDDEN, init_window, close_window,
                         window_ready, gen_mesh_cube, model_from_mesh, unload_model,
                         gen_image_color, texture_from_image, unload_image, rgb,
                         model_mesh_has_normals, model_mesh_material, model_material_has_texture,
                         set_model_material_texture, set_model_material_shader,
                         reset_model_material_shader, load_shader_from_memory,
                         bind_shader_map, shader_location, unload_shader,
                         MATERIAL_MAP_ALBEDO, MATERIAL_MAP_NORMAL, MATERIAL_MAP_OCCLUSION }

let vertex = "#version 330\nin vec3 vertexPosition;\nuniform mat4 mvp;\nvoid main() { gl_Position = mvp * vec4(vertexPosition, 1.0); }"
let fragment = "#version 330\nuniform sampler2D occlusionMap;\nout vec4 finalColor;\nvoid main() { finalColor = texture(occlusionMap, vec2(0.5)); }"

let main = () -> void => {
  set_config_flags(FLAG_WINDOW_HIDDEN)
  init_window(64, 64, "shaders test")
  if !window_ready() {
    println("no-display")
    return
  }
  var report = ""
  match gen_mesh_cube(1.0, 1.0, 1.0) {
    Some(cube) => match model_from_mesh(cube) {
      Some(model) => {
        report = "${model_mesh_has_normals(model, 0)} ${model_mesh_material(model, 0)}" ++
          " ${model_material_has_texture(model, 0, MATERIAL_MAP_ALBEDO)}" ++
          " ${model_material_has_texture(model, 0, MATERIAL_MAP_NORMAL)}"
        let image = gen_image_color(1, 1, rgb(128, 128, 255))
        match texture_from_image(image) {
          Some(t) => set_model_material_texture(model, 0, MATERIAL_MAP_NORMAL, t),
          None => { },
        }
        unload_image(image)
        report = report ++ " ${model_material_has_texture(model, 0, MATERIAL_MAP_NORMAL)}"
        report = report ++ " ${load_shader_from_memory(vertex, "not glsl").is_none()}"
        match load_shader_from_memory(vertex, fragment) {
          Some(s) => {
            report = report ++ " ${bind_shader_map(s, MATERIAL_MAP_OCCLUSION, "occlusionMap")}" ++
              " ${bind_shader_map(s, MATERIAL_MAP_OCCLUSION, "noSuchSampler")}" ++
              " ${shader_location(s, "mvp").is_some()}"
            set_model_material_shader(model, 0, s)
            reset_model_material_shader(model, 0)
            unload_shader(s)
          },
          None => { report = report ++ " no-shader" },
        }
        unload_model(model)
      },
      None => { report = "no-model" },
    },
    None => { report = "no-cube" },
  }
  close_window()
  println(report)
}
`
	bin := compileCached(t, lookClang(t), emitWithPrelude(t, src), "-L"+libdir, "-lraylib")
	raw, err := command(bin).Output()
	if err != nil {
		t.Fatalf("running the raylib-shaders binary failed: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	got := strings.TrimSpace(lines[len(lines)-1])
	if got == "no-display" {
		t.Skip("no display to create a GL context on")
	}
	want := "true 0 true false true true true false true"
	if got != want {
		t.Errorf("raylib shaders and materials = %q; want %q", got, want)
	}
}

// runHiddenWindow builds a raylib program and answers its last line, skipping where there
// is no display to create a GL context on.
func runHiddenWindow(t *testing.T, src string) string {
	t.Helper()
	libdir := pkgConfigLibDir(t, "raylib")
	bin := compileCached(t, lookClang(t), emitWithPrelude(t, src), "-L"+libdir, "-lraylib")
	raw, err := command(bin).Output()
	if err != nil {
		t.Fatalf("running the raylib binary failed: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	got := strings.TrimSpace(lines[len(lines)-1])
	if got == "no-display" {
		t.Skip("no display to create a GL context on")
	}
	return got
}

// Every uniform setter in `shaders.lyra`, read back as a pixel. Twelve one-pixel columns
// are drawn into a render texture through one shader, each in a shader mode of its own
// with `which` naming the uniform its red channel shows: a float, the vectors' last
// components, an int, each array's second element (the first is 0.9, so an array sent
// from the wrong element or as the wrong type reads otherwise), a matrix's translation
// (`m[3][0]`), and a sampler's texel. Each expected byte is the value times 255, rounded.
func TestExec_RaylibShaderUniforms(t *testing.T) {
	t.Parallel()
	src := `
module main
import bindings.raylib.{ set_config_flags, FLAG_WINDOW_HIDDEN, init_window, close_window,
                         window_ready, load_shader_from_memory, unload_shader, shader_location,
                         set_shader_float, set_shader_vec2, set_shader_vec3, set_shader_vec4,
                         set_shader_int, set_shader_floats, set_shader_vec2s, set_shader_vec3s,
                         set_shader_vec4s, set_shader_ints, set_shader_matrix, set_shader_texture,
                         begin_shader_mode, end_shader_mode, load_render_texture,
                         unload_render_texture, begin_texture_mode, end_texture_mode,
                         clear_background, draw_rectangle, rect, image_from_texture,
                         image_color_at, unload_image, gen_image_color, texture_from_image,
                         unload_texture, rgb, vec2, vec3, Vector4, matrix_translation, Shader }

let vertex = "#version 330\nin vec3 vertexPosition;\nuniform mat4 mvp;\nvoid main() { gl_Position = mvp * vec4(vertexPosition, 1.0); }"
let fragment = "#version 330
uniform int which;
uniform float a; uniform vec2 b; uniform vec3 c; uniform vec4 d; uniform int n;
uniform float fs[2]; uniform vec2 v2s[2]; uniform vec3 v3s[2]; uniform vec4 v4s[2]; uniform int ns[2];
uniform mat4 m; uniform sampler2D tex;
out vec4 finalColor;
void main() {
  float r = 0.0;
  if (which == 0) r = a;
  else if (which == 1) r = b.y;
  else if (which == 2) r = c.z;
  else if (which == 3) r = d.w;
  else if (which == 4) r = float(n) / 8.0;
  else if (which == 5) r = fs[1];
  else if (which == 6) r = v2s[1].y;
  else if (which == 7) r = v3s[1].z;
  else if (which == 8) r = v4s[1].w;
  else if (which == 9) r = float(ns[1]) / 16.0;
  else if (which == 10) r = m[3][0];
  else if (which == 11) r = texture(tex, vec2(0.5)).g;
  finalColor = vec4(r, 0.0, 0.0, 1.0);
}"

let loc = (s: Shader, name: string) -> i32 => shader_location(s, name) ?? -1

let main = () -> void => {
  set_config_flags(FLAG_WINDOW_HIDDEN)
  init_window(64, 64, "uniforms test")
  if !window_ready() {
    println("no-display")
    return
  }
  let Some(s) = load_shader_from_memory(vertex, fragment) else {
    println("no-shader")
    close_window()
    return
  }
  let Some(target) = load_render_texture(12, 4) else {
    unload_shader(s)
    close_window()
    println("no-target")
    return
  }
  let green = gen_image_color(1, 1, rgb(0, 200, 0))
  let Some(tex) = texture_from_image(green) else {
    unload_image(green)
    unload_render_texture(target)
    unload_shader(s)
    close_window()
    println("no-texture")
    return
  }
  unload_image(green)
  let other = Vector4 { x: 0.9, y: 0.9, z: 0.9, w: 0.9 }
  set_shader_float(s, loc(s, "a"), 0.25)
  set_shader_vec2(s, loc(s, "b"), vec2(0.9, 0.5))
  set_shader_vec3(s, loc(s, "c"), vec3(0.9, 0.9, 0.75))
  set_shader_vec4(s, loc(s, "d"), Vector4 { x: 0.9, y: 0.9, z: 0.9, w: 0.375 })
  set_shader_int(s, loc(s, "n"), 5)
  set_shader_floats(s, loc(s, "fs"), [0.9, 0.125])
  set_shader_vec2s(s, loc(s, "v2s"), [vec2(0.9, 0.9), vec2(0.9, 0.875)])
  set_shader_vec3s(s, loc(s, "v3s"), [vec3(0.9, 0.9, 0.9), vec3(0.9, 0.9, 0.0625)])
  set_shader_vec4s(s, loc(s, "v4s"), [other, Vector4 { x: 0.9, y: 0.9, z: 0.9, w: 0.3125 }])
  set_shader_ints(s, loc(s, "ns"), [3, 9])
  set_shader_matrix(s, loc(s, "m"), matrix_translation(vec3(0.4375, 0.9, 0.9)))
  begin_texture_mode(target)
  clear_background(rgb(0, 0, 0))
  for i in 0..<12 {
    begin_shader_mode(s)
    set_shader_int(s, loc(s, "which"), i32(i))
    set_shader_texture(s, loc(s, "tex"), tex)
    draw_rectangle(rect(f32(i), 0.0, 1.0, 4.0), rgb(255, 255, 255))
    end_shader_mode()
  }
  end_texture_mode()
  var report = ""
  match image_from_texture(target.texture) {
    Some(img) => {
      for i in 0..<12 { report = report ++ " ${image_color_at(img, i32(i), 1).r}" }
      unload_image(img)
    },
    None => { report = " no-readback" },
  }
  unload_texture(tex)
  unload_render_texture(target)
  unload_shader(s)
  close_window()
  println(report)
}
`
	want := "64 128 191 96 159 32 223 16 80 143 112 200"
	if got := runHiddenWindow(t, src); got != want {
		t.Errorf("uniforms read back = %q; want %q", got, want)
	}
}

// `load_texture_cubemap`: a 10x10 image fits no layout and is `None`; a 16x12 cross is
// detected and its faces are 4 pixels. Each face is then sampled through a model's
// `MATERIAL_MAP_CUBEMAP` slot and a `samplerCube` named by `bind_shader_map`, one column
// per direction (`+X -X +Y -Y +Z -Z`), and each must read the colour painted into the cell
// the CUBEMAP_LAYOUT note gives it — so the note is pinned as well as the binding.
func TestExec_RaylibCubemap(t *testing.T) {
	t.Parallel()
	src := `
module main
import bindings.raylib.{ set_config_flags, FLAG_WINDOW_HIDDEN, init_window, close_window,
                         window_ready, load_shader_from_memory, unload_shader, shader_location,
                         set_shader_float, set_shader_vec3, bind_shader_map, gen_mesh_cube,
                         model_from_mesh, unload_model, set_model_material_shader,
                         reset_model_material_shader, set_model_material_texture, draw_model,
                         load_render_texture, unload_render_texture, begin_texture_mode,
                         end_texture_mode, clear_background, image_from_texture, image_color_at,
                         unload_image, gen_image_color, rgb, vec3, rect, Image,
                         load_texture_cubemap, unload_texture, MATERIAL_MAP_CUBEMAP,
                         CUBEMAP_LAYOUT_AUTO_DETECT }

let vertex = "#version 330
in vec3 vertexPosition;
uniform float slot;
void main() { gl_Position = vec4((vertexPosition.x + 0.5 + slot) / 3.0 - 1.0, vertexPosition.y * 2.0, 0.0, 1.0); }"
let fragment = "#version 330
uniform samplerCube environmentMap;
uniform vec3 dir;
out vec4 finalColor;
void main() { finalColor = vec4(texture(environmentMap, dir).rgb, 1.0); }"

// The cross's cells, by the layout the constants' note gives: +X -X +Y -Y +Z -Z.
let cells = [(2, 1), (0, 1), (1, 0), (1, 2), (1, 1), (3, 1)]
let dirs = [vec3(1.0, 0.0, 0.0), vec3(-1.0, 0.0, 0.0), vec3(0.0, 1.0, 0.0),
            vec3(0.0, -1.0, 0.0), vec3(0.0, 0.0, 1.0), vec3(0.0, 0.0, -1.0)]

let cross = () -> Image => {
  var image = gen_image_color(16, 12, rgb(0, 0, 0))
  var k = 0
  for (cx, cy) in cells {
    k += 1
    image.paint_rectangle(rect(f32(cx * 4), f32(cy * 4), 4.0, 4.0), rgb(u8(k * 40), 0, 0))
  }
  image
}

let cubemap_report = () -> string => {
  let bad = gen_image_color(10, 10, rgb(0, 0, 0))
  var report = "${load_texture_cubemap(bad, CUBEMAP_LAYOUT_AUTO_DETECT).is_none()}"
  unload_image(bad)
  let image = cross()
  let found = load_texture_cubemap(image, CUBEMAP_LAYOUT_AUTO_DETECT)
  unload_image(image)
  let Some(cube) = found else { return report ++ " no-cubemap" }
  report = report ++ " ${cube.width}x${cube.height}"
  let Some(s) = load_shader_from_memory(vertex, fragment) else {
    unload_texture(cube)
    return report ++ " no-shader"
  }
  let Some(target) = load_render_texture(6, 4) else {
    unload_texture(cube)
    unload_shader(s)
    return report ++ " no-target"
  }
  match gen_mesh_cube(1.0, 1.0, 1.0) {
    Some(mesh) => match model_from_mesh(mesh) {
      Some(model) => {
        report = report ++ " ${bind_shader_map(s, MATERIAL_MAP_CUBEMAP, "environmentMap")}"
        set_model_material_shader(model, 0, s)
        set_model_material_texture(model, 0, MATERIAL_MAP_CUBEMAP, cube)
        begin_texture_mode(target)
        clear_background(rgb(0, 0, 0))
        for i in 0..<6 {
          set_shader_float(s, shader_location(s, "slot") ?? -1, f32(i))
          set_shader_vec3(s, shader_location(s, "dir") ?? -1, dirs[i])
          draw_model(model, vec3(0.0, 0.0, 0.0), 1.0, rgb(255, 255, 255))
        }
        end_texture_mode()
        match image_from_texture(target.texture) {
          Some(img) => {
            report = report ++ " faces"
            for i in 0..<6 { report = report ++ " ${image_color_at(img, i32(i), 1).r}" }
            unload_image(img)
          },
          None => { report = report ++ " no-readback" },
        }
        reset_model_material_shader(model, 0)
        unload_model(model)
      },
      None => {
        unload_texture(cube)
        report = report ++ " no-model"
      },
    },
    None => {
      unload_texture(cube)
      report = report ++ " no-mesh"
    },
  }
  unload_render_texture(target)
  unload_shader(s)
  report
}

let main = () -> void => {
  set_config_flags(FLAG_WINDOW_HIDDEN)
  init_window(64, 64, "cubemap test")
  if !window_ready() {
    println("no-display")
    return
  }
  let report = cubemap_report()
  close_window()
  println(report)
}
`
	want := "true 4x4 true faces 40 80 120 160 200 240"
	if got := runHiddenWindow(t, src); got != want {
		t.Errorf("cubemap = %q; want %q", got, want)
	}
}

// `load_materials` over an `.mtl` file: a missing file is `None`; two materials load,
// each valid with its `Kd` as the albedo colour; `material_in` binds one with no
// obligation (no W022 here); and `unload_materials` frees each and then raylib's array
// through `MemFree` — a double free would abort the run.
func TestExec_RaylibLoadMaterials(t *testing.T) {
	t.Parallel()
	mtl := filepath.Join(t.TempDir(), "two.mtl")
	if err := os.WriteFile(mtl, []byte("newmtl red\nKd 1 0 0\nnewmtl blue\nKd 0 0 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := strings.ReplaceAll(`
module main
import bindings.raylib.{ set_config_flags, FLAG_WINDOW_HIDDEN, init_window, close_window,
                         window_ready, load_materials, unload_materials, material_in,
                         material_color, material_valid, MATERIAL_MAP_ALBEDO }

let materials_report = (path: string) -> string => {
  var report = "${load_materials(path ++ ".missing").is_none()}"
  match load_materials(path) {
    Some(set) => {
      report = report ++ " ${set.count}"
      for i in 0..<i64(set.count) {
        let m = material_in(set, i)
        let c = material_color(m, MATERIAL_MAP_ALBEDO)
        report = report ++ " ${material_valid(m)} ${c.r},${c.g},${c.b}"
      }
      unload_materials(set)
    },
    None => { report = report ++ " none" },
  }
  report
}

let main = () -> void => {
  set_config_flags(FLAG_WINDOW_HIDDEN)
  init_window(64, 64, "materials test")
  if !window_ready() {
    println("no-display")
    return
  }
  let report = materials_report("MTL_PATH")
  close_window()
  println(report)
}
`, "MTL_PATH", mtl)
	want := "true 2 true 255,0,0 true 0,0,255"
	if got := runHiddenWindow(t, src); got != want {
		t.Errorf("materials = %q; want %q", got, want)
	}
}
