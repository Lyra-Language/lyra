package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// `examples/calendar/calendar.lyra` is `std.temporal`'s forcing function: a month view
// in the terminal, and nothing goes into the library until it needs it. Its first slice
// is the grid — calendar arithmetic, ISO weekdays and week numbers, and "today" in the
// local zone.
//
// The interactive mode is a loop over the same pure functions, so this pins `--print`,
// which renders one month as plain text and depends on nothing but the date it is given.
// The months are chosen for their edges: one starting on a Tuesday, one starting on the
// grid's last column, and one whose first row is ISO week 53 of the *previous* year.
func TestExample_CalendarPrintsAMonth(t *testing.T) {
	root := repoRoot(t)
	t.Setenv("LYRA_STD", root)
	bin := filepath.Join(t.TempDir(), "calendar")
	if _, stderr, code := captureRun(t, "build", "-o", bin,
		filepath.Join(root, "examples", "calendar", "calendar.lyra")); code != 0 {
		t.Fatalf("building the example exited %d\nstderr: %s", code, stderr)
	}
	print := func(t *testing.T, date string) string {
		t.Helper()
		out, err := exec.Command(bin, "--print", date).Output()
		if err != nil {
			t.Fatalf("--print %s: %v", date, err)
		}
		return string(out)
	}

	// Every grid row is exactly 26 columns — the width the interactive frame lays the
	// panel beside — so the trailing spaces are part of what is pinned.
	const september = "" +
		"      September 2026      \n" +
		"  Mo Tu We Th Fr Sa Su  wk\n" +
		"      1  2  3  4  5  6  36\n" +
		"   7  8  9 10 11 12 13  37\n" +
		"  14 15 16 17>18 19 20  38\n" +
		"  21 22 23 24 25 26 27  39\n" +
		"  28 29 30              40\n" +
		"                          \n" +
		"Friday 18 September 2026\n" +
		"2026-09-18\n" +
		"\n" +
		"day 261 of 365, week 38\n" +
		"30 days in September\n" +
		"\n" +
		"today\n"
	if got := print(t, "2026-09-18"); got != september {
		t.Errorf("September 2026 =\n%s\nwant\n%s", got, september)
	}

	// A month starting on Sunday puts its first day in the last column.
	if got := print(t, "2026-02-01"); !strings.Contains(got, "                   > 1   5\n") {
		t.Errorf("February 2026 should start in the Sunday column of week 5:\n%s", got)
	}

	// **The first row of January 2021 is week 53 — of 2020.** ISO weeks belong to the year
	// holding their Thursday, and 1 January 2021 was a Friday.
	if got := print(t, "2021-01-03"); !strings.Contains(got, "               1  2> 3  53\n") ||
		!strings.Contains(got, "   4  5  6  7  8  9 10   1\n") {
		t.Errorf("January 2021 should open with week 53 and then week 1:\n%s", got)
	}

	// Every month is eight rows of grid, so the interactive frame never changes height —
	// and every row is exactly 26 columns. This is what caught the weekday header one
	// column short (09/18), sitting `Mo` over the space before the day's digits: the exact
	// expectation above had been copied from that output and agreed with it.
	for _, date := range []string{"2026-02-01", "2026-03-01", "2026-08-31"} {
		lines := strings.Split(print(t, date), "\n")
		for i := 0; i < 8; i++ {
			if len([]rune(lines[i])) != 26 {
				t.Errorf("%s: grid row %d is %d columns, want 26: %q", date, i, len([]rune(lines[i])), lines[i])
			}
		}
	}

	// A date that does not exist is a usage error, reported against what was typed.
	out, err := exec.Command(bin, "--print", "2026-02-30").CombinedOutput()
	if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 2 {
		t.Errorf("--print 2026-02-30 should exit 2, got %v", err)
	}
	if !strings.Contains(string(out), "2026-02-30") {
		t.Errorf("the usage error should name the bad date: %q", out)
	}
}
