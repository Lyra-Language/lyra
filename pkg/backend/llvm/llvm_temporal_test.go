package llvm

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// `std.temporal`'s PlainDate and Duration, after the web's Temporal API, built slice by
// slice against `examples/calendar` (09/18).
//
// Most of this is the calendar's arithmetic, and the cases are the ones a date library
// gets wrong quietly: the century rules, the day clamped by month arithmetic, the ISO week
// that belongs to the neighbouring year, and the two ends of Temporal's range — whose day
// counts are computed here rather than remembered.
func TestExec_PlainDate(t *testing.T) {
	t.Parallel()
	const src = `
module main
import std.temporal.{ PlainDate, plain_date, parse_plain_date, days, weeks, months, years }
let show = pure (m: Maybe<PlainDate>) -> string => match m { Some d => "${d}", None => "none" }
let at = pure (s: string) -> PlainDate => parse_plain_date(s).unwrap_or(PlainDate(0))
let main = () -> void => {
  // The epoch is day 0, and the range is Temporal's: ±10^8 days, one extra at the start.
  println("${i64(at("1970-01-01"))} ${i64(at("-271821-04-19"))} ${i64(at("+275760-09-13"))}");
  println("${show(plain_date(-271821, 4, 18))} ${show(plain_date(275760, 9, 14))}");

  // Leap years: every fourth, except centuries, except every fourth century.
  println("${show(plain_date(2024, 2, 29))} ${show(plain_date(2023, 2, 29))} ${show(plain_date(1900, 2, 29))} ${show(plain_date(2000, 2, 29))}");

  // A date that does not exist is None, not clamped: the constructor rejects.
  println("${show(plain_date(2026, 2, 30))} ${show(plain_date(2026, 13, 1))} ${show(plain_date(2026, 4, 31))}");

  // Weekdays are ISO: 1 is Monday. The epoch was a Thursday, and a date before it needs
  // the floored remainder.
  println("${at("1970-01-01").day_of_week()} ${at("2026-09-18").day_of_week()} ${at("1969-12-28").day_of_week()}");

  // ISO weeks at the year boundaries.
  println("${at("2021-01-03").week_of_year()} ${at("2024-12-30").week_of_year()} ${at("2026-09-18").week_of_year()} ${at("2020-12-31").week_of_year()}");

  // Month arithmetic clamps the day (Temporal's default constrain overflow), and moves
  // the year the right way in both directions.
  let jan31 = at("2026-01-31");
  println("${jan31.add(months(1))} ${jan31.add(months(-13))} ${at("2024-02-29").add(years(1))} ${jan31.subtract(weeks(2))}");

  // until/since are days, and each is the other the other way round.
  println("${at("2026-01-01").until(at("2026-12-31")).days} ${at("2026-12-31").since(at("2026-01-01")).days} ${at("2026-03-01").until(at("2026-02-01")).days}");

  // Day of year and days in the month and year.
  println("${at("2026-12-31").day_of_year()} ${at("2024-12-31").day_of_year()} ${at("2024-02-10").days_in_month()} ${at("2024-06-01").days_in_year()}");

  // with_day clamps; ordering follows the calendar.
  println("${at("2026-02-10").with_day(31)} ${at("2026-02-10").with_day(0)} ${jan31 < jan31.add(days(1))} ${jan31 > jan31.add(days(1))}");
}
`
	want := strings.Join([]string{
		"0 -100000001 100000000",
		"none none",
		"2024-02-29 none none 2000-02-29",
		"none none none",
		"4 5 7",
		"53 1 38 53",
		"2026-02-28 2024-12-31 2025-02-28 2026-01-17",
		"364 364 -28",
		"365 366 29 366",
		"2026-02-28 2026-02-01 true false",
	}, "\n")
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
		t.Errorf("PlainDate =\n%s\nwant\n%s", got, want)
	}
}

// **Every day converts to its fields and back.** The civil algorithms are exact by
// construction, and this is what says the transcription is: a sweep across ±300 years in
// strides that land on every day of the week and every day of the month, each day's
// year-month-day built back into a date that must be the same day. A slip in one of the
// algorithms' magic constants shows up as a month boundary off by one somewhere in the
// sweep, which is exactly where a handful of hand-picked dates would not look.
func TestExec_PlainDateRoundTrips(t *testing.T) {
	t.Parallel()
	const src = `
module main
import std.temporal.{ PlainDate, plain_date }
let main = () -> void => {
  var bad = 0
  var checked = 0
  var n = 0 - 110000
  for n < 110000 {
    let d = PlainDate(n)
    let back = plain_date(d.year(), d.month(), d.day()).unwrap_or(PlainDate(0))
    if i64(back) != n { bad += 1 }
    // Consecutive days are consecutive: the day advances by one or wraps to 1.
    let next = PlainDate(n + 1)
    if next.day() != d.day() + 1 && next.day() != 1 { bad += 1 }
    checked += 1
    n += 7
  }
  println("${checked} ${bad}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "31429 0" {
		t.Errorf("round trip = %q; want \"31429 0\" (checked, bad)", got)
	}
}

// `parse_plain_date` and `show` agree about what a date looks like, including the signed
// six-digit years outside 0–9999, and refuse everything that is not exactly that.
func TestExec_PlainDateParsesWhatItShows(t *testing.T) {
	t.Parallel()
	const src = `
module main
import std.temporal.{ PlainDate, parse_plain_date }
let show = pure (m: Maybe<PlainDate>) -> string => match m { Some d => "${d}", None => "none" }
let main = () -> void => {
  for s in ["2026-09-18", "0000-01-01", "9999-12-31", "-000001-01-01", "+010000-01-01",
            "-271821-04-19", "+275760-09-13"] {
    println("${show(parse_plain_date(s))}");
  }
  // Not ISO 8601, or not a date: all None.
  for s in ["2026-9-18", "26-09-18", "2026/09/18", "2026-09-18T00:00", "-000000-01-01",
            "+2026-09-18", "2026-02-30", "", "abcd-ef-gh"] {
    println("${show(parse_plain_date(s))}");
  }
}
`
	want := strings.Join([]string{
		"2026-09-18", "0000-01-01", "9999-12-31", "-000001-01-01", "+010000-01-01",
		"-271821-04-19", "+275760-09-13",
		"none", "none", "none", "none", "none", "none", "none", "none", "none",
	}, "\n")
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
		t.Errorf("parse/show =\n%s\nwant\n%s", got, want)
	}
}

// **`today()` is the local date**, which is what reading `tm_gmtoff` out of `struct tm`
// is for — and the check is against Go's own clock and zone database, which share nothing
// with the Lyra path but the kernel.
//
// The zones are chosen to make a misread offset visible. In UTC, which is what a CI
// runner usually is, `tm_gmtoff` is 0 and a read from the wrong offset of a zeroed buffer
// could come back 0 too and pass. Kiritimati is UTC+14 and Pago Pago UTC−11, the two ends
// of the offsets in use, so for most of every day the local date in one of them is not the
// UTC date — and the three together disagree with any wrong constant.
//
// Midnight in a zone is a race the test can lose honestly, so a mismatch is retried once
// before it counts.
func TestExec_TodayIsTheLocalDate(t *testing.T) {
	t.Parallel()
	const src = `
module main
import std.temporal.{ today }
let main = () -> void => println("${today()}")
`
	bin := preludeBinary(t, src)
	for _, zone := range []string{"UTC", "Pacific/Kiritimati", "Pacific/Pago_Pago"} {
		loc, err := time.LoadLocation(zone)
		if err != nil {
			t.Skipf("no zoneinfo for %s: %v", zone, err)
		}
		run := func() (string, string) {
			cmd := exec.Command(bin)
			cmd.Env = append(os.Environ(), "TZ="+zone)
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("%s: run: %v", zone, err)
			}
			return strings.TrimSpace(string(out)), time.Now().In(loc).Format("2006-01-02")
		}
		got, want := run()
		if got != want {
			got, want = run()
		}
		if got != want {
			t.Errorf("TZ=%s: today() = %s; Go says the local date is %s", zone, got, want)
		}
	}
}
