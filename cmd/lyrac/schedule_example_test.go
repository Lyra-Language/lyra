package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// `examples/schedule/when.lyra` is **the program the zone work was built for**, and this
// is the test of whether the design held: every piece of `std.temporal`'s zone support was
// added because this was argued to need it, and an argument is not a demonstration.
//
// The three things it needs that nothing else here has needed, each one case below: a
// local time resolved into an instant when the zone has it **twice or not at all**,
// calendar arithmetic that keeps the **wall clock** across a transition, and a monthly
// event that does not **drift** when a month is too short for it.
//
// **The dates are found, not written down.** A test that hardcodes "the clocks go forward
// on 2026-03-08" is a test that fails when a country changes its mind, which is what the
// zone database is a record of — so Go locates each zone's real transitions and the cases
// are built around them.
func TestExample_ScheduleAcrossTransitions(t *testing.T) {
	root := repoRoot(t)
	t.Setenv("LYRA_STD", root)
	if _, err := os.Stat("/usr/share/zoneinfo"); err != nil {
		t.Skip("no /usr/share/zoneinfo on this machine")
	}
	bin := filepath.Join(t.TempDir(), "when")
	if _, stderr, code := captureRun(t, "build", "-o", bin,
		filepath.Join(root, "examples", "schedule", "when.lyra")); code != 0 {
		t.Fatalf("building the example exited %d\nstderr: %s", code, stderr)
	}
	run := func(t *testing.T, args ...string) []string {
		t.Helper()
		out, err := exec.Command(bin, args...).Output()
		if err != nil {
			t.Fatalf("running %v failed: %v", args, err)
		}
		return strings.Split(strings.TrimSpace(string(out)), "\n")
	}

	// **A monthly event on the 31st, which most months do not have.** Every occurrence is
	// computed from the first, so February clamps to the 28th and March is the 31st again;
	// computed from the one before, the event would walk backwards through the year and
	// never return. No zone is involved, so this holds whatever the database says.
	t.Run("a short month does not move the event permanently", func(t *testing.T) {
		got := run(t, "monthly 31 12:00", "--zone", "UTC", "--from", "2026-01-01", "--count", "4")
		want := []string{
			"2026-01-31T12:00:00Z[UTC]",
			"2026-02-28T12:00:00Z[UTC]",
			"2026-03-31T12:00:00Z[UTC]",
			"2026-04-30T12:00:00Z[UTC]",
		}
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	})

	// **A weekly event keeps its wall clock across a transition**, and its offset does not.
	// That is calendar arithmetic; 168 hours would have kept the offset and moved the
	// clock, which is the distinction the whole `Duration` split exists for.
	t.Run("a weekly event keeps its clock and changes its offset", func(t *testing.T) {
		loc, err := time.LoadLocation("Europe/Berlin")
		if err != nil {
			t.Skip("Go cannot load Europe/Berlin")
		}
		moment, ok := firstTransitionAfter(loc, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
		if !ok {
			t.Skip("Berlin no longer changes its clocks, and this case is about when it does")
		}
		start := time.Unix(moment, 0).In(loc).AddDate(0, 0, -14).Format("2006-01-02")
		got := run(t, "weekly Sunday 09:00", "--zone", "Europe/Berlin", "--from", start, "--count", "4")
		offsets := map[string]bool{}
		for _, line := range got {
			if !strings.Contains(line, "T09:00:00") {
				t.Errorf("an occurrence lost its wall clock: %q", line)
			}
			offsets[line[strings.Index(line, "+"):strings.Index(line, "[")]] = true
		}
		if len(offsets) < 2 {
			t.Errorf("no transition was crossed, so this proves nothing:\n%s", strings.Join(got, "\n"))
		}
	})

	// **A daily event at a time the clocks skip.** It must be reported and moved, not
	// silently dropped and not silently kept: an alarm that quietly becomes an hour later
	// once a year is the bug this whole slice is about.
	t.Run("a time that does not exist is moved, and said so", func(t *testing.T) {
		loc, err := time.LoadLocation("America/New_York")
		if err != nil {
			t.Skip("Go cannot load America/New_York")
		}
		moment, ok := firstTransitionAfter(loc, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
		if !ok {
			t.Skip("New York no longer changes its clocks")
		}
		spring := time.Unix(moment, 0).In(loc)
		if _, offset := spring.Zone(); offset < 0 {
			// The first transition of the year going *back* would be a fall-back zone;
			// these cases want the forward one.
			if _, before := time.Unix(moment-1, 0).In(loc).Zone(); before > offset {
				t.Skip("the first 2026 transition here is the one that goes back")
			}
		}
		start := spring.AddDate(0, 0, -1).Format("2006-01-02")
		got := run(t, "daily 02:30", "--zone", "America/New_York", "--from", start, "--count", "3")
		var moved string
		for _, line := range got {
			if strings.Contains(line, "does not exist here") {
				moved = line
			}
		}
		if moved == "" {
			t.Fatalf("no occurrence was reported as missing:\n%s", strings.Join(got, "\n"))
		}
		if !strings.Contains(moved, "T03:30:00") {
			t.Errorf("the moved occurrence should be an hour later: %q", moved)
		}
	})

	// **The repeated hour, under each rule.** The default takes the first reading, `later`
	// takes the second, and `reject` refuses — and the two readings are an hour apart in
	// instants while identical on the clock, which is the whole difficulty.
	t.Run("a time that happens twice is chosen by the rule", func(t *testing.T) {
		loc, err := time.LoadLocation("America/New_York")
		if err != nil {
			t.Skip("Go cannot load America/New_York")
		}
		fall, ok := fallBackAfter(loc, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
		if !ok {
			t.Skip("New York no longer puts its clocks back")
		}
		day := time.Unix(fall, 0).In(loc).Format("2006-01-02")
		first := run(t, "daily 01:30", "--zone", "America/New_York", "--from", day, "--count", "1")[0]
		later := run(t, "daily 01:30", "--zone", "America/New_York", "--from", day, "--count", "1", "--rule", "later")[0]
		refused := run(t, "daily 01:30", "--zone", "America/New_York", "--from", day, "--count", "1", "--rule", "reject")[0]
		for _, line := range []string{first, later} {
			if !strings.Contains(line, "happens twice here") {
				t.Errorf("the repeated hour was not reported: %q", line)
			}
		}
		if first == later {
			t.Errorf("the two rules chose the same instant: %q", first)
		}
		if !strings.Contains(refused, "refused") {
			t.Errorf("`reject` did not refuse: %q", refused)
		}
	})
}

// firstTransitionAfter is the zone's next change of offset after `from`, found by walking
// days — the same reason the other zone tests walk rather than bisect: the predicate is
// not monotone over a year.
func firstTransitionAfter(loc *time.Location, from time.Time) (int64, bool) {
	at := from
	end := from.AddDate(2, 0, 0)
	_, previous := at.In(loc).Zone()
	for at.Before(end) {
		next := at.AddDate(0, 0, 1)
		if _, offset := next.In(loc).Zone(); offset != previous {
			return transitionBetween(loc, at, next), true
		}
		at = next
	}
	return 0, false
}

// fallBackAfter is the next transition whose offset *decreases* — the one that repeats an
// hour rather than skipping one.
func fallBackAfter(loc *time.Location, from time.Time) (int64, bool) {
	at := from
	for i := 0; i < 8; i++ {
		moment, ok := firstTransitionAfter(loc, at)
		if !ok {
			return 0, false
		}
		_, before := time.Unix(moment-1, 0).In(loc).Zone()
		_, after := time.Unix(moment, 0).In(loc).Zone()
		if after < before {
			return moment, true
		}
		at = time.Unix(moment, 0).UTC().AddDate(0, 0, 1)
	}
	return 0, false
}
