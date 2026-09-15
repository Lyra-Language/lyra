package main

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// `examples/config/config.lyra` is the program that drove conversion traits: `?` across
// two error types through `impl From<JsonError> for ConfigError`, and typed field decoding
// through a receiver-less `FromJson::from_json` chosen by the annotation, which reaches the
// call through `?`. Each input exercises one path, and the wrong-kind message is the one
// worth pinning: its "should be" half comes from the impl the annotation picked.
func TestExample_Config(t *testing.T) {
	root := repoRoot(t)
	t.Setenv("LYRA_STD", root)
	bin := filepath.Join(t.TempDir(), "config")
	if _, stderr, code := captureRun(t, "build", "-o", bin, filepath.Join(root, "examples", "config", "config.lyra")); code != 0 {
		t.Fatalf("building the example exited %d\nstderr: %s", code, stderr)
	}
	for _, c := range []struct {
		file, want string
		code       int
	}{
		{"server.json", "http://localhost:8080 with 4 workers\n", 0},
		{"bad_port.json", "config: field \"port\" should be a whole number, found string\n", 1},
		{"malformed.json", "config: not valid JSON at byte 37: trailing comma in an object\n", 1},
		{"nowhere.json", "config: cannot read " + filepath.Join(root, "examples", "config", "nowhere.json") + "\n", 1},
	} {
		t.Run(c.file, func(t *testing.T) {
			out, err := exec.Command(bin, filepath.Join(root, "examples", "config", c.file)).CombinedOutput()
			code := 0
			if err != nil {
				ee, ok := err.(*exec.ExitError)
				if !ok {
					t.Fatalf("running the example failed: %v\n%s", err, out)
				}
				code = ee.ExitCode()
			}
			if string(out) != c.want || code != c.code {
				t.Errorf("%s gave (exit %d):\n%s\nwant (exit %d):\n%s", c.file, code, out, c.code, c.want)
			}
		})
	}
}
