package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// `examples/checksum/` is the standard library's third probe, and the first program here
// made of arithmetic rather than text: `u32` that wraps rather than traps, shifts, xor, and
// a file read as numbers. Everything else in `examples/` is strings, structs and files.
//
// **It is also the only example with an answer somebody else computed.** A renderer's test
// can only say the output is what the renderer produced the day it was written; a digest is
// specified, so this compares against Go's `crypto/sha256` — a different implementation, by
// different people, from the same standard. That is the whole reason to write a hash as a
// probe: a wrong `wrapping_add`, a rotate off by one bit or a padding length written
// little-endian all produce a confident, plausible, wrong digest.
//
// The sizes are the ones padding gets wrong: a block is 64 bytes and the length occupies
// the last 8, so 55 is the largest message that fits its own block, 56 forces a second one
// that is all padding, and 64 is an exact block whose padding is a whole extra one.
func TestExample_ChecksumMatchesCryptoSHA256(t *testing.T) {
	root := repoRoot(t)
	t.Setenv("LYRA_STD", root)
	bin := filepath.Join(t.TempDir(), "checksum")
	if _, stderr, code := captureRun(t, "build", "-o", bin,
		filepath.Join(root, "examples", "checksum", "checksum.lyra")); code != 0 {
		t.Fatalf("building the example exited %d\nstderr: %s", code, stderr)
	}

	dir := t.TempDir()
	random := rand.New(rand.NewSource(1))
	for _, n := range []int{0, 1, 2, 3, 55, 56, 57, 63, 64, 65, 119, 120, 127, 128, 1000, 65537} {
		t.Run(fmt.Sprintf("%d bytes", n), func(t *testing.T) {
			data := make([]byte, n)
			random.Read(data)
			path := filepath.Join(dir, fmt.Sprintf("f%d.bin", n))
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command(bin, path).Output()
			if err != nil {
				t.Fatalf("hashing %d bytes failed: %v", n, err)
			}
			sum := sha256.Sum256(data)
			want := hex.EncodeToString(sum[:]) + "  " + path + "\n"
			if string(out) != want {
				t.Errorf("for %d bytes the digest is\n  %s\nwant\n  %s", n, out, want)
			}
		})
	}

	// Standard input has no name, and is written `-` as every checksum tool writes it.
	stdin := exec.Command(bin, "-")
	stdin.Stdin = strings.NewReader("hello world\n")
	out, err := stdin.Output()
	if err != nil {
		t.Fatalf("hashing standard input failed: %v", err)
	}
	sum := sha256.Sum256([]byte("hello world\n"))
	if want := hex.EncodeToString(sum[:]) + "  -\n"; string(out) != want {
		t.Errorf("standard input hashed to\n  %s\nwant\n  %s", out, want)
	}
}

// **`--check` is the half that has to fail correctly.** A checker that says OK to
// everything passes every test made of files that match, so the cases here are the ones
// where it must not: a file whose bytes changed, and a file that is no longer there.
func TestExample_ChecksumChecksAList(t *testing.T) {
	root := repoRoot(t)
	t.Setenv("LYRA_STD", root)
	bin := filepath.Join(t.TempDir(), "checksum")
	if _, stderr, code := captureRun(t, "build", "-o", bin,
		filepath.Join(root, "examples", "checksum", "checksum.lyra")); code != 0 {
		t.Fatalf("building the example exited %d\nstderr: %s", code, stderr)
	}
	dir := t.TempDir()
	kept := filepath.Join(dir, "kept.txt")
	changed := filepath.Join(dir, "changed.txt")
	gone := filepath.Join(dir, "gone.txt")
	for path, body := range map[string]string{kept: "kept\n", changed: "before\n", gone: "here\n"} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	list := filepath.Join(dir, "sums.txt")
	sums, err := exec.Command(bin, kept, changed, gone).Output()
	if err != nil {
		t.Fatalf("writing the list failed: %v", err)
	}
	if err := os.WriteFile(list, sums, 0o644); err != nil {
		t.Fatal(err)
	}

	// Nothing has moved yet: every line matches and the exit code says so.
	if out, err := exec.Command(bin, "--check", list).Output(); err != nil {
		t.Errorf("checking an untouched list exited non-zero: %v\n%s", err, out)
	} else if got := strings.Count(string(out), ": OK"); got != 3 {
		t.Errorf("checking an untouched list reported %d OK, want 3:\n%s", got, out)
	}

	// Now one file's bytes differ and another is missing.
	if err := os.WriteFile(changed, []byte("after\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(bin, "--check", list).Output()
	if err == nil {
		t.Errorf("a list with a changed and a missing file exited 0:\n%s", out)
	}
	for _, want := range []string{"kept.txt: OK", "changed.txt: FAILED", "gone.txt: cannot read"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("the report has no %q:\n%s", want, out)
		}
	}
}
