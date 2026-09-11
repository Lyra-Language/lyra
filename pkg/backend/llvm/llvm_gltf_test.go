package llvm

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/driver"
	"github.com/Lyra-Language/lyra/pkg/modules"
)

// `examples/raylib/gltf.lyra` plays the glTF node animations raylib does not, and what
// it answers is a matrix per mesh — so these check matrices against ones worked out by hand,
// for a GLB this test writes itself: node 0 above node 1, node 1 carrying the mesh at rest
// one unit along x (which raylib bakes into the vertices), and two clips.
//
//   - "move": node 0 translates (0,0,0) → (4,0,0) over 2 s, LINEAR; node 1 turns 180° about
//     z, STEP. At 1 s the mesh has moved 2 and not yet turned. At 2 s it has moved 4 and
//     turned: the matrix takes the baked vertex back by its one-unit rest offset, turns it,
//     and puts it at node 1's place, 5 along x — so a vertex v lands at R·v + 6 (the turn
//     flips the undone offset), and m0 = -1, m12 = 6. The mesh's rest origin, (1,0,0),
//     lands at 5, which is the placement the clip describes.
//   - "grow": node 0 scales 1 → 3 over 1 s, CUBICSPLINE with flat tangents, so at 0.5 s
//     the Hermite curve is exactly halfway: m0 = 2, m12 = 0.
//
// raylib is linked (the reader uses its file loader and raymath) but no window opens.
func TestExec_GltfNodeAnimation(t *testing.T) {
	t.Parallel()
	libdir := pkgConfigLibDir(t, "raylib")
	glb := filepath.Join(t.TempDir(), "moving.glb")
	if err := os.WriteFile(glb, testNodeAnimationGLB(t), 0o644); err != nil {
		t.Fatal(err)
	}
	src := `
module main
import gltf.{ load_node_animation, node_clip_count, node_clip_name,
                                   node_clip_duration, node_mesh_count, pose }
let at = (m: bindings.raylib.Matrix) -> string => "${f64(m.m0).to_fixed(2)} ${f64(m.m12).to_fixed(2)}"
let main = () -> void => {
  match load_node_animation("` + glb + `") {
    Some(a) => {
      println("${node_clip_count(a)} ${node_mesh_count(a)} ${node_clip_name(a, 0)} ${f64(node_clip_duration(a, 0)).to_fixed(2)} ${node_clip_name(a, 1)}")
      println("${at(pose(a, 0, 0.0)[0])} | ${at(pose(a, 0, 1.0)[0])} | ${at(pose(a, 0, 2.0)[0])} | ${at(pose(a, 0, 9.0)[0])}")
      println("${at(pose(a, 1, 0.5)[0])}")
    },
    None => println("none"),
  }
}
`
	src = strings.Replace(src, "(m: bindings.raylib.Matrix)", "(m: Matrix)", 1)
	src = strings.Replace(src, "import gltf.{", "import bindings.raylib.{ Matrix }\nimport gltf.{", 1)
	bin := compileCached(t, lookClang(t), emitWithSibling(t, src, "examples/raylib/gltf.lyra"), "-L"+libdir, "-lraylib")
	raw, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("running the gltf-anim binary failed: %v", err)
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if !strings.HasPrefix(l, "INFO:") && !strings.HasPrefix(l, "WARNING:") {
			lines = append(lines, l)
		}
	}
	got := strings.Join(lines, "\n")
	want := "2 1 move 2.00 grow\n1.00 0.00 | 1.00 2.00 | -1.00 6.00 | -1.00 6.00\n2.00 0.00"
	if got != want {
		t.Errorf("node animation poses:\n%s\nwant:\n%s", got, want)
	}
}

// What `load_material_params` reads out of a material, beside what raylib keeps: a texture's
// UV set and KHR_texture_transform (and the sampler's wrap mode, as raylib's constant), the
// normal and occlusion strengths, the factors, the three material extensions it knows, and
// the alpha mode — and glTF's defaults for a material that says nothing, which matter because
// they are not raylib's (metallic 1, roughness 1). A `.gltf` of JSON alone is enough: no
// buffer or image is read.
func TestExec_GltfMaterialParams(t *testing.T) {
	t.Parallel()
	libdir := pkgConfigLibDir(t, "raylib")
	path := filepath.Join(t.TempDir(), "materials.gltf")
	doc := `{
  "asset": {"version": "2.0"},
  "samplers": [{"wrapS": 33071, "wrapT": 33071}, {"wrapS": 33648, "wrapT": 33648}],
  "images": [{"uri": "unused.png"}],
  "textures": [{"source": 0, "sampler": 0}, {"source": 0, "sampler": 1}, {"source": 0}],
  "materials": [
    {"pbrMetallicRoughness": {"metallicFactor": 0.25, "roughnessFactor": 0.5,
       "baseColorTexture": {"index": 0, "texCoord": 1,
         "extensions": {"KHR_texture_transform": {"offset": [0.5, 0], "rotation": 0.25, "scale": [2, 3]}}},
       "metallicRoughnessTexture": {"index": 1}},
     "normalTexture": {"index": 2, "scale": 0.3},
     "occlusionTexture": {"index": 2, "texCoord": 1, "strength": 0.5},
     "emissiveFactor": [1, 0.5, 0],
     "alphaMode": "MASK", "alphaCutoff": 0.3,
     "extensions": {"KHR_materials_emissive_strength": {"emissiveStrength": 4},
                    "KHR_materials_transmission": {"transmissionFactor": 1},
                    "KHR_materials_clearcoat": {"clearcoatFactor": 0.5, "clearcoatRoughnessFactor": 0.1}}},
    {}
  ]
}`
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	src := `
module main
import gltf.{ MaterialParams, load_material_params }
let f = (v: f32) -> string => f64(v).to_fixed(2)
let main = () -> void => {
  match load_material_params("` + path + `") {
    Some(ms) => {
      println("${ms.len()}")
      for m in ms {
        let a = m.slots[0]
        println("albedo uv${a.uv} ${f(a.offset_x)},${f(a.offset_y)} r${f(a.rotation)} s${f(a.scale_x)},${f(a.scale_y)} wrap${a.wrap} | mr wrap${m.slots[1].wrap} | occ uv${m.slots[3].uv}")
        println("n${f(m.normal_scale)} o${f(m.occlusion_strength)} m${f(m.metallic)} r${f(m.roughness)} e${f(m.emissive_r)},${f(m.emissive_g)},${f(m.emissive_b)}x${f(m.emissive_strength)} t${f(m.transmission)} cc${f(m.clearcoat)}/${f(m.clearcoat_roughness)} alpha${m.alpha_mode}@${f(m.alpha_cutoff)}")
      }
    },
    None => println("none"),
  }
}
`
	bin := compileCached(t, lookClang(t), emitWithSibling(t, src, "examples/raylib/gltf.lyra"), "-L"+libdir, "-lraylib")
	raw, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("running the material-params binary failed: %v", err)
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if !strings.HasPrefix(l, "INFO:") && !strings.HasPrefix(l, "WARNING:") {
			lines = append(lines, l)
		}
	}
	got := strings.Join(lines, "\n")
	want := strings.Join([]string{
		"2",
		"albedo uv1 0.50,0.00 r0.25 s2.00,3.00 wrap1 | mr wrap2 | occ uv1",
		"n0.30 o0.50 m0.25 r0.50 e1.00,0.50,0.00x4.00 t1.00 cc0.50/0.10 alpha1@0.30",
		"albedo uv0 0.00,0.00 r0.00 s1.00,1.00 wrap0 | mr wrap0 | occ uv0",
		"n1.00 o1.00 m1.00 r1.00 e0.00,0.00,0.00x1.00 t0.00 cc0.00/0.00 alpha0@0.50",
	}, "\n")
	if got != want {
		t.Errorf("material params:\n%s\nwant:\n%s", got, want)
	}
}

// emitWithSibling is emitWithPrelude with one module copied in beside the program, from a
// path relative to the repository — the way the viewer imports `gltf`, which resolves
// from the importing file's own directory and not from any library root.
func emitWithSibling(t *testing.T, src, sibling string) string {
	t.Helper()
	dir := t.TempDir()
	body, err := os.ReadFile(filepath.Join(repoStdRoot(t), sibling))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, filepath.Base(sibling)), body, 0o644); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(dir, "app.lyra")
	if err := os.WriteFile(entry, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	units, diags := modules.Resolve(entry, []string{dir, repoStdRoot(t)}, modules.Options{Prelude: modules.PreludeModule})
	if len(diags) != 0 {
		t.Fatalf("resolve: %v", diags)
	}
	res := driver.AnalyzeUnits(units)
	if res.HasErrors() {
		t.Fatalf("unexpected analysis errors: %v", res.Errors())
	}
	ep, epDiags := driver.ResolveEntryPoint(res)
	if ep == nil {
		t.Fatalf("no entry point: %v", epDiags)
	}
	ir, err := New().Emit(res, ep)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	return string(ir)
}

// testNodeAnimationGLB builds the two-node, two-clip file TestExec_GltfNodeAnimation reads.
func testNodeAnimationGLB(t *testing.T) []byte {
	t.Helper()
	var bin []byte
	type view struct {
		Buffer     int `json:"buffer"`
		ByteOffset int `json:"byteOffset"`
		ByteLength int `json:"byteLength"`
	}
	type accessor struct {
		BufferView    int    `json:"bufferView"`
		ComponentType int    `json:"componentType"`
		Count         int    `json:"count"`
		Type          string `json:"type"`
	}
	var views []view
	var accessors []accessor
	add := func(kind string, width int, fs ...float32) int {
		for len(bin)%4 != 0 {
			bin = append(bin, 0)
		}
		views = append(views, view{0, len(bin), 4 * len(fs)})
		for _, f := range fs {
			bin = binary.LittleEndian.AppendUint32(bin, math.Float32bits(f))
		}
		accessors = append(accessors, accessor{len(views) - 1, 5126, len(fs) / width, kind})
		return len(accessors) - 1
	}
	position := add("VEC3", 3, 0, 0, 0)
	times2 := add("SCALAR", 1, 0, 2)
	slide := add("VEC3", 3, 0, 0, 0, 4, 0, 0)
	turn := add("VEC4", 4, 0, 0, 0, 1, 0, 0, 1, 0)
	times1 := add("SCALAR", 1, 0, 1)
	grow := add("VEC3", 3, 0, 0, 0, 1, 1, 1, 0, 0, 0, 0, 0, 0, 3, 3, 3, 0, 0, 0)
	doc := map[string]any{
		"asset":  map[string]any{"version": "2.0"},
		"scene":  0,
		"scenes": []any{map[string]any{"nodes": []int{0}}},
		"nodes": []any{
			map[string]any{"children": []int{1}},
			map[string]any{"mesh": 0, "translation": []float64{1, 0, 0}},
		},
		"meshes": []any{map[string]any{"primitives": []any{map[string]any{"attributes": map[string]int{"POSITION": position}}}}},
		"animations": []any{
			map[string]any{"name": "move",
				"channels": []any{
					map[string]any{"sampler": 0, "target": map[string]any{"node": 0, "path": "translation"}},
					map[string]any{"sampler": 1, "target": map[string]any{"node": 1, "path": "rotation"}},
				},
				"samplers": []any{
					map[string]any{"input": times2, "output": slide, "interpolation": "LINEAR"},
					map[string]any{"input": times2, "output": turn, "interpolation": "STEP"},
				}},
			map[string]any{"name": "grow",
				"channels": []any{map[string]any{"sampler": 0, "target": map[string]any{"node": 0, "path": "scale"}}},
				"samplers": []any{map[string]any{"input": times1, "output": grow, "interpolation": "CUBICSPLINE"}}},
		},
		"accessors":   accessors,
		"bufferViews": views,
		"buffers":     []any{map[string]any{"byteLength": len(bin)}},
	}
	js, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	for len(js)%4 != 0 {
		js = append(js, ' ')
	}
	for len(bin)%4 != 0 {
		bin = append(bin, 0)
	}
	out := binary.LittleEndian.AppendUint32(nil, 0x46546C67)
	out = binary.LittleEndian.AppendUint32(out, 2)
	out = binary.LittleEndian.AppendUint32(out, uint32(12+8+len(js)+8+len(bin)))
	out = binary.LittleEndian.AppendUint32(out, uint32(len(js)))
	out = binary.LittleEndian.AppendUint32(out, 0x4E4F534A)
	out = append(out, js...)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(bin)))
	out = binary.LittleEndian.AppendUint32(out, 0x004E4942)
	return append(out, bin...)
}
