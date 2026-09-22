package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// **`std.temporal`'s zone rules, read from the system's IANA database** (09/22) — the first
// slice of the zone work: a zone's transitions, and the offset, abbreviation and
// daylight-ness in effect at an instant.
//
// The oracle is Go's `time`, which reads the same files by a different implementation, so
// this is the checksum test's arrangement rather than a renderer's: the expectations are
// not what the Lyra code happened to print. It is worth that much care because a zone
// database is a pile of special cases, and every one of them is somebody's ordinary
// Tuesday — a half-hour offset, a 45-minute one, a country that abolished daylight saving,
// a southern-hemisphere zone whose summer is January.
//
// **The instants are the interesting ones.** A table of round numbers would agree with any
// implementation that reads the first record and stops, so most of these are found by
// asking Go where this zone's transitions actually are and sampling a second either side
// of each: the instant a rule takes effect, and the last instant of the rule before it.
func TestExample_TimeZoneMatchesGo(t *testing.T) {
	root := repoRoot(t)
	t.Setenv("LYRA_STD", root)
	if _, err := os.Stat("/usr/share/zoneinfo"); err != nil {
		t.Skip("no /usr/share/zoneinfo on this machine")
	}

	probe := filepath.Join(t.TempDir(), "probe.lyra")
	if err := os.WriteFile(probe, []byte(`module main
import std.collections.{ parse_args }
import std.temporal.{ load_time_zone, offset_at, abbreviation_at, is_dst_at }

let main = () -> u8 => {
  let args = parse_args([])
  let Some(zone) = load_time_zone(args.positional[0]) else {
    println("no such zone")
    return 1
  }
  for i in 1..<args.positional.len() {
    let at = match args.positional[i].parse_i64() { Some(v) => v, None => 0 }
    println("${zone.offset_at(at)} ${zone.abbreviation_at(at)} ${zone.is_dst_at(at)}")
  }
  0
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "probe")
	if _, stderr, code := captureRun(t, "build", "-o", bin, probe); code != 0 {
		t.Fatalf("building the probe exited %d\nstderr: %s", code, stderr)
	}

	zones := []string{
		"UTC",
		"America/New_York",    // the ordinary case, and the one every bug is found in first
		"Europe/Berlin",       // transitions on a different weekend from America's
		"Asia/Kolkata",        // +05:30, and no daylight saving at all
		"Asia/Kathmandu",      // +05:45, the offset that catches minute arithmetic
		"Australia/Lord_Howe", // a *half-hour* daylight shift, which is nobody's default
		"Pacific/Chatham",     // +12:45 and +13:45
		"America/Sao_Paulo",   // abolished daylight saving in 2019: transitions then nothing
		"Africa/Nairobi",      // a zone whose whole history is two records
	}
	for _, name := range zones {
		t.Run(name, func(t *testing.T) {
			loc, err := time.LoadLocation(name)
			if err != nil {
				t.Skipf("Go cannot load %s either: %v", name, err)
			}
			instants := sampleInstants(loc)
			// `--` before the instants: several are negative, and without it they are
			// read as flags — which is how `parse_args` came to grow the separator.
			args := []string{name, "--"}
			for _, at := range instants {
				args = append(args, strconv.FormatInt(at, 10))
			}
			out, err := exec.Command(bin, args...).Output()
			if err != nil {
				t.Fatalf("probing %s failed: %v", name, err)
			}
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			if len(lines) != len(instants) {
				t.Fatalf("%s: got %d lines for %d instants:\n%s", name, len(lines), len(instants), out)
			}
			for i, at := range instants {
				abbrev, offset := time.Unix(at, 0).In(loc).Zone()
				isDST := time.Unix(at, 0).In(loc).IsDST()
				want := fmt.Sprintf("%d %s %t", offset, abbrev, isDST)
				if lines[i] != want {
					t.Errorf("%s at %d (%s): got %q, want %q",
						name, at, time.Unix(at, 0).In(loc).Format(time.RFC3339), lines[i], want)
				}
			}
		})
	}
}

// sampleInstants is a fixed spread plus, for each of the zone's own transitions, the
// second it takes effect and the second before it — which is where an off-by-one in the
// search shows up and nowhere else.
//
// **Found by a day-by-day scan, not by bisection.** The first version bisected between a
// date and two years later, which sounds reasonable and is wrong: a zone is usually in the
// same season two years on, so the offsets matched, the search concluded "no transitions"
// and every zone was compared at five round instants. The test passed, and an off-by-one
// planted in the binary search still passed it. A predicate that is not monotone cannot be
// bisected — so this walks days, which is 25k lookups per zone and costs milliseconds.
func sampleInstants(loc *time.Location) []int64 {
	out := []int64{
		-2208988800, // 1900, before most zones' records begin
		0,           // the epoch
		1000000000,  // 2001
		1773000000,  // 2026
		2000000000,  // 2033, near the end of many tables
		2500000000,  // 2049, past every table and into the footer's rule
		4102444800,  // 2100
	}
	// **To 2100, which is well past where the table stops.** A zone file lists transitions
	// to about 2037 and then leaves a POSIX rule for the rest; walking only to 2035 tested
	// the table and nothing else, and an implementation that ignored the footer — as this
	// one did until 09/22 — passed. Go applies the footer too, so it stays the oracle.
	at := time.Date(1965, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)
	_, previous := at.In(loc).Zone()
	for at.Before(end) {
		next := at.AddDate(0, 0, 1)
		_, offset := next.In(loc).Zone()
		if offset != previous {
			moment := transitionBetween(loc, at, next)
			out = append(out, moment-1, moment)
			previous = offset
		}
		at = next
	}
	return out
}

// transitionBetween narrows a day that contains a change to the second it happened. The
// predicate *is* monotone inside one day — a zone changes at most once in it — which is
// what makes the bisection sound here and unsound over two years.
func transitionBetween(loc *time.Location, lo, hi time.Time) int64 {
	_, start := lo.In(loc).Zone()
	for hi.Sub(lo) > time.Second {
		mid := lo.Add(hi.Sub(lo) / 2)
		if _, off := mid.In(loc).Zone(); off == start {
			lo = mid
		} else {
			hi = mid
		}
	}
	return hi.Unix()
}

// **`ZonedDateTime`'s rendering, against Go's** (09/22) — the second slice: an instant seen
// from a zone, written as Temporal writes one,
// `2026-09-22T14:30:00-04:00[America/New_York]`.
//
// Slice one's test compared offsets, which is the zone table. This compares the *local
// date and clock* those offsets produce, which is the arithmetic on top: a moment that is
// tomorrow in Greenwich and still today here, an offset of 45 minutes, and — before a zone
// was standardized — an offset with seconds in it, which a formatter assuming whole
// minutes rounds away in silence.
func TestExample_ZonedDateTimeMatchesGo(t *testing.T) {
	root := repoRoot(t)
	t.Setenv("LYRA_STD", root)
	if _, err := os.Stat("/usr/share/zoneinfo"); err != nil {
		t.Skip("no /usr/share/zoneinfo on this machine")
	}
	probe := filepath.Join(t.TempDir(), "probe.lyra")
	if err := os.WriteFile(probe, []byte(`module main
import std.collections.{ parse_args }
import std.temporal.{ load_time_zone, instant, to_zoned_date_time }

let main = () -> u8 => {
  let args = parse_args([])
  let Some(zone) = load_time_zone(args.positional[0]) else {
    println("no such zone")
    return 1
  }
  for i in 1..<args.positional.len() {
    let at = match args.positional[i].parse_i64() { Some(v) => v, None => 0 }
    let Some(moment) = instant(at) else {
      println("out of range")
      continue
    }
    println("${moment.to_zoned_date_time(zone)}")
  }
  0
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "probe")
	if _, stderr, code := captureRun(t, "build", "-o", bin, probe); code != 0 {
		t.Fatalf("building the probe exited %d\nstderr: %s", code, stderr)
	}

	for _, name := range []string{
		"UTC",
		"America/New_York",
		"Asia/Kathmandu",     // +05:45
		"Pacific/Chatham",    // +12:45 and +13:45
		"Asia/Kolkata",       // an offset with *seconds* before 1906
		"Pacific/Kiritimati", // +14:00, the far side of the date line
	} {
		t.Run(name, func(t *testing.T) {
			loc, err := time.LoadLocation(name)
			if err != nil {
				t.Skipf("Go cannot load %s either: %v", name, err)
			}
			instants := sampleInstants(loc)
			args := append([]string{name, "--"}, formatInts(instants)...)
			out, err := exec.Command(bin, args...).Output()
			if err != nil {
				t.Fatalf("probing %s failed: %v", name, err)
			}
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			if len(lines) != len(instants) {
				t.Fatalf("%s: got %d lines for %d instants:\n%s", name, len(lines), len(instants), out)
			}
			for i, at := range instants {
				local := time.Unix(at, 0).In(loc)
				_, offset := local.Zone()
				want := local.Format("2006-01-02T15:04:05") + offsetString(offset) + "[" + name + "]"
				if lines[i] != want {
					t.Errorf("%s at %d: got %q, want %q", name, at, lines[i], want)
				}
			}
		})
	}
}

func formatInts(values []int64) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		out = append(out, strconv.FormatInt(v, 10))
	}
	return out
}

// offsetString writes an offset as ISO 8601 does, including the seconds a pre-standard
// zone has and omitting them when it does not — the same rule the Lyra side states, spelled
// out here rather than borrowed, so the two are not one implementation agreeing with
// itself.
func offsetString(seconds int) string {
	if seconds == 0 {
		return "Z"
	}
	sign := "+"
	if seconds < 0 {
		sign = "-"
		seconds = -seconds
	}
	out := fmt.Sprintf("%s%02d:%02d", sign, seconds/3600, (seconds%3600)/60)
	if seconds%60 != 0 {
		out += fmt.Sprintf(":%02d", seconds%60)
	}
	return out
}

// **What `Show` writes, `parse_zoned_date_time` reads back** (09/22) — the round trip the
// other four temporal types already had and this one did not, at the type you would most
// want to store in a file.
//
// The property is checked over every instant the zone tests sample, ±1s around every
// transition to 2100: render it, read it back, and demand the same moment. **The repeated
// hour is where this earns its keep** — `01:30-04:00` and `01:30-05:00` are the same clock
// reading and two different instants, and a round trip that lost the offset would collapse
// them onto one without ever failing loudly.
func TestExample_ZonedDateTimeRoundTrips(t *testing.T) {
	root := repoRoot(t)
	t.Setenv("LYRA_STD", root)
	if _, err := os.Stat("/usr/share/zoneinfo"); err != nil {
		t.Skip("no /usr/share/zoneinfo on this machine")
	}
	probe := filepath.Join(t.TempDir(), "probe.lyra")
	if err := os.WriteFile(probe, []byte(`module main
import std.collections.{ parse_args }
import std.temporal.{
  load_time_zone, instant, to_zoned_date_time, parse_zoned_date_time, to_instant,
  epoch_second,
}

/// Renders each instant in the zone, reads the rendering back, and prints the moment that
/// came out — so a round trip that loses information shows up as a different number.
let main = () -> u8 => {
  let args = parse_args([])
  let Some(zone) = load_time_zone(args.positional[0]) else {
    println("no such zone")
    return 1
  }
  for i in 1..<args.positional.len() {
    let at = match args.positional[i].parse_i64() { Some(v) => v, None => 0 }
    let Some(moment) = instant(at) else {
      println("out of range")
      continue
    }
    let written = "${moment.to_zoned_date_time(zone)}"
    match parse_zoned_date_time(written) {
      Some(read_back) => println("${read_back.to_instant().epoch_second()}"),
      None => println("unreadable: ${written}"),
    }
  }
  0
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "probe")
	if _, stderr, code := captureRun(t, "build", "-o", bin, probe); code != 0 {
		t.Fatalf("building the probe exited %d\nstderr: %s", code, stderr)
	}

	for _, name := range []string{
		"UTC", "America/New_York", "Europe/Berlin", "Asia/Kathmandu",
		"Australia/Lord_Howe", "Pacific/Chatham",
	} {
		t.Run(name, func(t *testing.T) {
			loc, err := time.LoadLocation(name)
			if err != nil {
				t.Skipf("Go cannot load %s either: %v", name, err)
			}
			instants := sampleInstants(loc)
			args := append([]string{name, "--"}, formatInts(instants)...)
			out, err := exec.Command(bin, args...).Output()
			if err != nil {
				t.Fatalf("probing %s failed: %v", name, err)
			}
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			if len(lines) != len(instants) {
				t.Fatalf("%s: got %d lines for %d instants", name, len(lines), len(instants))
			}
			ambiguous := 0
			for i, at := range instants {
				if lines[i] != strconv.FormatInt(at, 10) {
					t.Errorf("%s: %d came back as %q", name, at, lines[i])
				}
				// Count the readings a zone shows twice, to know this test met any.
				if _, offset := time.Unix(at, 0).In(loc).Zone(); offset != 0 {
					if _, earlier := time.Unix(at-3600, 0).In(loc).Zone(); earlier != offset {
						ambiguous++
					}
				}
			}
			if name != "UTC" && ambiguous == 0 {
				t.Errorf("%s: no instant near a change of offset, so the round trip was never hard", name)
			}
		})
	}
}

// **The forms the round trip never produces.** `Show` always writes an offset, so the
// property test above cannot reach an input without one — removing the parser's `T` anchor,
// which only matters for that case, passed it untouched. These are the edges, written out:
// the offset omitted, the offset wrong, the zone unknown, the brackets missing.
//
// The assertions are **relationships rather than instants** wherever a zone decides them —
// that the two readings of a repeated hour are an hour apart, and that the offsetless form
// agrees with the earlier of them — so this keeps holding when a country next changes its
// rules, which is the event the whole database exists to record.
func TestExample_ParseZonedDateTimeEdges(t *testing.T) {
	root := repoRoot(t)
	t.Setenv("LYRA_STD", root)
	if _, err := os.Stat("/usr/share/zoneinfo"); err != nil {
		t.Skip("no /usr/share/zoneinfo on this machine")
	}
	probe := filepath.Join(t.TempDir(), "probe.lyra")
	if err := os.WriteFile(probe, []byte(`module main
import std.io.{ read_stdin }
import std.temporal.{ parse_zoned_date_time, parse_instant, to_instant, epoch_second }

/// One string per line in, its instant (or "none") out — read both ways, since a string
/// with an offset and no zone is an instant and not a zoned date-time.
let main = () -> u8 => {
  for line in read_stdin().lines() {
    let text = line.trim()
    if text.len() == 0 { continue }
    match parse_zoned_date_time(text) {
      Some(z) => println("${z.to_instant().epoch_second()}"),
      None => match parse_instant(text) {
        Some(i) => println("instant ${i.epoch_second()}"),
        None => println("none"),
      },
    }
  }
  0
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "probe")
	if _, stderr, code := captureRun(t, "build", "-o", bin, probe); code != 0 {
		t.Fatalf("building the probe exited %d\nstderr: %s", code, stderr)
	}

	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("Go cannot load America/New_York")
	}
	fall, ok := fallBackAfter(loc, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if !ok {
		t.Skip("New York no longer puts its clocks back, and half of this is about that")
	}
	// An hour before the clocks go back, written three ways.
	repeated := time.Unix(fall-1800, 0).In(loc).Format("2006-01-02T15:04:05")
	_, before := time.Unix(fall-1, 0).In(loc).Zone()
	_, after := time.Unix(fall, 0).In(loc).Zone()
	inputs := []string{
		repeated + offsetString(before) + "[America/New_York]", // the first reading
		repeated + offsetString(after) + "[America/New_York]",  // the second, an hour later
		repeated + "[America/New_York]",                        // no offset: the rule decides
		repeated + "+09:00[America/New_York]",                  // an offset the zone never had
		repeated + offsetString(before) + "[Nowhere/Nope]",     // a zone that does not exist
		repeated + offsetString(before),                        // no brackets: an instant
		"2026-09-22T18:32:29Z",                                 // Instant's own rendering
		"not a time at all",
	}
	out := runFeeding(t, bin, nil, strings.Join(inputs, "\n")+"\n")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != len(inputs) {
		t.Fatalf("got %d lines for %d inputs:\n%s", len(lines), len(inputs), out)
	}
	first, err := strconv.ParseInt(lines[0], 10, 64)
	if err != nil {
		t.Fatalf("the first reading did not parse: %q", lines[0])
	}
	second, err := strconv.ParseInt(lines[1], 10, 64)
	if err != nil {
		t.Fatalf("the second reading did not parse: %q", lines[1])
	}
	if second-first != int64(before-after) {
		t.Errorf("the two readings are %d seconds apart, want %d", second-first, before-after)
	}
	if lines[2] != lines[0] {
		t.Errorf("without an offset the rule should take the first reading: %q vs %q", lines[2], lines[0])
	}
	if lines[3] != "none" {
		t.Errorf("an offset the zone never used should be refused, got %q", lines[3])
	}
	if lines[4] != "none" {
		t.Errorf("an unknown zone should be refused, got %q", lines[4])
	}
	if lines[5] != "instant "+lines[0] {
		t.Errorf("without brackets it is an instant, not a zoned date-time: %q", lines[5])
	}
	if lines[6] != "instant 1790101949" {
		t.Errorf("a UTC instant should read back exactly, got %q", lines[6])
	}
	if lines[7] != "none" {
		t.Errorf("nonsense should be refused, got %q", lines[7])
	}
}
