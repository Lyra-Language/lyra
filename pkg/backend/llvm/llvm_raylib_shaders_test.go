package llvm

import (
	"os/exec"
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
	raw, err := exec.Command(bin).Output()
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
