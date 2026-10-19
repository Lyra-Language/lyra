package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// buildLui builds tools/lui into a temporary directory, skipping without a C compiler.
func buildLui(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("clang"); err != nil && os.Getenv("LYRA_CC") == "" {
		t.Skip("no C compiler to build lui")
	}
	root := repoRoot(t)
	t.Setenv("LYRA_STD", root)
	lui := filepath.Join(t.TempDir(), "lui")
	if _, stderr, code := captureRun(t, "build", "-o", lui, filepath.Join(root, "tools", "lui", "lui.lyra")); code != 0 {
		t.Fatalf("building lui exited %d\nstderr: %s", code, stderr)
	}
	return lui
}

// The example's view is what lui makes from its markup — an edit to the generated module
// by hand, or a lui change not rerun, fails here — and the program around it checks clean,
// which is the markup's Lyra checked by lyrac.
func TestLui_ExampleIsCurrentAndChecks(t *testing.T) {
	lui := buildLui(t)
	root := repoRoot(t)
	if b, err := exec.Command(lui, "--check", filepath.Join(root, "examples", "imgui", "components_view.lui")).CombinedOutput(); err != nil {
		t.Fatalf("components_view.lyra is stale: %v\n%s", err, b)
	}
	stdout, stderr, code := captureRun(t, "check", filepath.Join(root, "examples", "imgui", "components.lyra"))
	if code != 0 {
		t.Fatalf("checking the example exited %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if out := stdout + stderr; strings.Contains(out, "warning") {
		t.Errorf("the example or its generated view warns:\n%s", out)
	}
}

// A mistake in markup is reported at its line in the .lui, saying what is wrong — not
// left to lyrac to report in the generated module.
func TestLui_ReportsMistakesAtTheirLine(t *testing.T) {
	lui := buildLui(t)
	dir := t.TempDir()
	cases := []struct{ name, source, want string }{
		{"unknown", "<component name=\"a\">\n  <Buton/>\n</component>", "x.lui:2: <Buton> is not an element of bindings.imgui.ui"},
		{"attribute", "<component name=\"a\" props=\"x: e\">\n  <Button onclick={x}>Hi</Button>\n</component>", "x.lui:2: <Button> has no attribute onclick"},
		{"required", "<component name=\"a\" props=\"x: e\">\n  <Button>Hi</Button>\n</component>", "x.lui:2: <Button> needs on_click"},
		{"closing", "<component name=\"a\">\n  <Row>\n</component>", "x.lui:3: </component> closes <Row> (line 2)"},
		{"bare text", "<component name=\"a\">\n  <Row>hello</Row>\n</component>", "x.lui:2: text \"hello\" outside an element that takes it"},
		{"style", "<component name=\"a\">\n  <style> Button { colour: #fff } </style>\n</component>", "x.lui:2: no style property colour"},
		{"shortcut", "<component name=\"a\" props=\"x: e\">\n  <MenuItem on_select={x} shortcut=\"Alt+X\">Hi</MenuItem>\n</component>", "Alt is not Cmd, Ctrl or Shift"},
		{"prop", "<component name=\"a\" props=\"x: e\">\n  <b y={x}/>\n</component>\n<component name=\"b\" props=\"x: e\"></component>", "x.lui:2: component b has no prop y"},
		{"else", "<component name=\"a\">\n  <else>no</else>\n</component>", "x.lui:2: <else> with no <if> before it"},
		{"quoted event", "<component name=\"a\" props=\"x: e\">\n  <Button on_click=\"x\">Hi</Button>\n</component>", "x.lui:2: on_click of <Button> takes {…}"},
	}
	for _, c := range cases {
		path := filepath.Join(dir, "x.lui")
		if err := os.WriteFile(path, []byte(c.source), 0o644); err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command(lui, path).CombinedOutput()
		if err == nil {
			t.Errorf("%s: lui accepted it", c.name)
			continue
		}
		if !strings.Contains(string(out), c.want) {
			t.Errorf("%s: got %q, want it to contain %q", c.name, out, c.want)
		}
	}
}

// The value-bearing elements Vega's Settings needed — a combo of selectables, a form's
// field, a centered window, a width in ems — generate Lyra that checks clean.
func TestLui_ComboFormAndEmWidths(t *testing.T) {
	lui := buildLui(t)
	dir := t.TempDir()
	source := `<script>
  pub data Ev = Pick(i64) | Close
</script>
<component name="view" props="chosen: i64" event="Ev">
  <Window title="Pick" centered on_close={Close}>
    <Form>
      <Field label="Size">
        <Combo label="##size" id="Size" preview="{chosen}" style="width: 6em">
          <for each={n in 0..<3}>
            <Selectable id="size {n}" selected={n == chosen} on_select={Pick(n)}>{n}</Selectable>
          </for>
        </Combo>
      </Field>
    </Form>
  </Window>
</component>
`
	path := filepath.Join(dir, "view.lui")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	if b, err := exec.Command(lui, path).CombinedOutput(); err != nil {
		t.Fatalf("lui refused it: %v\n%s", err, b)
	}
	generated, err := os.ReadFile(filepath.Join(dir, "view.lyra"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"width_em: Some(6.0)", "centered: true", "Field(FieldAttrs { label: \"Size\" }", "Combo(ComboAttrs {"} {
		if !strings.Contains(string(generated), want) {
			t.Errorf("the generated module lacks %q:\n%s", want, generated)
		}
	}
	stdout, stderr, code := captureRun(t, "check", filepath.Join(dir, "view.lyra"))
	if code != 0 || strings.Contains(stdout+stderr, "warning") {
		t.Errorf("checking the generated module exited %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
}

// lyrac's errors in a generated module are put back at the markup by `lui map`: one in an
// element's attribute at its tag's line, one in a script at its own line, and a line
// naming no generated module left as it is.
func TestLui_MapPutsErrorsBackAtTheMarkup(t *testing.T) {
	lui := buildLui(t)
	dir := t.TempDir()
	source := `<script>
  pub data Ev = Go

  let broken = () -> bool => 7
</script>
<component name="view" event="Ev">
  <Row>
    <Button on_click={Gone}>Go</Button>
  </Row>
</component>
`
	path := filepath.Join(dir, "view.lui")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	if b, err := exec.Command(lui, path).CombinedOutput(); err != nil {
		t.Fatalf("lui refused it: %v\n%s", err, b)
	}
	stdout, stderr, _ := captureRun(t, "check", filepath.Join(dir, "view.lyra"))
	diagnostics := stdout + stderr + "unrelated.lyra:3:4: error: left alone\n"
	cmd := exec.Command(lui, "map")
	cmd.Stdin = strings.NewReader(diagnostics)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("lui map: %v\n%s", err, out)
	}
	got := string(out)
	for _, want := range []string{
		path + ":4: error",
		path + ":8: error",
		"unrelated.lyra:3:4: error: left alone",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("mapped output lacks %q:\n%s\nfrom:\n%s", want, got, diagnostics)
		}
	}
	if strings.Contains(got, "view.lyra:") {
		t.Errorf("a place in the generated module was left unmapped:\n%s", got)
	}
}

// `lui --watch` generates at once, and again when the markup changes.
func TestLui_WatchRegeneratesOnChange(t *testing.T) {
	lui := buildLui(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "view.lui")
	out := filepath.Join(dir, "view.lyra")
	write := func(label string) {
		src := `<component name="view" props="go: e"><Button on_click={go}>` + label + `</Button></component>`
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	waitFor := func(text string) bool {
		for i := 0; i < 50; i++ {
			if b, _ := os.ReadFile(out); strings.Contains(string(b), text) {
				return true
			}
			time.Sleep(100 * time.Millisecond)
		}
		return false
	}
	write("First")
	cmd := exec.Command(lui, "--watch", path)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	if !waitFor(`"First"`) {
		t.Fatal("--watch did not generate at once")
	}
	write("Second")
	if !waitFor(`"Second"`) {
		t.Fatal("--watch did not regenerate after the markup changed")
	}
}
