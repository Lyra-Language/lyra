package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// `bindings/imgui/generated.lyra` is 5,000 lines no one wrote, so a compiler change that
// breaks it would otherwise surface only when someone next builds against it. Checking the
// example checks the binding it imports — every wrapper, handle and constant — and needs
// no ImGui, SDL3 or C compiler, so it runs everywhere the suite does.
func TestExample_ImGuiPixelsChecksClean(t *testing.T) {
	root := repoRoot(t)
	t.Setenv("LYRA_STD", root)
	stdout, stderr, code := captureRun(t, "check", filepath.Join(root, "examples", "imgui", "pixels.lyra"))
	if code != 0 {
		t.Fatalf("checking the example exited %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if out := stdout + stderr; strings.Contains(out, "warning") {
		t.Errorf("the example or the binding warns:\n%s", out)
	}
}

// The committed binding is exactly what the generator makes from the pinned dcimgui.json:
// an edit to generated.lyra by hand, or a generator change not rerun, fails here. The JSON
// is downloaded by bindings/imgui/build.sh, so this skips on a checkout that never ran it.
func TestImGuiGenerator_ReproducesCommittedBinding(t *testing.T) {
	root := repoRoot(t)
	matches, _ := filepath.Glob(filepath.Join(root, "build", "deps", "DearBindings_*", "dcimgui.json"))
	if len(matches) != 1 {
		t.Skipf("no single build/deps/DearBindings_*/dcimgui.json (found %d) — run ./build.sh", len(matches))
	}
	if _, err := exec.LookPath("clang"); err != nil && os.Getenv("LYRA_CC") == "" {
		t.Skip("no C compiler to build the generator")
	}
	t.Setenv("LYRA_STD", root)
	dir := t.TempDir()
	gen := filepath.Join(dir, "gen")
	if _, stderr, code := captureRun(t, "build", "-o", gen,
		filepath.Join(root, "bindings", "imgui", "gen", "gen.lyra")); code != 0 {
		t.Fatalf("building the generator exited %d\nstderr: %s", code, stderr)
	}
	out := filepath.Join(dir, "generated.lyra")
	if b, err := exec.Command(gen, matches[0], out).CombinedOutput(); err != nil {
		t.Fatalf("the generator failed: %v\n%s", err, b)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join(root, "bindings", "imgui", "generated.lyra"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("bindings/imgui/generated.lyra is not what gen/gen.lyra makes from %s — rerun the generator (its header has the command)", matches[0])
	}
}
