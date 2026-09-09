/* A pure-C caller of ffi_fixture.c, printing exactly what the Lyra programs beside it
 * print. It is the oracle the expectations come from.
 *
 * The point is what an ABI test is *for*. If the expected value were read off what the
 * Lyra program happened to produce, the test would assert that Lyra agrees with itself —
 * which it does by construction, and which is not the claim. Compiling the same calls from
 * C and demanding the two outputs match is the claim, and it keeps holding when somebody
 * edits the fixture: both sides move together, and a fixture change that moves only one is
 * the bug the test exists to catch.
 *
 * Same rules as the fixture: C99, no headers beyond these three, no build system.
 */

#include <stdint.h>
#include <stdio.h>

int32_t lyra_fixture_narrow(int8_t a, uint8_t b, int16_t c, uint16_t d);
int64_t lyra_fixture_point_size(void);

struct LyraFixturePoint {
  int32_t x;
  uint8_t tag;
  double weight;
  int64_t id;
};

double lyra_fixture_read_point(const struct LyraFixturePoint *p);
void lyra_fixture_bump_point(struct LyraFixturePoint *p);

/* The union, redeclared here rather than shared through a header for the reason the file
 * header gives: two independent transcriptions that must agree is the claim, and a shared
 * declaration would make them agree by construction. */
typedef struct LyraFixtureUserPayload {
  uint32_t kind;
  uint32_t reserved;
  int64_t code;
  double weight;
} LyraFixtureUserPayload;

typedef union LyraFixtureEvent {
  uint32_t kind;
  LyraFixtureUserPayload user;
  uint8_t padding[64];
} LyraFixtureEvent;

int64_t lyra_fixture_event_size(void);
int64_t lyra_fixture_event_align(void);
int64_t lyra_fixture_event_code_offset(void);
void lyra_fixture_make_event(LyraFixtureEvent *out, int64_t code);
int64_t lyra_fixture_event_code(const LyraFixtureEvent *ev);

int main(void) {
  printf("%d\n", lyra_fixture_narrow(-3, 200, -300, 40000));

  struct LyraFixturePoint p = {10, 3, 0.5, 1000};
  double before = lyra_fixture_read_point(&p);
  lyra_fixture_bump_point(&p);
  /* %g renders these the way Lyra's shortest-round-trip formatter does for the values
   * involved: 1013.5, 1116, 1. A value needing exponent notation would not match, which
   * is a reason to keep the fixture's numbers small rather than to reach for a format. */
  printf("%lld %g %g %d %u %g %lld\n", (long long)lyra_fixture_point_size(), before,
         lyra_fixture_read_point(&p), p.x, (unsigned)p.tag, p.weight, (long long)p.id);
  LyraFixtureEvent ev;
  lyra_fixture_make_event(&ev, 99);
  printf("%lld %lld %lld %u %lld %g %lld\n", (long long)lyra_fixture_event_size(),
         (long long)lyra_fixture_event_align(), (long long)lyra_fixture_event_code_offset(),
         ev.kind, (long long)ev.user.code, ev.user.weight,
         (long long)lyra_fixture_event_code(&ev));

  return 0;
}
