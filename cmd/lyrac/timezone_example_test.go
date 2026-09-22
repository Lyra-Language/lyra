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
	}
	at := time.Date(1965, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2035, 1, 1, 0, 0, 0, 0, time.UTC)
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
