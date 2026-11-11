package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// `bindings/imgui/generated.lyra` is 5,000 lines no one wrote, so a compiler change that
// breaks it would otherwise surface only when someone next builds against it. Checking the
// example checks the binding it imports — every wrapper, handle and constant — and needs
// no ImGui, SDL3 or C compiler, so it runs everywhere the suite does.
func TestExample_ImGuiPixelsChecksClean(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	setenv(t, "LYRA_STD", root)
	stdout, stderr, code := captureRun(t, "check", filepath.Join(root, "examples", "imgui", "pixels.lyra"))
	if code != 0 {
		t.Fatalf("checking the example exited %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if out := stdout + stderr; strings.Contains(out, "warning") {
		t.Errorf("the example or the binding warns:\n%s", out)
	}
}

// The committed binding is exactly what the generator makes from the pinned dcimgui.json —
// generated.lyra and the field accessors in generated.cpp: an edit to either by hand, or a
// generator change not rerun, fails here. The JSON is downloaded by bindings/imgui/build.sh,
// so this skips on a checkout that never ran it.
func TestImGuiGenerator_ReproducesCommittedBinding(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	matches, _ := filepath.Glob(filepath.Join(root, "build", "deps", "DearBindings_*", "dcimgui.json"))
	if len(matches) != 1 {
		t.Skipf("no single build/deps/DearBindings_*/dcimgui.json (found %d) — run ./build.sh", len(matches))
	}
	if _, err := exec.LookPath("clang"); err != nil && os.Getenv("LYRA_CC") == "" {
		t.Skip("no C compiler to build the generator")
	}
	setenv(t, "LYRA_STD", root)
	dir := t.TempDir()
	gen := filepath.Join(dir, "gen")
	if _, stderr, code := captureRun(t, "build", "-o", gen,
		filepath.Join(root, "bindings", "imgui", "gen", "gen.lyra")); code != 0 {
		t.Fatalf("building the generator exited %d\nstderr: %s", code, stderr)
	}
	outLyra, outCpp := filepath.Join(dir, "generated.lyra"), filepath.Join(dir, "generated.cpp")
	if b, err := command(t, gen, matches[0], outLyra, outCpp).CombinedOutput(); err != nil {
		t.Fatalf("the generator failed: %v\n%s", err, b)
	}
	for _, name := range []string{"generated.lyra", "generated.cpp"} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile(filepath.Join(root, "bindings", "imgui", name))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Errorf("bindings/imgui/%s is not what gen/gen.lyra makes from %s — rerun the generator (its header has the command)", name, matches[0])
		}
	}
}

// The shapes the generator makes of ImGui's pointers — a `[]f32` plot, a `[]Vec2`
// polyline, a `[]string` list box, a `[]u8` drag-and-drop payload copied back out, a
// `Maybe` handle, a `const ImVec4 *` copied, a NULL-default callback left out, an
// `ImVector`'s length and elements, and a list clipper made with its constructor — driven
// on a headless host by `examples/imgui/gallery.lyra --check`, beside the components
// example's own check. Checking needs nothing; running needs build.sh's
// build/lib/liblyra-imgui.a (and SDL3), and skips without it.
func TestRun_ImGuiExamplesCheckHeadless(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	setenv(t, "LYRA_STD", root)
	gallery := filepath.Join(root, "examples", "imgui", "gallery.lyra")
	stdout, stderr, code := captureRun(t, "check", gallery)
	if code != 0 || strings.Contains(stdout+stderr, "warning") {
		t.Fatalf("checking the gallery exited %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if !imguiArchiveRunsHere(filepath.Join(root, "build", "lib", "liblyra-imgui.a")) {
		t.Skip("no build/lib/liblyra-imgui.a built for this platform — run ./build.sh")
	}
	if err := command(t, "pkg-config", "--exists", "sdl3").Run(); err != nil {
		t.Skip("no SDL3")
	}
	for _, name := range []string{"gallery", "components"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			bin := filepath.Join(t.TempDir(), name)
			src := filepath.Join(root, "examples", "imgui", name+".lyra")
			if _, stderr, code := captureRunUnoptimized(t, "build", "-o", bin, src); code != 0 {
				t.Fatalf("building %s exited %d\nstderr: %s", name, code, stderr)
			}
			out, err := command(t, bin, "--check").CombinedOutput()
			if err != nil || !strings.Contains(string(out), "all passed") {
				t.Errorf("%s --check: %v\n%s", name, err, out)
			}
		})
	}
}

// imguiArchiveRunsHere reports whether the archive at path exists and holds this platform's
// objects: `./asan.sh` mounts the Mac's build/ into a Linux container, where a Mach-O archive
// is there and links nothing.
func imguiArchiveRunsHere(path string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	if runtime.GOOS == "darwin" {
		return bytes.Contains(b, []byte{0xcf, 0xfa, 0xed, 0xfe})
	}
	return bytes.Contains(b, []byte("\x7fELF"))
}
