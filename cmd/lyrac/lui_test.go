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
		{"attribute", "<component name=\"a\" props={{ x: e }}>\n  <Button onclick={x}>Hi</Button>\n</component>", "x.lui:2: <Button> has no attribute onclick"},
		{"required", "<component name=\"a\" props={{ x: e }}>\n  <Button>Hi</Button>\n</component>", "x.lui:2: <Button> needs on_click"},
		{"closing", "<component name=\"a\">\n  <Row>\n</component>", "x.lui:3: </component> closes <Row> (line 2)"},
		{"bare text", "<component name=\"a\">\n  <Row>hello</Row>\n</component>", "x.lui:2: text \"hello\" outside an element that takes it"},
		{"style", "<component name=\"a\">\n  <style> Button { colour: #fff } </style>\n</component>", "x.lui:2: no style property colour"},
		{"shortcut", "<component name=\"a\" props={{ x: e }}>\n  <MenuItem on_select={x} shortcut=\"Meta+X\">Hi</MenuItem>\n</component>", "Meta is not Cmd, Ctrl, Alt or Shift"},
		{"prop", "<component name=\"a\" props={{ x: e }}>\n  <b y={x}/>\n</component>\n<component name=\"b\" props={{ x: e }}></component>", "x.lui:2: component b has no prop y"},
		{"else", "<component name=\"a\">\n  <else>no</else>\n</component>", "x.lui:2: <else> with no <if> before it"},
		{"string props", "<component name=\"a\" props=\"x: e\">\n</component>", "x.lui:1: props are an object: props={{ name: Type, other: Type = default }}"},
		{"untyped prop", "<component name=\"a\" props={{ x }}>\n</component>", "x.lui:1: prop \"x\" has no type"},
		{"quoted event", "<component name=\"a\" props={{ x: e }}>\n  <Button on_click=\"x\">Hi</Button>\n</component>", "x.lui:2: on_click of <Button> takes {…}"},
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
<component name="view" props={{ chosen: i64 }} event="Ev">
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

// A slider and a color editor, and a text field's hint: `on_change={Size}` names a
// constructor of an f32 or a u32, which lui makes the typed lambda as for the other
// inputs; a lambda written out carries what the constructor needs besides (`Paint("fg", v)`).
// The module checks clean.
func TestLui_SliderColorEditAndHint(t *testing.T) {
	lui := buildLui(t)
	dir := t.TempDir()
	source := `<script>
  pub data Ev = Size(f32) | SizeDone | Paint(string, u32) | Named(string)
</script>
<component name="view" props={{ size: f32, fg: u32, name: string }} event="Ev">
  <Slider label="##size" value={size} min={8.0} max={32.0} format="%.0f pt" on_change={Size} on_commit={SizeDone}/>
  <ColorEdit label="##fg" value={fg} inputs={false} on_change={(v: u32) -> Ev => Paint("fg", v)}/>
  <InputText label="##name" value={name} hint="Theme name" on_change={Named}/>
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
	for _, want := range []string{
		"Slider(SliderAttrs {",
		"on_change: (value: f32) -> Ev => Size(value)",
		`format: "%.0f pt"`,
		"on_commit: Some(SizeDone)",
		"ColorEdit(ColorEditAttrs {",
		"inputs: false",
		`hint: "Theme name"`,
	} {
		if !strings.Contains(string(generated), want) {
			t.Errorf("the generated module lacks %q:\n%s", want, generated)
		}
	}
	stdout, stderr, code := captureRun(t, "check", filepath.Join(dir, "view.lyra"))
	if code != 0 || strings.Contains(stdout+stderr, "warning") {
		t.Errorf("checking the generated module exited %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
}

// A shortcut's modifiers and keys as a Linux terminal's menus show them: Alt alone (no
// primary modifier), a named key, and every modifier at once; the module checks clean.
func TestLui_ShortcutsWithAltAndNamedKeys(t *testing.T) {
	lui := buildLui(t)
	dir := t.TempDir()
	source := `<script>
  pub data Ev = First | Previous | All
</script>
<component name="view" event="Ev">
  <MainMenuBar>
    <Menu label="Window">
      <MenuItem on_select={First} shortcut="Alt+1">First</MenuItem>
      <MenuItem on_select={Previous} shortcut="Ctrl+pgup">Previous</MenuItem>
      <MenuItem on_select={All} shortcut="Cmd+Alt+Shift+K">All</MenuItem>
    </Menu>
  </MainMenuBar>
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
	for _, want := range []string{
		`Shortcut { key: "1", alt: true, primary: false }`,
		`Shortcut { key: "PgUp" }`,
		`Shortcut { key: "k", shift: true, alt: true }`,
	} {
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
		src := `<component name="view" props={{ go: e }}><Button on_click={go}>` + label + `</Button></component>`
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

// The value inputs and the modal: `on_change={Width}` names a constructor, which lui makes
// a typed lambda of (Lyra has no constructor values, and a lambda in a generic field gets
// no parameter types from it); the rest — a radio, a closing button, a slot — as written,
// a slot's name interpolated as any attribute is (a component naming its caller's slot).
// The module checks clean.
func TestLui_InputsModalAndSlot(t *testing.T) {
	lui := buildLui(t)
	dir := t.TempDir()
	source := `<script>
  pub data Ev = Width(i64) | Named(string) | Done | Pick | Close
</script>
<component name="view" props={{ w: i64, name: string }} event="Ev">
  <Modal title="Edit###edit" on_escape={Close}>
    <swatch which="fg"/>
    <InputInt label="Width" value={w} step={8} on_change={Width} on_commit={Done}/>
    <InputText label="Name" value={name} on_change={(text: string) -> Ev => Named(text)}/>
    <Radio selected on_select={Pick}>Pick</Radio>
    <Slot name="preview"/>
    <Button closes on_click={Close}>OK</Button>
  </Modal>
</component>
<component name="swatch" props={{ which: string }}>
  <Slot name="colour {which}"/>
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
	for _, want := range []string{
		"on_change: (value: i64) -> Ev => Width(value)",
		"on_commit: Some(Done)",
		"on_change: (text: string) -> Ev => Named(text)",
		`Slot("preview")`,
		`Slot("colour ${which}")`,
		"closes: true",
		"on_escape: Some(Close)",
	} {
		if !strings.Contains(string(generated), want) {
			t.Errorf("the generated module lacks %q:\n%s", want, generated)
		}
	}
	stdout, stderr, code := captureRun(t, "check", filepath.Join(dir, "view.lyra"))
	if code != 0 || strings.Contains(stdout+stderr, "warning") {
		t.Errorf("checking the generated module exited %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
}
