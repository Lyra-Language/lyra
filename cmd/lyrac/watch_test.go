package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// One watch session through its whole life: a program that never exits is replaced
// when its source changes, a broken save leaves it running, and the fix replaces it
// with one that exits — which is reported, not fatal.
func TestWatch_RestartsOnChangeAndSurvivesABrokenSave(t *testing.T) {
	requireCC(t)
	path := filepath.Join(t.TempDir(), "prog.lyra")
	save := func(src string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		// Two saves inside the file system's timestamp resolution would look like
		// one, so each is dated a little later than the last.
		stamp := time.Now().Add(time.Duration(len(src)) * time.Second)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	save("let main = () => {\n  println(\"first\")\n  for true {\n  }\n}\n")

	stdout, stderr, restore := captureStreams(t)
	stop := make(chan os.Signal, 1)
	result := make(chan int, 1)
	go func() { result <- watch(buildOptions{path: path, opt: "-O0"}, stop, 20*time.Millisecond) }()
	finish := func() {
		t.Helper()
		stop <- os.Interrupt
		select {
		case code := <-result:
			restore()
			if code != 0 {
				t.Errorf("watch returned %d after the interrupt, want 0", code)
			}
		case <-time.After(30 * time.Second):
			restore()
			t.Fatal("watch did not return after the interrupt")
		}
	}

	if !waitFor(stdout, "first\n") {
		finish()
		t.Fatalf("the first build never ran\nstderr: %s", stderr)
	}

	save("let main = () => {\n  let x: i32 = \"broken\"\n}\n")
	if !waitFor(stderr, "the previous build keeps running") {
		finish()
		t.Fatalf("a broken save was not reported as such\nstderr: %s", stderr)
	}
	if !strings.Contains(stderr.String(), "error") {
		t.Errorf("the broken save's diagnostic is missing\nstderr: %s", stderr)
	}

	save("let main = () => {\n  println(\"second\")\n}\n")
	if !waitFor(stderr, "exited with status 0; waiting for changes") {
		finish()
		t.Fatalf("the fixed program did not run to its exit\nstdout: %s\nstderr: %s", stdout, stderr)
	}
	finish()
	if got := stdout.String(); got != "first\nsecond\n" {
		t.Errorf("stdout = %q, want each build's output once, in order", got)
	}
}

func TestWatch_IsARunFlag(t *testing.T) {
	if o, ok := parseBuildArgs("run", []string{"--watch", "prog.lyra"}); !ok || !o.watch {
		t.Errorf("run --watch: ok=%v watch=%v, want both true", ok, o.watch)
	}
	if _, ok := parseBuildArgs("build", []string{"--watch", "prog.lyra"}); ok {
		t.Error("build --watch was accepted; it runs nothing to restart")
	}
}

func TestChangedFile(t *testing.T) {
	t0 := time.Unix(1000, 0)
	before := map[string]fileStamp{"a": {t0, 10}, "b": {t0, 20}}
	cases := []struct {
		name  string
		after map[string]fileStamp
		want  string
	}{
		{"unchanged", map[string]fileStamp{"a": {t0, 10}, "b": {t0, 20}}, ""},
		{"newer", map[string]fileStamp{"a": {t0, 10}, "b": {t0.Add(time.Second), 20}}, "b"},
		// An editor can rewrite a file within one timestamp tick; the size still moves.
		{"same time, new size", map[string]fileStamp{"a": {t0, 11}, "b": {t0, 20}}, "a"},
		{"deleted", map[string]fileStamp{"a": {}, "b": {t0, 20}}, "a"},
		{"newly read", map[string]fileStamp{"a": {t0, 10}, "b": {t0, 20}, "c": {t0, 1}}, "c"},
	}
	for _, c := range cases {
		if got := changedFile(before, c.after); got != c.want {
			t.Errorf("%s: changedFile = %q, want %q", c.name, got, c.want)
		}
	}
}

// captureStreams points os.Stdout and os.Stderr at buffers for a test that, unlike
// captureRun's, keeps running while it reads them. restore puts the streams back and
// waits for the last of the output.
func captureStreams(t *testing.T) (stdout, stderr *syncBuffer, restore func()) {
	t.Helper()
	origOut, origErr := os.Stdout, os.Stderr
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = outW, errW
	stdout, stderr = &syncBuffer{}, &syncBuffer{}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _, _ = io.Copy(stdout, outR) }()
	go func() { defer wg.Done(); _, _ = io.Copy(stderr, errR) }()
	return stdout, stderr, func() {
		os.Stdout, os.Stderr = origOut, origErr
		_ = outW.Close()
		_ = errW.Close()
		wg.Wait()
	}
}

// waitFor reports whether b comes to contain s within the time a build takes.
func waitFor(b *syncBuffer, s string) bool {
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(b.String(), s) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// syncBuffer is a bytes.Buffer safe to write from one goroutine while another reads.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
