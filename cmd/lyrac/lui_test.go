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
	setenv(t, "LYRA_STD", root)
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
	t.Parallel()
	lui := buildLui(t)
	root := repoRoot(t)
	if b, err := command(t, lui, "--check", filepath.Join(root, "examples", "imgui", "components_view.lui")).CombinedOutput(); err != nil {
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
	t.Parallel()
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
		{"string event", "<component name=\"a\" event=\"Ev\">\n</component>", "x.lui:1: event is a type in braces: event={SettingsEvent}"},
		{"untyped prop", "<component name=\"a\" props={{ x }}>\n</component>", "x.lui:1: prop \"x\" has no type"},
		{"quoted event", "<component name=\"a\" props={{ x: e }}>\n  <Button on_click=\"x\">Hi</Button>\n</component>", "x.lui:2: on_click of <Button> takes {…}"},
	}
	for _, c := range cases {
		path := filepath.Join(dir, "x.lui")
		if err := os.WriteFile(path, []byte(c.source), 0o644); err != nil {
			t.Fatal(err)
		}
		out, err := command(t, lui, path).CombinedOutput()
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
	t.Parallel()
	lui := buildLui(t)
	dir := t.TempDir()
	source := `<script>
  pub data Ev = Pick(i64) | Close
</script>
<component name="view" props={{ chosen: i64 }} event={Ev}>
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
	if b, err := command(t, lui, path).CombinedOutput(); err != nil {
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
	t.Parallel()
	lui := buildLui(t)
	dir := t.TempDir()
	source := `<script>
  pub data Ev = Size(f32) | SizeDone | Paint(string, u32) | Named(string)
</script>
<component name="view" props={{ size: f32, fg: u32, name: string }} event={Ev}>
  <Slider label="##size" value={size} min={8.0} max={32.0} format="%.0f pt" on_change={Size} on_commit={SizeDone}/>
  <ColorEdit label="##fg" value={fg} inputs={false} on_change={(v: u32) -> Ev => Paint("fg", v)}/>
  <InputText label="##name" value={name} hint="Theme name" on_change={Named}/>
</component>
`
	path := filepath.Join(dir, "view.lui")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	if b, err := command(t, lui, path).CombinedOutput(); err != nil {
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

// Spacing and boxes: `gap` in a sheet and inline, a Form and a Row with their attributes
// (and with none, an empty attributes struct), a Space, and Groups side by side in a Row;
// the module checks clean.
func TestLui_GapFormRowAndSpace(t *testing.T) {
	t.Parallel()
	lui := buildLui(t)
	dir := t.TempDir()
	source := `<component name="view" props={{ go: e }}>
  <style>
    Form { gap: 10 }
    Row.tight { gap: 2 }
  </style>
  <Form>
    <Field label="A"><Button on_click={go}>One</Button></Field>
  </Form>
  <Form style="gap: 4"/>
  <Space height={12}/>
  <Row class="tight">
    <Button on_click={go}>Two</Button>
  </Row>
  <Row wrap>
    <Group style="gap: 6">
      <Text>Left</Text>
      <Button on_click={go}>Three</Button>
    </Group>
    <Group class="right"><Text>Right</Text></Group>
  </Row>
</component>
`
	path := filepath.Join(dir, "view.lui")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	if b, err := command(t, lui, path).CombinedOutput(); err != nil {
		t.Fatalf("lui refused it: %v\n%s", err, b)
	}
	generated, err := os.ReadFile(filepath.Join(dir, "view.lyra"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`StyleRule { selector: "Form", style: Style { gap: Some(10.0) } }`,
		`StyleRule { selector: "Row.tight", style: Style { gap: Some(2.0) } }`,
		"Form(FormAttrs {}, [",
		"Form(FormAttrs { style: Style { gap: Some(4.0) } }, [",
		"Space(SpaceAttrs { height: 12 })",
		`Row(RowAttrs { class: "tight" }, [`,
		"Row(RowAttrs { wrap: true }, [",
		"Group(GroupAttrs { style: Style { gap: Some(6.0) } }, [",
		`Group(GroupAttrs { class: "right" }, [`,
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
	t.Parallel()
	lui := buildLui(t)
	dir := t.TempDir()
	source := `<script>
  pub data Ev = First | Previous | All
</script>
<component name="view" event={Ev}>
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
	if b, err := command(t, lui, path).CombinedOutput(); err != nil {
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
	t.Parallel()
	lui := buildLui(t)
	dir := t.TempDir()
	source := `<script>
  pub data Ev = Go

  let broken = () -> bool => 7
</script>
<component name="view" event={Ev}>
  <Row>
    <Button on_click={Gone}>Go</Button>
  </Row>
</component>
`
	path := filepath.Join(dir, "view.lui")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	if b, err := command(t, lui, path).CombinedOutput(); err != nil {
		t.Fatalf("lui refused it: %v\n%s", err, b)
	}
	stdout, stderr, _ := captureRun(t, "check", filepath.Join(dir, "view.lyra"))
	diagnostics := stdout + stderr + "unrelated.lyra:3:4: error: left alone\n"
	cmd := command(t, lui, "map")
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
	t.Parallel()
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
	cmd := command(t, lui, "--watch", path)
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

// A directory is every .lui file beneath it — nested ones too, a hidden directory's not —
// and under --watch it is walked again on every pass, so a file added after the watch
// began is generated as it appears.
func TestLui_DirectoriesAndNewFilesUnderWatch(t *testing.T) {
	t.Parallel()
	lui := buildLui(t)
	dir := t.TempDir()
	view := func(label string) string {
		return `<component name="view" props={{ go: e }}><Button on_click={go}>` + label + `</Button></component>`
	}
	put := func(rel, text string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	put("top.lui", view("Top"))
	put("deep/down/nested.lui", view("Nested"))
	put(".hidden/skipped.lui", view("Hidden"))
	if b, err := command(t, lui, dir).CombinedOutput(); err != nil {
		t.Fatalf("lui on a directory failed: %v\n%s", err, b)
	}
	for _, rel := range []string{"top.lyra", "deep/down/nested.lyra"} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Errorf("%s was not generated: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".hidden/skipped.lyra")); err == nil {
		t.Error("a hidden directory's markup was generated")
	}
	if b, err := command(t, lui, "--check", dir).CombinedOutput(); err != nil {
		t.Errorf("--check on the directory just generated failed: %v\n%s", err, b)
	}
	empty := t.TempDir()
	if b, err := command(t, lui, empty).CombinedOutput(); err == nil || !strings.Contains(string(b), "no .lui file") {
		t.Errorf("a directory with no markup should be refused, said %q", b)
	}

	cmd := command(t, lui, "--watch", dir)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	put("later/added.lui", view("Added"))
	out := filepath.Join(dir, "later/added.lyra")
	for i := 0; i < 50; i++ {
		if b, _ := os.ReadFile(out); strings.Contains(string(b), `"Added"`) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("--watch on a directory did not generate a file added after it began")
}

// The value inputs and the modal: `on_change={Width}` names a constructor, which lui makes
// a typed lambda of (Lyra has no constructor values, and a lambda in a generic field gets
// no parameter types from it); the rest — a radio, a closing button, a slot — as written,
// a slot's name interpolated as any attribute is (a component naming its caller's slot).
// The module checks clean.
func TestLui_InputsModalAndSlot(t *testing.T) {
	t.Parallel()
	lui := buildLui(t)
	dir := t.TempDir()
	source := `<script>
  pub data Ev = Width(i64) | Named(string) | Done | Pick | Close
</script>
<component name="view" props={{ w: i64, name: string }} event={Ev}>
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
	if b, err := command(t, lui, path).CombinedOutput(); err != nil {
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

// The ui target's example — components_view.lui ported from bindings.imgui.ui — is what lui
// makes from its markup, and the program running it checks clean, warnings and all.
func TestLui_UiTargetExampleIsCurrentAndChecks(t *testing.T) {
	t.Parallel()
	skipWithoutText(t)
	lui := buildLui(t)
	root := repoRoot(t)
	if b, err := command(t, lui, "--check", filepath.Join(root, "examples", "ui", "components_view.lui")).CombinedOutput(); err != nil {
		t.Fatalf("examples/ui/components_view.lyra is stale: %v\n%s", err, b)
	}
	stdout, stderr, code := captureRun(t, "check", filepath.Join(root, "examples", "ui", "components.lyra"))
	if code != 0 {
		t.Fatalf("checking the example exited %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if out := stdout + stderr; strings.Contains(out, "warning") {
		t.Errorf("the example or its generated view warns:\n%s", out)
	}
}

// The ui target's mistakes, at their markup lines: an element ui lacks, a part a component
// does not have (its own, or a control's), a token in an inline style, `key=` or a global
// style in the bindings.imgui.ui target, an unknown target, a <for> of several elements, an
// import lui cannot read, and a misspelt prop of an imported component.
func TestLui_UiTargetReportsMistakesAtTheirLine(t *testing.T) {
	t.Parallel()
	lui := buildLui(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "w.lui"), []byte("<script target=\"ui\"></script>\n<component name=\"badge\" props={{ words: string }}>\n  <Text part=\"label\">{words}</Text>\n</component>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ui := "<script target=\"ui\"></script>\n"
	cases := []struct{ name, source, want string }{
		{"unknown", ui + "<component name=\"a\">\n  <Buton/>\n</component>", "x.lui:3: <Buton> is not an element of ui"},
		{"attribute", ui + "<component name=\"a\">\n  <Column gapp=\"8\"/>\n</component>", "x.lui:3: <Column> has no attribute gapp"},
		{"own part", ui + "<component name=\"a\">\n  <Text part=\"title\">T</Text>\n</component>\n<style global>\n  a::titel { color: #fff }\n</style>", "x.lui:6: a has no part titel (its parts: title)"},
		{"control part", ui + "<style global>\n\n  Slider::knob { color: #fff }\n</style>", "x.lui:4: Slider has no part knob (its parts: track, fill, thumb)"},
		{"class part", ui + "<component name=\"a\">\n  <Text part=\"title\">T</Text>\n</component>\n<style global>\n  a.big::tittle { color: #fff }\n</style>", "x.lui:6: a has no part tittle"},
		{"inline token", ui + "<component name=\"a\">\n  <Column style=\"gap: $space.m\"/>\n</component>", "x.lui:3: $space.m reads the theme, which an inline style cannot"},
		{"bad token", ui + "<style global>\n  .a { color: $colors-x }\n</style>", "x.lui:3: $colors-x is not a token"},
		{"imgui key", "<component name=\"a\" props={{ x: e }}>\n  <Button on_click={x} key=\"k\">Hi</Button>\n</component>", "x.lui:2: key= and part= are the ui target's"},
		{"imgui global", "<style global>\n</style>", "x.lui:1: a global <style> is the ui target's"},
		{"target", "<script target=\"web\"></script>", "x.lui:1: target is \"ui\" or \"imgui\", not \"web\""},
		{"for", ui + "<component name=\"a\">\n  <for each={i in 0..<2}>\n    <Text>a</Text>\n    <Text>b</Text>\n  </for>\n</component>", "x.lui:3: a <for> holds one element in the ui target"},
		{"import", ui + "<import src=\"nope.lui\"/>", "x.lui:2: cannot read nope.lui"},
		{"imported prop", ui + "<import src=\"w.lui\"/>\n<component name=\"a\">\n  <badge word=\"hi\"/>\n</component>", "x.lui:4: component badge has no prop word"},
		{"imported part", ui + "<import src=\"w.lui\"/>\n<style global>\n  badge::lable { color: #fff }\n</style>", "x.lui:4: badge has no part lable (its parts: label)"},
		{"flex value", ui + "<component name=\"a\">\n  <Flex wrap=\"wraps\"/>\n</component>", "x.lui:3: flex-wrap is nowrap, wrap, wrap-reverse, not wraps"},
		{"track", ui + "<component name=\"a\">\n  <Grid columns=\"1fr bogus\"/>\n</component>", "x.lui:3: a track is a length, Nfr, auto, min-content, max-content, minmax(…) or repeat(…), not bogus"},
		{"grid attribute", ui + "<component name=\"a\">\n  <Grid colums=\"1fr\"/>\n</component>", "x.lui:3: <Grid> has no attribute colums"},
		{"grid line", ui + "<component name=\"a\">\n  <Text style=\"grid-column: 3 / 2\">x</Text>\n</component>", "x.lui:3: grid lines are N, span N, N / M or N / span M, not 3 / 2"},
		{"justify stretch", ui + "<component name=\"a\">\n  <Text style=\"justify-content: stretch\">x</Text>\n</component>", "x.lui:3: justify-content is start, flex-start, end, flex-end, center, space-between, space-around, space-evenly, not stretch"},
		{"braced word", ui + "<component name=\"a\">\n  <Flex direction={d}/>\n</component>", "x.lui:3: direction of <Flex> is written as text, not {…}"},
		{"media feature", ui + "<style global>\n  @media (min-wdth: 600) {\n    .a { gap: 1 }\n  }\n</style>", "x.lui:3: no media feature min-wdth in ui"},
		{"media orientation", ui + "<style global>\n  @media (orientation: sideways) {\n    .a { gap: 1 }\n  }\n</style>", "x.lui:3: orientation is portrait or landscape, not sideways"},
		{"media length", ui + "<style global>\n  @media (min-width: wide) {\n    .a { gap: 1 }\n  }\n</style>", "x.lui:3: min-width is a length in logical units (600 or 600px), not wide"},
		{"media parentheses", ui + "<style global>\n  @media min-width: 600 {\n    .a { gap: 1 }\n  }\n</style>", "x.lui:3: min-width: 600 is not a media feature in (…)"},
		{"media nested", ui + "<style global>\n  @media (min-width: 1) {\n    @media (max-width: 2) {\n      .a { gap: 1 }\n    }\n  }\n</style>", "x.lui:4: an @media inside an @media"},
		{"at-rule", ui + "<style global>\n  @supports (x: y) {\n  }\n</style>", "x.lui:3: @supports is not an at-rule of a <style>; @media is"},
		{"selector in a list", ui + "<style global>\n  .a,\n  Column Text { gap: 1 }\n</style>", "x.lui:4: Column Text is not a selector ui reads"},
		{"pseudo-class", ui + "<style global>\n  .a:visited { gap: 1 }\n</style>", "x.lui:3: .a:visited: :visited is not a pseudo-class ui has (hover, active, focus, focus-visible, focus-within, checked, read-only, read-write, disabled, enabled)"},
		{"state class", ui + "<style global>\n  Button.pressed { gap: 1 }\n</style>", "x.lui:3: Button.pressed: a state is a pseudo-class, as in CSS: write :active, not .pressed"},
		{"class on a part", ui + "<style global>\n  Slider::thumb.big { gap: 1 }\n</style>", "x.lui:3: Slider::thumb.big: a part takes pseudo-classes after it (Comp::part:hover), not classes"},
		{"pseudo-element", ui + "<style global>\n  .a::before:hover { gap: 1 }\n</style>", "x.lui:3: .a::before:hover is not a selector ui reads"},
		{"disabled text", ui + "<component name=\"a\">\n  <Column disabled=\"yes\"/>\n</component>", "x.lui:3: disabled is bare or {condition}, not text"},
		{"shadow lengths", ui + "<style global>\n  .a { box-shadow: 0 #000 }\n</style>", "x.lui:3: a box-shadow is [inset] x y [blur [spread]] #colour, not 0 #000"},
		{"shadow many", ui + "<style global>\n  .a { box-shadow: 1 2 3 4 5 #000 }\n</style>", "x.lui:3: a box-shadow is [inset] x y [blur [spread]] #colour, not 1 2 3 4 5 #000"},
		{"gradient side", ui + "<style global>\n  .a { background: linear-gradient(to middle, #000, #fff) }\n</style>", "x.lui:3: a gradient goes to top, right, bottom, left or a corner, not to middle"},
		{"gradient stops", ui + "<style global>\n  .a { background: linear-gradient(#000) }\n</style>", "x.lui:3: a gradient needs two colour stops or more"},
		{"gradient kind", ui + "<style global>\n  .a { background-image: conic-gradient(#000, #fff) }\n</style>", "x.lui:3: a gradient is linear-gradient(…) or radial-gradient(…)"},
		{"filter", ui + "<style global>\n  .a { filter: grayscale(1) }\n</style>", "x.lui:3: filter is blur(N) or none, not grayscale(1)"},
		{"stop place", ui + "<style global>\n  .a { background: linear-gradient(#000 50, #fff) }\n</style>", "x.lui:3: a colour stop's place is a percentage, not 50"},
		{"transition time", ui + "<style global>\n  .a { transition: opacity 200 }\n</style>", "x.lui:3: a transition's duration and delay are times, Nms or Ns, not 200"},
		{"transition property", ui + "<style global>\n  .a { transition: sideways 1s }\n</style>", "x.lui:3: transition-property is all or a property that animates"},
		{"transition times", ui + "<style global>\n  .a { transition: opacity 1s 2s 3s }\n</style>", "x.lui:3: a transition is [property] duration [timing-function] [delay], not opacity 1s 2s 3s"},
		{"transition timings", ui + "<style global>\n  .a { transition: opacity 1s ease linear }\n</style>", "x.lui:3: a transition has one timing function, not two in opacity 1s ease linear"},
		{"transition bezier", ui + "<style global>\n  .a { transition-timing-function: cubic-bezier(2, 0, 1, 1) }\n</style>", "x.lui:3: cubic-bezier's x1 and x2 are 0 to 1, not 2 in cubic-bezier(2, 0, 1, 1)"},
		{"transition steps", ui + "<style global>\n  .a { transition-timing-function: steps(0) }\n</style>", "x.lui:3: steps(n) takes one step or more, not 0"},
		{"transition timing", ui + "<style global>\n  .a { transition-timing-function: bouncy }\n</style>", "x.lui:3: transition-timing-function is ease, linear, ease-in, ease-out, ease-in-out, cubic-bezier(x1, y1, x2, y2) or steps(n[, jump-start|jump-end]), not bouncy"},
		{"transition delay", ui + "<style global>\n  .a { transition-delay: soon }\n</style>", "x.lui:3: transition-delay is a time, Nms or Ns, not soon"},
		{"transition longhand", ui + "<style global>\n  .a { transition-propery: opacity }\n</style>", "x.lui:3: no style property transition-propery in ui"},
		{"readonly text", ui + "<script target=\"ui\">\n  pub data Ev = Go\n</script>\n<component name=\"a\" event={Ev}>\n  <TextInput value=\"x\" readonly=\"yes\"/>\n</component>", "x.lui:6: readonly is bare or {condition}, not text"},
		{"readonly condition", ui + "<script target=\"ui\">\n  pub data Ev = Go\n</script>\n<component name=\"a\" event={Ev}>\n  <TextInput value=\"x\" readonly={true}/>\n</component>", "x.lui:6: <TextInput> needs on_edit"},
		{"overflow value", ui + "<style global>\n  .a { overflow: sideways }\n</style>", "x.lui:3: overflow is visible, hidden, clip, scroll or auto, not sideways"},
		{"overflow many", ui + "<style global>\n  .a { overflow: auto auto hidden }\n</style>", "x.lui:3: overflow is one or two of visible, hidden, clip, scroll, auto, not auto auto hidden"},
		{"overflow-y value", ui + "<component name=\"a\">\n  <Column style=\"overflow-y: scrolls\"/>\n</component>", "x.lui:3: overflow is visible, hidden, clip, scroll or auto, not scrolls"},
		{"window overflow", ui + "<window overflow=\"wrap\"/>", "x.lui:2: overflow is visible, hidden, clip, scroll or auto, not wrap"},
		{"window width alone", ui + "<window width=\"300\"/>", "x.lui:2: <window> takes width and height together"},
		{"window size", ui + "<window size=\"300\"/>", "x.lui:2: <window> has no attribute size"},
		{"window title bare", ui + "<window title/>", "x.lui:2: title of <window> needs a value"},
		{"window resize text", ui + "<window resize_x=\"false\"/>", "x.lui:2: resize_x is bare or {condition}, not text"},
	}
	for _, c := range cases {
		path := filepath.Join(dir, "x.lui")
		if err := os.WriteFile(path, []byte(c.source), 0o644); err != nil {
			t.Fatal(err)
		}
		out, err := command(t, lui, path).CombinedOutput()
		if err == nil {
			t.Errorf("%s: lui accepted it", c.name)
			continue
		}
		if !strings.Contains(string(out), c.want) {
			t.Errorf("%s: got %q, want it to contain %q", c.name, out, c.want)
		}
	}
}

// A component of another file, imported with <import src="…"/>, is called with its props
// checked by lui and imported into the module by lyrac; its part is styled from the
// importing file's global sheet, a `$token` reads the theme the sheet is made from, and
// `--target ui` names the target for a file that does not. A mistake lyrac finds in a token
// (`$colours`) or a script is put back at its markup line by `lui map`.
func TestLui_UiTargetAcrossFilesTokensAndMap(t *testing.T) {
	t.Parallel()
	skipWithoutText(t)
	lui := buildLui(t)
	dir := t.TempDir()
	widgets := `<component name="badge" props={{ words: string }}>
  <style>
    Text { font-size: $font.size }
  </style>
  <Text part="label">{words}</Text>
</component>
`
	view := `<script>
  import ui.style.{ Theme }
  pub data Ev = Go
</script>
<import src="widgets.lui"/>
<style global theme={Theme} name="app_sheet">
  .box { padding: $space.m; background: $colors.surface }
  badge::label { color: $colors.accent }
</style>
<component name="view" props={{ t: Theme }} event={Ev}>
  <Themed theme={t} sheet={app_sheet(t)}>
    <Column class="box" key="top">
      <badge words="hello"/>
      <Button on_click={Go}>Go</Button>
    </Column>
  </Themed>
</component>
`
	for name, text := range map[string]string{"widgets.lui": widgets, "view.lui": view} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"widgets.lui", "view.lui"} {
		if b, err := command(t, lui, "--target", "ui", filepath.Join(dir, name)).CombinedOutput(); err != nil {
			t.Fatalf("lui refused %s: %v\n%s", name, err, b)
		}
	}
	generated, err := os.ReadFile(filepath.Join(dir, "view.lyra"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"import widgets.{ badge }",
		"pub let app_sheet = pure (t: Theme) -> Sheet",
		`Rule { selector: "badge::label", style: Style { color: Some(t.colors.accent) } }, // view.lui:8`,
		`.keyed("top")`,
	} {
		if !strings.Contains(string(generated), want) {
			t.Errorf("view.lyra lacks %q:\n%s", want, generated)
		}
	}
	main := filepath.Join(dir, "main.lyra")
	if err := os.WriteFile(main, []byte(`import view.{ view }
import ui.layout.{ layout, loose }
import ui.style.{ light_theme }
import ui.text.{ fonts_new }
import ui.draw.{ Size }

let main = () -> u8 => {
  var fonts = fonts_new()
  let laid = layout(view(light_theme()), loose(Size { w: 200.0, h: 200.0 }), fonts)
  println("${laid.tag} ${laid.children[0].laid.tag}")
  0
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	setenv(t, "LYRA_STD", repoRoot(t))
	stdout, stderr, code := captureRun(t, "run", main)
	if code != 0 || stdout != "view Themed\n" {
		t.Errorf("running it exited %d, printed %q\nstderr: %s", code, stdout, stderr)
	}

	// A token naming no field of the theme, and a script's mistake: lyrac's errors, mapped.
	broken := strings.Replace(view, "$colors.accent", "$colours.accent", 1)
	broken = strings.Replace(broken, "pub data Ev = Go", "pub data Ev = Go\n  let wrong = () -> bool => 7", 1)
	if err := os.WriteFile(filepath.Join(dir, "view.lui"), []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	if b, err := command(t, lui, "--target", "ui", filepath.Join(dir, "view.lui")).CombinedOutput(); err != nil {
		t.Fatalf("lui refused the broken view: %v\n%s", err, b)
	}
	so, se, _ := captureRun(t, "check", filepath.Join(dir, "view.lyra"))
	cmd := command(t, lui, "map")
	cmd.Stdin = strings.NewReader(so + se)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("lui map: %v\n%s", err, out)
	}
	path := filepath.Join(dir, "view.lui")
	for _, want := range []string{path + ":4: error", path + ":9: error"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("mapped output lacks %q:\n%s", want, out)
		}
	}
}

// The ui target's flex and grid: <Flex> and <Grid> attributes are the style properties they
// name, and every CSS layout property reads in a style or a sheet — flex's shorthand (`1`,
// `none`, `2 0 auto`), a basis as a percentage, order, the alignments, track lists with fr,
// minmax and repeat, a dense column flow, grid lines with spans — a later declaration
// replacing an earlier one, as in CSS; and the module checks clean.
func TestLui_UiTargetFlexAndGrid(t *testing.T) {
	t.Parallel()
	lui := buildLui(t)
	dir := t.TempDir()
	source := `<script target="ui">
  pub data Ev = Go
</script>
<style global name="app_sheet">
  .item { flex: 1; min-width: 120; order: 2; align-self: center }
  .fixed { flex: none; flex-basis: 25% }
  .cell { grid-column: 2 / span 3; grid-row: 1 / 3; justify-self: end }
</style>
<component name="view" event={Ev}>
  <Column>
    <Flex direction="column-reverse" wrap="wrap-reverse" justify="space-between" align="baseline" align-content="stretch" gap="8" row-gap={12.0}>
      <Text class="item">a</Text>
      <Text class="fixed" style="flex: 2 0 auto; max-height: 40">b</Text>
    </Flex>
    <Grid columns="100 1fr minmax(min-content, 2fr) repeat(2, auto max-content)" rows="auto" flow="column dense" justify-items="center" auto-rows="minmax(80, auto)">
      <Text class="cell">c</Text>
    </Grid>
  </Column>
</component>
`
	path := filepath.Join(dir, "view.lui")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	if b, err := command(t, lui, path).CombinedOutput(); err != nil {
		t.Fatalf("lui refused it: %v\n%s", err, b)
	}
	generated, err := os.ReadFile(filepath.Join(dir, "view.lyra"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"flex_direction: Some(FlexColumnReverse), flex_wrap: Some(WrapReverse), justify_content: Some(JustifyBetween), align_items: Some(AlignBaseline), align_content: Some(JustifyStretch), gap: Some(8.0), row_gap: Some(12.0)",
		"grid_columns: Some([px(100.0), fr(1.0), minmax(TrackMinContent, TrackFr(2.0)), ...repeat_tracks(2, [auto_track(), max_content()])])",
		"grid_flow: Some(FlowColumnDense), justify_items: Some(AlignCenter), grid_auto_rows: Some(minmax(TrackPx(80.0), TrackAuto))",
		"grow: Some(1.0), shrink: Some(1.0), basis: Some(BasisLength(0.0)), min_width: Some(120.0), order: Some(2), align_self: Some(AlignCenter)",
		"grow: Some(0.0), shrink: Some(0.0), basis: Some(BasisFraction(25.0 / 100.0)) }",
		"grow: Some(2.0), shrink: Some(0.0), basis: Some(BasisAuto), max_height: Some(40.0)",
		"grid_column: Some(GridSpan { start: 2, span: 3 }), grid_row: Some(GridSpan { start: 1, span: 2 }), justify_self: Some(AlignEnd)",
	} {
		if !strings.Contains(string(generated), want) {
			t.Errorf("the generated module lacks %q:\n%s", want, generated)
		}
	}
	setenv(t, "LYRA_STD", repoRoot(t))
	stdout, stderr, code := captureRun(t, "check", filepath.Join(dir, "view.lyra"))
	if code != 0 || strings.Contains(stdout+stderr, "warning") {
		t.Errorf("checking the generated module exited %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
}

// The layouts example — a wrapping card list and a dashboard grid in markup, a component
// call's class spanning its tile — is what lui makes from it, and checks clean.
func TestLui_LayoutsExampleIsCurrentAndChecks(t *testing.T) {
	t.Parallel()
	skipWithoutText(t)
	lui := buildLui(t)
	root := repoRoot(t)
	if b, err := command(t, lui, "--check", filepath.Join(root, "examples", "ui", "layouts_view.lui")).CombinedOutput(); err != nil {
		t.Fatalf("examples/ui/layouts_view.lyra is stale: %v\n%s", err, b)
	}
	stdout, stderr, code := captureRun(t, "check", filepath.Join(root, "examples", "ui", "layouts.lyra"))
	if code != 0 {
		t.Fatalf("checking the example exited %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if out := stdout + stderr; strings.Contains(out, "warning") {
		t.Errorf("the example or its generated view warns:\n%s", out)
	}
}

// @media in the ui target: a query's features (lengths with or without px, orientation,
// prefers-color-scheme), `and`, a comma and a leading `not` become a rule's []Media, on every
// rule inside the block; a selector list is a rule for each selector, each at its own line,
// part selectors among them; and the module checks clean. In the bindings.imgui.ui target
// @media is refused.
func TestLui_UiTargetMediaAndSelectorLists(t *testing.T) {
	t.Parallel()
	lui := buildLui(t)
	dir := t.TempDir()
	source := `<script target="ui">
  pub data Ev = Go
</script>
<style global name="app_sheet">
  Button.primary, Checkbox::box,
    .card { radius: 8 }
  @media (min-width: 600px) and (orientation: landscape), not (max-height: 400) {
    .grid { grid-template-columns: repeat(4, 1fr) }
    Text, .note { font-size: 15 }
  }
  @media (prefers-color-scheme: dark) {
    .card { background: #222 }
  }
</style>
<component name="view" event={Ev}>
  <Column class="card"><Text class="note">hi</Text></Column>
</component>
`
	path := filepath.Join(dir, "view.lui")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	if b, err := command(t, lui, path).CombinedOutput(); err != nil {
		t.Fatalf("lui refused it: %v\n%s", err, b)
	}
	generated, err := os.ReadFile(filepath.Join(dir, "view.lyra"))
	if err != nil {
		t.Fatal(err)
	}
	media := "media: [Media { min_width: Some(600.0), orientation: Some(Landscape) }, Media { max_height: Some(400.0), not: true }]"
	for _, want := range []string{
		`Rule { selector: "Button.primary", style: Style { radius: Some(8.0) } }, // view.lui:5`,
		`Rule { selector: "Checkbox::box", style: Style { radius: Some(8.0) } }, // view.lui:5`,
		`Rule { selector: ".card", style: Style { radius: Some(8.0) } }, // view.lui:6`,
		`Rule { selector: ".grid", style: Style { grid_columns: Some([...repeat_tracks(4, [fr(1.0)])]) }, ` + media + ` }, // view.lui:8`,
		`Rule { selector: ".note", style: Style { font_size: Some(15.0) }, ` + media + ` }, // view.lui:9`,
		`Rule { selector: ".card", style: Style { background: Some(rgb(0x222222)) }, media: [Media { scheme: Some(DarkMode) }] }, // view.lui:12`,
		"import ui.backend.{ DarkMode }",
	} {
		if !strings.Contains(string(generated), want) {
			t.Errorf("the generated module lacks %q:\n%s", want, generated)
		}
	}
	setenv(t, "LYRA_STD", repoRoot(t))
	stdout, stderr, code := captureRun(t, "check", filepath.Join(dir, "view.lyra"))
	if code != 0 || strings.Contains(stdout+stderr, "warning") {
		t.Errorf("checking the generated module exited %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	imgui := filepath.Join(dir, "imgui.lui")
	if err := os.WriteFile(imgui, []byte("<component name=\"a\">\n  <style>\n    @media (min-width: 1) {\n      Button { color: #fff }\n    }\n  </style>\n  <Text>x</Text>\n</component>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := command(t, lui, imgui).CombinedOutput(); err == nil || !strings.Contains(string(out), "imgui.lui:4: @media is the ui target's") {
		t.Errorf("an @media in the imgui target: %v %s", err, out)
	}
}

// Effects in markup: box-shadow lists (inset, px, spread, none), linear gradients by angle
// and by side, a stop at a percentage, a radial circle, filter and backdrop-filter blur, and
// a compound selector (.card:hover); and the module checks clean.
func TestLui_UiTargetEffects(t *testing.T) {
	t.Parallel()
	lui := buildLui(t)
	dir := t.TempDir()
	source := `<script target="ui">
  pub data Ev = Go
</script>
<style global name="fx_sheet">
  .card { box-shadow: 0 4px 12px #00000033, inset 0 0 0 1 #ffffff80; background: #ffffff }
  .card:hover { box-shadow: 0 10 24 2 #00000040 }
  .header { background: linear-gradient(to right, #ff8a00, #e52e71 60%, #6a11cb) }
  .glow { background-image: radial-gradient(circle, #ffffff, #000000) }
  .tilt { background: linear-gradient(135deg, #ffffff 10%, #000000) }
  .frost { backdrop-filter: blur(12px); filter: blur(2) }
  .plain { filter: none; box-shadow: none }
</style>
<component name="view" event={Ev}>
  <Column class="card"><Text class="header">hi</Text></Column>
</component>
`
	path := filepath.Join(dir, "view.lui")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	if b, err := command(t, lui, path).CombinedOutput(); err != nil {
		t.Fatalf("lui refused it: %v\n%s", err, b)
	}
	generated, err := os.ReadFile(filepath.Join(dir, "view.lyra"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"box_shadow: Some([BoxShadow { x: 0.0, y: 4.0, blur: 12.0, spread: 0.0, color: rgba(0x00000033) }, BoxShadow { x: 0.0, y: 0.0, blur: 0.0, spread: 1.0, color: rgba(0xFFFFFF80), inset: true }])",
		`Rule { selector: ".card:hover", style: Style { box_shadow: Some([BoxShadow { x: 0.0, y: 10.0, blur: 24.0, spread: 2.0, color: rgba(0x00000040) }]) } }`,
		"background_gradient: Some(Gradient { kind: LinearGradient(90.0), stops: [ColorStop { color: rgb(0xFF8A00) }, ColorStop { color: rgb(0xE52E71), at: Some(60.0 / 100.0) }, ColorStop { color: rgb(0x6A11CB) }] })",
		"kind: RadialGradient(true)",
		"kind: LinearGradient(135.0), stops: [ColorStop { color: rgb(0xFFFFFF), at: Some(10.0 / 100.0) }",
		"backdrop_blur: Some(12.0), filter_blur: Some(2.0)",
		"filter_blur: Some(0.0), box_shadow: Some([])",
	} {
		if !strings.Contains(string(generated), want) {
			t.Errorf("the generated module lacks %q:\n%s", want, generated)
		}
	}
	setenv(t, "LYRA_STD", repoRoot(t))
	stdout, stderr, code := captureRun(t, "check", filepath.Join(dir, "view.lyra"))
	if code != 0 || strings.Contains(stdout+stderr, "warning") {
		t.Errorf("checking the generated module exited %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
}

// The effects example — shadows, gradients, a frosted backdrop and a blurred line in markup —
// is what lui makes from it, and checks clean.
func TestLui_EffectsExampleIsCurrentAndChecks(t *testing.T) {
	t.Parallel()
	skipWithoutText(t)
	lui := buildLui(t)
	root := repoRoot(t)
	if b, err := command(t, lui, "--check", filepath.Join(root, "examples", "ui", "effects_view.lui")).CombinedOutput(); err != nil {
		t.Fatalf("examples/ui/effects_view.lyra is stale: %v\n%s", err, b)
	}
	stdout, stderr, code := captureRun(t, "check", filepath.Join(root, "examples", "ui", "effects.lyra"))
	if code != 0 {
		t.Fatalf("checking the example exited %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if out := stdout + stderr; strings.Contains(out, "warning") {
		t.Errorf("the example or its generated view warns:\n%s", out)
	}
}

// Overflow in the ui target: `overflow` with one value sets both axes and with two sets x
// then y, `overflow-x` and `overflow-y` one each, in a sheet and inline; `<window
// overflow>` sets window_spec's overflow, and a window without it leaves the default.
func TestLui_UiTargetOverflow(t *testing.T) {
	t.Parallel()
	lui := buildLui(t)
	dir := t.TempDir()
	source := `<script target="ui">
  pub data Ev = Go
</script>
<window min-width="200" overflow="hidden"/>
<style global name="app_sheet">
  .both { overflow: auto }
  .two { overflow: hidden scroll }
  .one { overflow-x: clip; overflow-y: visible }
</style>
<component name="view" event={Ev}>
  <Column class="both">
    <Column class="two"/>
    <Column class="one" style="overflow-y: auto"/>
  </Column>
</component>
`
	path := filepath.Join(dir, "view.lui")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	if b, err := command(t, lui, path).CombinedOutput(); err != nil {
		t.Fatalf("lui refused it: %v\n%s", err, b)
	}
	generated, err := os.ReadFile(filepath.Join(dir, "view.lyra"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"overflow_x: Some(OverflowAuto), overflow_y: Some(OverflowAuto)",
		"overflow_x: Some(OverflowHidden), overflow_y: Some(OverflowScroll)",
		"overflow_x: Some(OverflowClip), overflow_y: Some(OverflowVisible)",
		"overflow_y: Some(OverflowAuto)",
		"  overflow: OverflowHidden,",
	} {
		if !strings.Contains(string(generated), want) {
			t.Errorf("the generated module lacks %q:\n%s", want, generated)
		}
	}
	setenv(t, "LYRA_STD", repoRoot(t))
	stdout, stderr, code := captureRun(t, "check", filepath.Join(dir, "view.lyra"))
	if code != 0 || strings.Contains(stdout+stderr, "warning") {
		t.Errorf("checking the generated module exited %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	plain := strings.Replace(source, ` overflow="hidden"`, "", 1)
	if err := os.WriteFile(path, []byte(plain), 0o644); err != nil {
		t.Fatal(err)
	}
	if b, err := command(t, lui, path).CombinedOutput(); err != nil {
		t.Fatalf("lui refused it without the window's overflow: %v\n%s", err, b)
	}
	if generated, err = os.ReadFile(filepath.Join(dir, "view.lyra")); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(generated), "overflow: Overflow") {
		t.Errorf("a window without overflow set window_spec's:\n%s", generated)
	}
}

// `<window>` makes window_spec, a function answering a ui.app.WindowSpec for ui.gpu.open_app
// (a function, so a reload reaches the open window): `title`, `width` and
// `height` (the size it opens at), the limits, and `resize`, `resize_x` and `resize_y`, a
// side's own before `resize`; a window without them leaves WindowSpec's defaults.
func TestLui_UiTargetWindowSize(t *testing.T) {
	t.Parallel()
	lui := buildLui(t)
	dir := t.TempDir()
	source := `<script target="ui">
  pub data Ev = Go
</script>
<window title="Sized" width="320" height={240.0 / 2.0} min-width="200" resize={false} resize_x/>
<component name="view" event={Ev}>
  <Column/>
</component>
`
	path := filepath.Join(dir, "view.lui")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	if b, err := command(t, lui, path).CombinedOutput(); err != nil {
		t.Fatalf("lui refused it: %v\n%s", err, b)
	}
	generated, err := os.ReadFile(filepath.Join(dir, "view.lyra"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"pub let window_spec = pure () -> WindowSpec => WindowSpec {",
		"  title: \"Sized\",",
		"  size: Size { w: 320.0, h: 240.0 / 2.0 },",
		"  min_size: Size { w: 200.0, h: 0.0 },",
		"  resize_x: true,",
		"  resize_y: false,",
		"import ui.app.{ WindowSpec }",
	} {
		if !strings.Contains(string(generated), want) {
			t.Errorf("the generated module lacks %q:\n%s", want, generated)
		}
	}
	setenv(t, "LYRA_STD", repoRoot(t))
	stdout, stderr, code := captureRun(t, "check", filepath.Join(dir, "view.lyra"))
	if code != 0 || strings.Contains(stdout+stderr, "warning") {
		t.Errorf("checking the generated module exited %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	plain := strings.Replace(source, ` title="Sized" width="320" height={240.0 / 2.0}`, "", 1)
	plain = strings.Replace(plain, ` resize={false} resize_x`, "", 1)
	if err := os.WriteFile(path, []byte(plain), 0o644); err != nil {
		t.Fatal(err)
	}
	if b, err := command(t, lui, path).CombinedOutput(); err != nil {
		t.Fatalf("lui refused it without the window's size and resizing: %v\n%s", err, b)
	}
	if generated, err = os.ReadFile(filepath.Join(dir, "view.lyra")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(generated), "pub let window_spec = pure () -> WindowSpec => WindowSpec {\n  min_size: Size { w: 200.0, h: 0.0 },\n}") {
		t.Errorf("a window with only a least width should set only that:\n%s", generated)
	}
}

// The overflow example — scrollers down, across, both ways, hidden and nested, in a window
// that scrolls — is what lui makes from it, and checks clean.
func TestLui_OverflowExampleIsCurrentAndChecks(t *testing.T) {
	t.Parallel()
	skipWithoutText(t)
	lui := buildLui(t)
	root := repoRoot(t)
	if b, err := command(t, lui, "--check", filepath.Join(root, "examples", "ui", "overflow_view.lui")).CombinedOutput(); err != nil {
		t.Fatalf("examples/ui/overflow_view.lyra is stale: %v\n%s", err, b)
	}
	stdout, stderr, code := captureRun(t, "check", filepath.Join(root, "examples", "ui", "overflow.lyra"))
	if code != 0 {
		t.Fatalf("checking the example exited %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if out := stdout + stderr; strings.Contains(out, "warning") {
		t.Errorf("the example or its generated view warns:\n%s", out)
	}
}

// Transitions in the ui target: CSS's shorthand — a comma list, each [property] duration
// [timing-function] [delay] in any order, ms or s, all by default — and its longhands, paired
// by place with the shorter lists repeated; lui's radius for border-radius; none; inline too.
func TestLui_UiTargetTransitions(t *testing.T) {
	t.Parallel()
	lui := buildLui(t)
	dir := t.TempDir()
	source := `<script target="ui">
  pub data Ev = Go
</script>
<style global name="app_sheet">
  .a { transition: opacity 200ms, filter 0.3s ease-in-out 50ms, background cubic-bezier(0.1, 0.7, 1.0, 0.1) 1s }
  .b { transition-property: opacity, radius; transition-duration: 150ms; transition-timing-function: steps(4, jump-start), linear; transition-delay: 0, .5s }
  .c { transition: 250ms }
  .d { transition: none }
</style>
<component name="view" event={Ev}>
  <Column class="a" style="transition: color 1s step-end">
    <Column class="b"/>
  </Column>
</component>
`
	path := filepath.Join(dir, "view.lui")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	if b, err := command(t, lui, path).CombinedOutput(); err != nil {
		t.Fatalf("lui refused it: %v\n%s", err, b)
	}
	generated, err := os.ReadFile(filepath.Join(dir, "view.lyra"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`transition: Some([Transition { property: "opacity", duration: 200.0, timing: TimingEase, delay: 0.0 }, Transition { property: "filter", duration: 300.0, timing: TimingEaseInOut, delay: 50.0 }, Transition { property: "background", duration: 1000.0, timing: TimingBezier(0.1, 0.7, 1.0, 0.1), delay: 0.0 }])`,
		`transition: Some([Transition { property: "opacity", duration: 150.0, timing: TimingSteps(4, true), delay: 0.0 }, Transition { property: "border-radius", duration: 150.0, timing: TimingLinear, delay: 500.0 }])`,
		`transition: Some([Transition { property: "all", duration: 250.0, timing: TimingEase, delay: 0.0 }])`,
		`transition: Some([])`,
		`transition: Some([Transition { property: "color", duration: 1000.0, timing: TimingSteps(1, false), delay: 0.0 }])`,
	} {
		if !strings.Contains(string(generated), want) {
			t.Errorf("the generated module lacks %q:\n%s", want, generated)
		}
	}
	setenv(t, "LYRA_STD", repoRoot(t))
	stdout, stderr, code := captureRun(t, "check", filepath.Join(dir, "view.lyra"))
	if code != 0 || strings.Contains(stdout+stderr, "warning") {
		t.Errorf("checking the generated module exited %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
}

// A read-only field in markup, HTML's readonly: bare, it needs no on_edit (one that must never
// be called is written for it); as a condition it keeps its on_edit for while the condition
// does not hold. The module checks clean.
func TestLui_UiTargetReadOnlyField(t *testing.T) {
	t.Parallel()
	lui := buildLui(t)
	dir := t.TempDir()
	source := `<script target="ui">
  pub data Event = Typed(string)
</script>
<component name="view" props={{ count: i64, locked: bool }} event={Event}>
  <TextInput value={"${count}"} readonly />
  <TextInput value="name" readonly={locked} on_edit={Typed}/>
</component>
`
	path := filepath.Join(dir, "view.lui")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	if b, err := command(t, lui, path).CombinedOutput(); err != nil {
		t.Fatalf("lui refused it: %v\n%s", err, b)
	}
	generated, err := os.ReadFile(filepath.Join(dir, "view.lyra"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`text_input(FieldSpec { value: "${count}", read_only: true, on_edit: (_: string) -> Event => panic("a read-only field raised an edit") })`,
		`text_input(FieldSpec { value: "name", read_only: locked, on_edit: (value: string) -> Event => Typed(value) })`,
	} {
		if !strings.Contains(string(generated), want) {
			t.Errorf("the generated module lacks %q:\n%s", want, generated)
		}
	}
	setenv(t, "LYRA_STD", repoRoot(t))
	stdout, stderr, code := captureRun(t, "check", filepath.Join(dir, "view.lyra"))
	if code != 0 || strings.Contains(stdout+stderr, "warning") {
		t.Errorf("checking the generated module exited %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
}
