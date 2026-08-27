/* A C library written for nobody, linked into the FFI tests.
 *
 * The suite's other extern tests call libc and libm, deliberately: a library nobody wrote
 * for Lyra is the thing worth proving. What those cannot reach is the rest of the ABI —
 * libc's surface is i32/i64/f64 and pointers, so `float`, the narrow integer widths, a
 * mixed register-class argument list, an argument list long enough to spill, and a struct
 * whose layout both sides must agree on are all untested by them. This file is those
 * cases, and nothing else: it exists to be an ABI counterparty, not a library.
 *
 * Rules for anything added here:
 *   - **C99, no headers beyond <stdint.h>, <stddef.h> and <string.h>.** It is compiled by whatever
 *     clang the harness found, on macOS and in the Debian container, with no package
 *     installed and no build system. Anything needing more belongs in an example.
 *   - **Every function must be checkable from Lyra by its return value alone**, or through
 *     a pointer the caller owns. There is no test harness on this side.
 *   - **Values are chosen so a wrong width is visibly wrong.** Returning 0 on success
 *     proves nothing when a truncated argument also produces 0; each function returns
 *     something that changes if any argument arrived mangled.
 */

#include <stdarg.h>
#include <stddef.h>
#include <stdint.h>
#include <string.h>

/* Narrow integer widths. C's default argument promotions do not apply through a
 * prototype, so each of these is passed at its declared width and the callee is entitled
 * to assume the high bits are extended correctly — signed for i8/i16, zero for u8/u16.
 * Mixing that up is invisible in the IR and wrong only for some values, so the returns
 * below are weighted by width: any single argument arriving mangled changes the sum. */
int32_t lyra_fixture_narrow(int8_t a, uint8_t b, int16_t c, uint16_t d) {
  return (int32_t)a + (int32_t)b * 2 + (int32_t)c * 4 + (int32_t)d * 8;
}

/* `float`, which libm's double-only surface never exercises. A f32 travels in a different
 * register slot from a f64 on both targets, and a Lyra `f32` lowered as a `double` would
 * still *link*. */
float lyra_fixture_f32(float x, float y) { return x * y + 1.5f; }

/* Mixed classes in one list: integers and floats are allocated from separate register
 * banks, so the seventh argument's position depends on how the first six were classified.
 * A width error anywhere shifts everything after it. */
double lyra_fixture_mixed(int32_t a, double b, int64_t c, float d, uint8_t e, double f) {
  return (double)a + b + (double)c + (double)d + (double)e + f;
}

/* Long enough to spill past the register-argument budget on both AArch64 (8) and
 * x86-64 (6 integer). The last arguments arrive on the stack, at offsets the caller and
 * callee must agree on. */
int64_t lyra_fixture_many(int64_t a, int64_t b, int64_t c, int64_t d, int64_t e,
                          int64_t f, int64_t g, int64_t h, int64_t i, int64_t j) {
  return a + b * 2 + c * 3 + d * 4 + e * 5 + f * 6 + g * 7 + h * 8 + i * 9 + j * 10;
}

/* C's `long`, which is the one width that moves between targets (LP64 here, LLP32 on
 * Windows x64). std.ffi's CLong/CULong name it so this crossing is a grep target. */
long lyra_fixture_long(long a, unsigned long b) { return a + (long)(b / 2); }

/* Out-parameters at three widths. Writing through a pointer is how a C function returns
 * more than one value, and it is also the only way a Lyra aggregate reaches this side. */
void lyra_fixture_out(int32_t *i, double *d, uint8_t *b) {
  *i = -12345;
  *d = 2.5;
  *b = 200;
}

/* A struct, by pointer — by value has no C spelling in a Lyra signature (lyra-E063), so
 * this is the whole of the aggregate boundary. Both sides must agree on the *layout*:
 * field order, each field's alignment, and the tail padding. The mixed widths here are
 * chosen so a disagreement shows up as a wrong value rather than as a crash. */
struct LyraFixturePoint {
  int32_t x;
  uint8_t tag;
  double weight;
  int64_t id;
};

double lyra_fixture_read_point(const struct LyraFixturePoint *p) {
  return (double)p->x + (double)p->tag + p->weight + (double)p->id;
}

void lyra_fixture_bump_point(struct LyraFixturePoint *p) {
  p->x += 1;
  p->tag += 1;
  p->weight += 0.5;
  p->id += 100;
}

/* The size and offsets this side computed, so a Lyra test can assert the two agree
 * directly rather than inferring it from a wrong sum. */
int64_t lyra_fixture_point_size(void) { return (int64_t)sizeof(struct LyraFixturePoint); }
int64_t lyra_fixture_point_offset(int32_t field) {
  switch (field) {
  case 0: return (int64_t)offsetof(struct LyraFixturePoint, x);
  case 1: return (int64_t)offsetof(struct LyraFixturePoint, tag);
  case 2: return (int64_t)offsetof(struct LyraFixturePoint, weight);
  case 3: return (int64_t)offsetof(struct LyraFixturePoint, id);
  }
  return -1;
}

/* A byte buffer in and out, which is what a real library's data interface looks like:
 * the caller owns the memory and passes a length, since ownership never crosses. */
int64_t lyra_fixture_sum_bytes(const uint8_t *p, int64_t n) {
  int64_t total = 0;
  for (int64_t i = 0; i < n; i++) total += p[i];
  return total;
}

void lyra_fixture_fill_bytes(uint8_t *p, int64_t n, uint8_t v) {
  for (int64_t i = 0; i < n; i++) p[i] = (uint8_t)(v + (uint8_t)i);
}

/* A NUL-terminated string coming *back*, for std.ffi's cstring_len/decode_utf8 to read.
 * Static storage: ownership does not cross, so the caller must not free it. */
const char *lyra_fixture_greeting(void) { return "héllo, ffi"; }

/* rune is an i32 code point on the Lyra side and a plain int32_t here. */
int32_t lyra_fixture_next_rune(int32_t r) { return r + 1; }

/* Variadics, which are the reason the promotions exist.
 *
 * `va_arg` reads each argument at the type C says it was *promoted* to, so these are the
 * ground truth for the rule: ask for `int` and a caller that passed an unpromoted `i8`
 * hands over a slot that is the wrong size and, on Apple aarch64, in the wrong place
 * entirely — variadic arguments go on the stack while fixed ones go in registers. A caller
 * that gets this wrong still links.
 *
 * They are also what lets the suite test variadics without printf: a format string is a
 * second language, and asserting on rendered text would test the C library's formatter
 * rather than the boundary. */
int64_t lyra_fixture_va_sum(int32_t count, ...) {
  va_list ap;
  va_start(ap, count);
  int64_t total = 0;
  for (int32_t i = 0; i < count; i++) total += (int64_t)va_arg(ap, int);
  va_end(ap);
  return total;
}

/* The promotions, one of each, weighted so any single argument arriving unpromoted or
 * wrongly extended changes the answer. `char`/`short` are read as `int` and `float` as
 * `double` because that is what the caller is required to have passed. */
double lyra_fixture_va_mixed(int32_t count, ...) {
  va_list ap;
  va_start(ap, count);
  double total = 0.0;
  for (int32_t i = 0; i < count; i++) {
    total = total * 10.0 + va_arg(ap, double);
  }
  va_end(ap);
  return total;
}

/* Signedness is the half LLVM cannot recover on its own: an i16 and a u16 are the same
 * `i16` in the IR, so the sign of the extension has to come from the Lyra type. Reading
 * back as `int` is what makes a zero-extended -300 visible as 65236. */
int64_t lyra_fixture_va_signed(int32_t count, ...) {
  va_list ap;
  va_start(ap, count);
  int64_t total = 0;
  for (int32_t i = 0; i < count; i++) total += (int64_t)va_arg(ap, int);
  va_end(ap);
  return total;
}
