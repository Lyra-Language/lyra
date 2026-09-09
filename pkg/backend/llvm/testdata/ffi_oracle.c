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

typedef struct LyraFixtureV2 { float x, y; } LyraFixtureV2;
typedef struct LyraFixtureColor { uint8_t r, g, b, a; } LyraFixtureColor;
typedef struct LyraFixtureRect { float x, y, w, h; } LyraFixtureRect;
typedef struct LyraFixtureBig { int32_t a, b, c, d, e; } LyraFixtureBig;

float lyra_fixture_v2_sum(LyraFixtureV2 v);
LyraFixtureV2 lyra_fixture_v2_make(float x, float y);
int32_t lyra_fixture_color_sum(LyraFixtureColor c);
LyraFixtureColor lyra_fixture_color_make(uint8_t r, uint8_t g, uint8_t b, uint8_t a);
float lyra_fixture_rect_area(LyraFixtureRect r);
int64_t lyra_fixture_big_sum(LyraFixtureBig s);
LyraFixtureBig lyra_fixture_big_make(int32_t n);
float lyra_fixture_agg_mixed(LyraFixtureV2 a, float k, LyraFixtureColor c);

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

  LyraFixtureV2 v = lyra_fixture_v2_make(3.0f, 4.0f);
  LyraFixtureColor col = lyra_fixture_color_make(1, 2, 3, 4);
  LyraFixtureBig big = lyra_fixture_big_make(10);
  LyraFixtureV2 sv = {1.5f, 2.5f};
  LyraFixtureColor sc = {10, 20, 30, 40};
  LyraFixtureRect sr = {0.0f, 0.0f, 3.0f, 4.0f};
  LyraFixtureBig sb = {1, 2, 3, 4, 5};
  LyraFixtureColor mc = {7, 0, 0, 0};
  printf("%g %g %g %d %u %u %u %u %g %lld %d %d %g\n",
         lyra_fixture_v2_sum(sv), v.x, v.y, lyra_fixture_color_sum(sc),
         col.r, col.g, col.b, col.a, lyra_fixture_rect_area(sr),
         (long long)lyra_fixture_big_sum(sb), big.a, big.e,
         lyra_fixture_agg_mixed(sv, 0.5f, mc));

  return 0;
}
