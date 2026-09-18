package main

import (
	"os"
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

	// --- events (09/18) --------------------------------------------------------------
	//
	// The file is ISO 8601 throughout, so every field goes through std.temporal's own
	// parser. What is pinned is what a reader relies on: all-day first and then by start
	// time whatever order the file used, an end computed by `PlainDateTime.add` — so the
	// deploy running past midnight says `+1` — a start with no duration shown as a moment,
	// and a day with events marked `*` in the grid (the selected day's `>` wins). `--print`
	// takes the printed date's midnight as "now", so the relative times are fixed.
	events := filepath.Join(t.TempDir(), "events.txt")
	writeFile(t, events, "# a comment\n"+
		"2026-09-18T14:30 PT30M Standup\n"+
		"2026-09-18T09:00 PT1H Planning\n"+
		"\n"+
		"2026-09-18 Release day\n"+
		"2026-09-18T23:30 PT2H Late deploy\n"+
		"2026-09-18T17:00 Ship it\n"+
		"2026-09-20 Alice's birthday\n")
	withEvents, err := exec.Command(bin, "--events", events, "--print", "2026-09-18").Output()
	if err != nil {
		t.Fatalf("--events: %v", err)
	}
	const agenda = "" +
		"all day       Release day\n" +
		"09:00-10:00   Planning  in 9h\n" +
		"14:30-15:00   Standup  in 14h 30m\n" +
		"17:00         Ship it  in 17h\n" +
		"23:30-01:30+1 Late deploy  in 23h 30m\n"
	if got := string(withEvents); !strings.HasSuffix(got, "today\n\n"+agenda) {
		t.Errorf("the day's events should end the panel, sorted:\n%s\nwant a suffix of\n%s", got, agenda)
	}
	if got := string(withEvents); !strings.Contains(got, "  14 15 16 17>18 19*20  38\n") {
		t.Errorf("the 20th has an event and should be marked:\n%s", got)
	}

	// The sample file shipped beside the example parses, so the command its header gives
	// works — it is the first thing anyone trying the calendar runs.
	sample := filepath.Join(root, "examples", "calendar", "events.txt")
	if out, err := exec.Command(bin, "--events", sample, "--print", "2026-09-18").CombinedOutput(); err != nil ||
		!strings.Contains(string(out), "Release day") {
		t.Errorf("examples/calendar/events.txt should load and show 18 September's events: %v\n%s", err, out)
	}

	// **A line that does not parse stops the calendar**, naming the file, the line and the
	// word: a calendar that silently drops an event is worse than one that refuses to
	// start, since the event that is not there is the one thing its user cannot see.
	for _, c := range []struct{ file, want string }{
		{"2026-09-18T09:00 PT1H Planning\n2026-02-30 Nope\n", ":2: `2026-02-30` is not a date"},
		{"2026-09-18T09:00 PT1X Planning\n", ":1: `PT1X` is not an ISO duration"},
		{"2026-09-18T25:00 Too late\n", ":1: `2026-09-18T25:00` is not a date-time"},
		{"2026-09-18\n", ":1: the event on 2026-09-18 has no title"},
	} {
		bad := filepath.Join(t.TempDir(), "bad.txt")
		writeFile(t, bad, c.file)
		out, err := exec.Command(bin, "--events", bad, "--print", "2026-09-18").CombinedOutput()
		if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 1 {
			t.Errorf("%q should exit 1, got %v", c.file, err)
		}
		if !strings.Contains(string(out), c.want) {
			t.Errorf("%q: want %q in\n%s", c.file, c.want, out)
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

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
