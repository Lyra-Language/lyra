package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// **Disambiguation** (09/22) — the direction where a wall clock is not a moment. Where the
// clocks go forward `02:30` never happens; where they go back `01:30` happens twice, an
// hour apart. `Compatible`, `Earlier`, `Later` and `Reject` are the four answers Temporal
// admits, and this checks all of them at every real transition of six zones.
//
// **Python's `zoneinfo` is the oracle, not Go's `time`.** Go's documentation says outright
// that for an ambiguous or missing local time "the choice of time zone, and therefore the
// time, is not guaranteed" — so it cannot say what the right answer is. Python implements
// PEP 495's `fold`, which is specified, and maps onto these rules exactly: for an ambiguous
// time `fold=0` is the earlier instant and `fold=1` the later, and for a gap the two swap,
// `fold=0` being the reading after the jump. **An oracle has to be pinned down harder than
// the thing it is judging**, and the widely-used implementation was the wrong one here.
//
// The local times tested are found by walking each zone's transitions and stepping ±90,
// ±30 and 0 minutes around the readings either side, in *wall-clock* arithmetic — which is
// what lands inside a 30-minute gap (Lord Howe) as readily as a 60-minute one.
func TestExample_DisambiguationMatchesPython(t *testing.T) {
	root := repoRoot(t)
	t.Setenv("LYRA_STD", root)
	if _, err := os.Stat("/usr/share/zoneinfo"); err != nil {
		t.Skip("no /usr/share/zoneinfo on this machine")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not on PATH, and it is the oracle here")
	}
	if out, err := exec.Command(python, "-c", "import zoneinfo").CombinedOutput(); err != nil {
		t.Skipf("python3 has no zoneinfo: %s", out)
	}

	probe := filepath.Join(t.TempDir(), "probe.lyra")
	if err := os.WriteFile(probe, []byte(`module main
import std.io.{ read_stdin }
import std.temporal.{
  PlainDateTime, TimeZone, Disambiguation, load_time_zone, parse_plain_date_time,
  to_zoned_date_time, possible_instants, to_instant, epoch_second,
  Compatible, Earlier, Later,
}

/// One "<zone> <local>" per line in, one "<count> <compatible> <earlier> <later>" out —
/// the instants as epoch seconds, so the comparison is of moments and not of spellings.
let main = () -> u8 => {
  for line in read_stdin().lines() {
    if line.trim().len() == 0 { continue }
    let parts = line.trim().split(" ")
    let Some(zone) = load_time_zone(parts[0]) else {
      println("no zone")
      continue
    }
    let Some(local) = parse_plain_date_time(parts[1]) else {
      println("no local")
      continue
    }
    println("${possible_instants(local, zone).len()} ${resolve(local, zone, Compatible)} " ++
      "${resolve(local, zone, Earlier)} ${resolve(local, zone, Later)}")
  }
  0
}

let resolve = pure (local: PlainDateTime, zone: TimeZone, how: Disambiguation) -> string =>
  match local.to_zoned_date_time(zone, how) {
    Some(z) => "${z.to_instant().epoch_second()}",
    None => "none",
  }
`), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "probe")
	if _, stderr, code := captureRun(t, "build", "-o", bin, probe); code != 0 {
		t.Fatalf("building the probe exited %d\nstderr: %s", code, stderr)
	}

	oracle := filepath.Join(t.TempDir(), "oracle.py")
	if err := os.WriteFile(oracle, []byte(`import sys
from datetime import datetime
from zoneinfo import ZoneInfo
for line in sys.stdin:
    line = line.strip()
    if not line: continue
    name, local = line.split(" ")
    d = datetime.fromisoformat(local).replace(tzinfo=ZoneInfo(name))
    t0 = int(d.replace(fold=0).timestamp())
    t1 = int(d.replace(fold=1).timestamp())
    if t0 == t1:
        print(f"1 {t0} {t0} {t0}")
    elif t1 > t0:
        print(f"2 {t0} {t0} {t1}")
    else:
        print(f"0 {t0} {t1} {t0}")
`), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{
		"America/New_York",
		"Europe/Berlin",
		"Australia/Lord_Howe", // a *thirty minute* gap, where an hour is usually assumed
		"Pacific/Chatham",     // +12:45 either side of its transitions
		"America/Sao_Paulo",   // transitions at midnight: the day itself begins at 01:00
		"Asia/Kolkata",        // never changes, so every reading must be unambiguous
	} {
		t.Run(name, func(t *testing.T) {
			loc, err := time.LoadLocation(name)
			if err != nil {
				t.Skipf("Go cannot load %s either: %v", name, err)
			}
			var input strings.Builder
			locals := localsAroundTransitions(loc)
			for _, local := range locals {
				fmt.Fprintf(&input, "%s %s\n", name, local)
			}
			got := runFeeding(t, bin, nil, input.String())
			want := runFeeding(t, python, []string{oracle}, input.String())
			gotLines, wantLines := strings.Split(strings.TrimSpace(got), "\n"), strings.Split(strings.TrimSpace(want), "\n")
			if len(gotLines) != len(wantLines) {
				t.Fatalf("%s: %d lines from Lyra, %d from the oracle", name, len(gotLines), len(wantLines))
			}
			for i := range gotLines {
				if gotLines[i] != wantLines[i] {
					t.Errorf("%s %s: got %q, want %q", name, locals[i], gotLines[i], wantLines[i])
				}
			}
		})
	}
}

func runFeeding(t *testing.T, bin string, args []string, input string) string {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s failed: %v", filepath.Base(bin), err)
	}
	return string(out)
}

// localsAroundTransitions is every wall-clock reading worth asking about: the readings
// either side of each of the zone's transitions, stepped by ±90 and ±30 minutes in
// *wall-clock* arithmetic, which is what lands inside the gap or the repeated hour.
func localsAroundTransitions(loc *time.Location) []string {
	var out []string
	seen := map[string]bool{}
	at := time.Date(1995, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2035, 1, 1, 0, 0, 0, 0, time.UTC)
	_, previous := at.In(loc).Zone()
	for at.Before(end) {
		next := at.AddDate(0, 0, 1)
		if _, offset := next.In(loc).Zone(); offset != previous {
			moment := transitionBetween(loc, at, next)
			previous = offset
			for _, side := range []int64{moment - 1, moment} {
				reading := time.Unix(side, 0).In(loc)
				naive := time.Date(reading.Year(), reading.Month(), reading.Day(),
					reading.Hour(), reading.Minute(), reading.Second(), 0, time.UTC)
				for _, step := range []time.Duration{-90 * time.Minute, -30 * time.Minute, 0,
					30 * time.Minute, 90 * time.Minute} {
					local := naive.Add(step).Format("2006-01-02T15:04:05")
					if !seen[local] {
						seen[local] = true
						out = append(out, local)
					}
				}
			}
		}
		at = next
	}
	return out
}

// **The calendar units move the clock; the clock units move the timeline.** A week after
// 09:00 is 09:00 whatever the zone did in between, because "next Monday's meeting" is a
// statement about a wall clock. 168 hours after 09:00 is 08:00 or 10:00 if the clocks
// changed, because an hour is an hour. Temporal separates a `Duration`'s calendar and clock
// fields for this, and it is the payoff of the whole zone slice: the two answers differ by
// exactly one hour across a transition, and a program that cannot say which it meant will
// be wrong twice a year.
//
// Python says the same thing in its own idiom: a naive add and then localize is the
// calendar rule, and `timedelta` on an aware datetime is the exact one.
func TestExample_ZonedArithmeticMatchesPython(t *testing.T) {
	root := repoRoot(t)
	t.Setenv("LYRA_STD", root)
	if _, err := os.Stat("/usr/share/zoneinfo"); err != nil {
		t.Skip("no /usr/share/zoneinfo on this machine")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not on PATH, and it is the oracle here")
	}

	probe := filepath.Join(t.TempDir(), "probe.lyra")
	if err := os.WriteFile(probe, []byte(`module main
import std.io.{ read_stdin }
import std.temporal.{
  ZonedDateTime, Duration, load_time_zone, parse_plain_date_time, to_zoned_date_time,
  add, to_instant, epoch_second, days, weeks, hours,
}

/// "<zone> <local>" in; the instants of +1 day, +1 week and +24 hours out.
let main = () -> u8 => {
  for line in read_stdin().lines() {
    if line.trim().len() == 0 { continue }
    let parts = line.trim().split(" ")
    let Some(zone) = load_time_zone(parts[0]) else { continue }
    let Some(local) = parse_plain_date_time(parts[1]) else { continue }
    let Some(start) = local.to_zoned_date_time(zone) else { continue }
    println("${moved(start, days(1))} ${moved(start, weeks(1))} ${moved(start, hours(24))}")
  }
  0
}

let moved = pure (start: ZonedDateTime, amount: Duration) -> string =>
  match start.add(amount) {
    Some(z) => "${z.to_instant().epoch_second()}",
    None => "none",
  }
`), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "probe")
	if _, stderr, code := captureRun(t, "build", "-o", bin, probe); code != 0 {
		t.Fatalf("building the probe exited %d\nstderr: %s", code, stderr)
	}

	oracle := filepath.Join(t.TempDir(), "oracle.py")
	if err := os.WriteFile(oracle, []byte(`import sys
from datetime import datetime, timedelta, timezone
from zoneinfo import ZoneInfo
for line in sys.stdin:
    line = line.strip()
    if not line: continue
    name, local = line.split(" ")
    zone = ZoneInfo(name)
    start = datetime.fromisoformat(local).replace(tzinfo=zone)
    naive = start.replace(tzinfo=None)
    # Calendar units: move the wall clock, then ask the zone what instant that is.
    day = (naive + timedelta(days=1)).replace(tzinfo=zone)
    week = (naive + timedelta(weeks=1)).replace(tzinfo=zone)
    # Clock units: move the *instant*. Adding a timedelta to an aware datetime is
    # wall-clock arithmetic in Python -- documented, and it silently produced the calendar
    # answer here -- so this goes through UTC, where an hour is an hour.
    exact = (start.astimezone(timezone.utc) + timedelta(hours=24)).astimezone(zone)
    print(f"{int(day.timestamp())} {int(week.timestamp())} {int(exact.timestamp())}")
`), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"America/New_York", "Europe/Berlin", "Australia/Lord_Howe"} {
		t.Run(name, func(t *testing.T) {
			loc, err := time.LoadLocation(name)
			if err != nil {
				t.Skipf("Go cannot load %s either: %v", name, err)
			}
			// Starts a day either side of each transition, so `+1 day` and `+1 week` both
			// step over one — which is where the two rules part company.
			var input strings.Builder
			var locals []string
			for _, local := range localsAroundTransitions(loc) {
				at, err := time.Parse("2006-01-02T15:04:05", local)
				if err != nil {
					continue
				}
				for _, shift := range []int{-1, -7} {
					from := at.AddDate(0, 0, shift).Format("2006-01-02T15:04:05")
					locals = append(locals, from)
					fmt.Fprintf(&input, "%s %s\n", name, from)
				}
			}
			got := runFeeding(t, bin, nil, input.String())
			want := runFeeding(t, python, []string{oracle}, input.String())
			gotLines := strings.Split(strings.TrimSpace(got), "\n")
			wantLines := strings.Split(strings.TrimSpace(want), "\n")
			if len(gotLines) != len(wantLines) {
				t.Fatalf("%s: %d lines from Lyra, %d from the oracle", name, len(gotLines), len(wantLines))
			}
			differed := 0
			for i := range gotLines {
				if gotLines[i] != wantLines[i] {
					t.Errorf("%s from %s: got %q, want %q", name, locals[i], gotLines[i], wantLines[i])
				}
				fields := strings.Fields(wantLines[i])
				if len(fields) == 3 && fields[0] != fields[2] {
					differed++
				}
			}
			// **The test is only testing something if the two rules disagree somewhere**:
			// on a day with no transition in it, `+1 day` and `+24 hours` are the same
			// instant and every implementation agrees.
			if differed == 0 {
				t.Errorf("%s: no case where a calendar day and 24 hours differ, so this proves nothing", name)
			}
		})
	}
}
