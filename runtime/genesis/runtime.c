/*
 * The Genesis runtime: what a Lyra program compiled for the Genesis needs beneath it —
 * the 68000's exception vectors, the start-up reset runs before `main`, the memory
 * functions LLVM calls, and the two host functions a panic calls, `write` and `exit`.
 *
 * Compiled by lyrac's Genesis build with the M68k clang (-ffreestanding, so a loop here
 * is never turned back into a call to the function it implements) and linked with the
 * program; the cartridge header at 0x100 is written by the linker, which also defines
 * the __data_* and __bss_* symbols. C rather than assembly because none of it needs
 * anything C cannot say, and LLVM's M68k assembler reads little beyond what its own
 * compiler writes.
 *
 * MIT, as Lyra is: it is linked into every game.
 */

typedef unsigned char u8;
typedef unsigned short u16;
typedef unsigned long u32;
typedef unsigned long size_t;

extern u16 __data_load[], __data_start[], __data_end[], __bss_start[], __bss_end[];
extern int main(int argc, char **argv);

void _start(void) __attribute__((noreturn));
void __lyra_fault(void) __attribute__((noreturn));
void __lyra_hblank_entry(void);
void __lyra_vblank_entry(void);
void __lyra_ignore_interrupt(void);

/*
 * The interrupt handlers a program may define — a Lyra function marked `@interrupt(vblank)`
 * or `@interrupt(hblank)` is exported under these names. These weak ones do nothing, so a
 * program without a handler needs none, and one with a handler replaces them at the link.
 */
__attribute__((weak)) void __lyra_interrupt_hblank(void) {}
__attribute__((weak)) void __lyra_interrupt_vblank(void) {}

/*
 * The vectors: the stack at the top of RAM (FFFFFF; 01000000 wraps onto it), reset at
 * _start, the interrupts at their entries, every other exception at __lyra_fault. `used` because nothing refers to it —
 * the linker places the .vectors section at address 0.
 */
__attribute__((section(".vectors"), used))
void (*const __vectors[64])(void) = {
    (void (*)(void))0x01000000,
    _start,
    [2 ... 23] = __lyra_fault,
    /* The spurious interrupt and the unused autovectors are ignored, not faults: a
       level-2 external interrupt (the pad port's TH line) can arrive without a game
       asking for it. */
    [24] = __lyra_ignore_interrupt,
    [25] = __lyra_ignore_interrupt,
    [26] = __lyra_ignore_interrupt,
    [27] = __lyra_ignore_interrupt,
    [28] = __lyra_hblank_entry, /* level 4: the VDP's horizontal interrupt */
    [29] = __lyra_ignore_interrupt,
    [30] = __lyra_vblank_entry, /* level 6: the VDP's vertical interrupt */
    [31] = __lyra_ignore_interrupt,
    [32 ... 63] = __lyra_fault,
};

/*
 * Reset. The 68000 enters with interrupts masked (SR = 2700), so there is nothing to mask;
 * the TMSS handshake unlocks the VDP on consoles that have it, `.data` is copied from ROM
 * and `.bss` zeroed — nothing before this may touch a global — and `main(0, NULL)` runs.
 * A `main` that returns parks the CPU.
 */
void _start(void) {
    volatile u8 *version = (volatile u8 *)0xA10001;
    if (*version & 0x0F) {
        *(volatile u32 *)0xA14000 = 0x53454741; /* "SEGA" */
    }
    for (u16 *from = __data_load, *to = __data_start; to < __data_end;) {
        *to++ = *from++;
    }
    for (u16 *p = __bss_start; p < __bss_end;) {
        *p++ = 0;
    }
    /* Interrupts on for the program: the CPU takes any the VDP raises, and the VDP raises
       none until the program enables one (std.genesis.vdp). `move #$2000, sr` as words —
       LLVM's M68k assembler reads `move.w #…, %sr` as a move to d0. */
    __asm__ volatile(".short 0x46fc, 0x2000");
    main(0, 0);
    for (;;) {
    }
}

/*
 * An interrupt's entry: the scratch registers a Lyra function may clobber (d0, d1, a0, a1)
 * saved, the handler called, and `rte`. The handler is an ordinary function — its own
 * convention preserves d2–d7 and a2–a6 — rather than one compiled with LLVM's M68k
 * interrupt convention, which returns with rte but does not save the scratch registers it
 * or its callees clobber. Naked: the whole body is this, with no frame of its own.
 */
__attribute__((naked)) void __lyra_vblank_entry(void) {
    __asm__ volatile("movem.l %d0-%d1/%a0-%a1, -(%sp)\n\t"
                     "jsr __lyra_interrupt_vblank\n\t"
                     "movem.l (%sp)+, %d0-%d1/%a0-%a1\n\t"
                     "rte");
}

__attribute__((naked)) void __lyra_hblank_entry(void) {
    __asm__ volatile("movem.l %d0-%d1/%a0-%a1, -(%sp)\n\t"
                     "jsr __lyra_interrupt_hblank\n\t"
                     "movem.l (%sp)+, %d0-%d1/%a0-%a1\n\t"
                     "rte");
}

/* An interrupt nothing handles: return to what it interrupted. */
__attribute__((naked)) void __lyra_ignore_interrupt(void) {
    __asm__ volatile("rte");
}

/*
 * A fault — any exception but reset — or a panic: the screen turns red, the display on
 * and everything else as it was, and the CPU stays here. The VDP is written directly so
 * this works whatever state the program left it in.
 */
void __lyra_fault(void) {
    volatile u16 *control = (volatile u16 *)0xC00004;
    volatile u16 *data = (volatile u16 *)0xC00000;
    *control = 0x8004;                      /* mode 5, no H interrupt */
    *control = 0x8144;                      /* display on, no DMA */
    *control = 0x8700;                      /* backdrop: palette 0, colour 0 */
    *(volatile u32 *)0xC00004 = 0xC0000000; /* CRAM write at 0 */
    *data = 0x000E;                         /* red */
    for (;;) {
    }
}

/* A panic's message has nowhere to go; its exit is the red screen. */
long write(int fd, const void *bytes, long count) {
    (void)fd;
    (void)bytes;
    return count;
}

void exit(int code) __attribute__((noreturn));
void exit(int code) {
    (void)code;
    __lyra_fault();
}

/*
 * Memory a long at a time where both ends are even — the 68000 moves a long to and from any
 * even address — and the odd bytes left a byte at a time. A byte at a time throughout, a
 * 768-byte buffer cleared each frame cost a third of a frame (10/15), and the backend moves
 * every aggregate through memory on the 68000 (`sret.go`).
 */
static int even(const void *a, const void *b) {
    return ((((unsigned long)a) | ((unsigned long)b)) & 1) == 0;
}

void *memcpy(void *to, const void *from, size_t n) {
    u8 *d = to;
    const u8 *s = from;
    if (even(d, s)) {
        u32 *dl = (u32 *)d;
        const u32 *sl = (const u32 *)s;
        while (n >= 16) {
            dl[0] = sl[0];
            dl[1] = sl[1];
            dl[2] = sl[2];
            dl[3] = sl[3];
            dl += 4;
            sl += 4;
            n -= 16;
        }
        while (n >= 4) {
            *dl++ = *sl++;
            n -= 4;
        }
        d = (u8 *)dl;
        s = (const u8 *)sl;
    }
    while (n--) {
        *d++ = *s++;
    }
    return to;
}

void *memmove(void *to, const void *from, size_t n) {
    u8 *d = to;
    const u8 *s = from;
    if (d < s) {
        /* Forwards, each long read before the one below it is written. */
        return memcpy(to, from, n);
    } else {
        d += n;
        s += n;
        while (n--) {
            *--d = *--s;
        }
    }
    return to;
}

void *memset(void *to, int value, size_t n) {
    u8 *d = to;
    if (even(d, d)) {
        u32 v = (u8)value;
        v |= v << 8;
        v |= v << 16;
        u32 *dl = (u32 *)d;
        while (n >= 16) {
            dl[0] = v;
            dl[1] = v;
            dl[2] = v;
            dl[3] = v;
            dl += 4;
            n -= 16;
        }
        while (n >= 4) {
            *dl++ = v;
            n -= 4;
        }
        d = (u8 *)dl;
    }
    while (n--) {
        *d++ = (u8)value;
    }
    return to;
}

int memcmp(const void *a, const void *b, size_t n) {
    const u8 *x = a, *y = b;
    for (; n--; x++, y++) {
        if (*x != *y) {
            return *x - *y;
        }
    }
    return 0;
}

/*
 * A 32-bit multiply, which the 68000 does not have: LLVM calls this for an `i32`/`u32`
 * `*`. Three 16×16 `mulu` products; the high one (ah·bh) only reaches bits the 32-bit
 * result drops. The products are inline assembly because LLVM recognises the C spelling
 * of this and turns it back into a 32-bit multiply — a call to this function, forever.
 */
static inline u32 mulu16(u16 a, u16 b) {
    u32 r = a;
    __asm__("mulu %1, %0" : "+d"(r) : "d"(b));
    return r;
}

u32 __mulsi3(u32 a, u32 b) {
    u16 al = (u16)a, ah = (u16)(a >> 16), bl = (u16)b, bh = (u16)(b >> 16);
    if (!(ah | bh)) {
        return mulu16(al, bl);
    }
    u16 cross = (u16)(mulu16(ah, bl) + mulu16(al, bh));
    return mulu16(al, bl) + ((u32)cross << 16);
}

/*
 * A 64-bit multiply's low 64 bits: an `i64`/`u64` `*` — Lyra's default integer, so an
 * index times three is one. compiler-rt's built it from 32-bit C multiplies, each a call
 * to __mulsi3: some 3,700 cycles a multiply, five of them a frame in a song's player
 * (10/15). Here the low words' whole product is four `mulu`s — one where both fit 16 bits,
 * as most of a Genesis game's numbers do — and the high words add theirs past bit 32,
 * skipped where they are 0.
 */
typedef unsigned long long u64;

static u64 mulu32(u32 a, u32 b) {
    u16 a0 = (u16)a, a1 = (u16)(a >> 16), b0 = (u16)b, b1 = (u16)(b >> 16);
    u32 low = mulu16(a0, b0);
    if (!(a1 | b1)) {
        return low;
    }
    u32 p01 = mulu16(a0, b1), p10 = mulu16(a1, b0);
    u32 mid = p01 + p10;
    // A carry out of the middle products is worth 2^48: 2^16 of the high word.
    u32 high = mulu16(a1, b1) + (mid >> 16) + (mid < p01 ? 0x10000 : 0);
    u32 sum = low + (mid << 16);
    high += sum < low;
    return ((u64)high << 32) | sum;
}

u64 __muldi3(u64 a, u64 b) {
    u32 al = (u32)a, ah = (u32)(a >> 32), bl = (u32)b, bh = (u32)(b >> 32);
    u32 high = 0;
    if (ah) {
        high += __mulsi3(ah, bl);
    }
    if (bh) {
        high += __mulsi3(al, bh);
    }
    return mulu32(al, bl) + ((u64)high << 32);
}

/*
 * 32-bit division, which the 68000 has only as 32 ÷ 16 (`divu`: a 16-bit quotient and
 * remainder): an `i32`/`u32` `/` or `%`. compiler-rt's was a shift and subtract of up to
 * 32 steps in C, some 5,000 cycles a division, which a song's player made every frame
 * (10/15). A divisor of 16 bits is two `divu`s, the high half's remainder carried into the
 * low half's; a wider one leaves a quotient of at most 16 bits, found a bit at a time.
 * A divisor of 0 is never asked: Lyra traps before dividing.
 */
static inline u32 divu16(u32 a, u16 b) {
    __asm__("divu %1, %0" : "+d"(a) : "d"(b));
    return a;
}

static u32 udivmod(u32 a, u32 b, u32 *rem) {
    if (b <= 0xFFFF) {
        u32 high = divu16(a >> 16, (u16)b);
        u32 low = divu16((high & 0xFFFF0000) | (a & 0xFFFF), (u16)b);
        *rem = low >> 16;
        return (high << 16) | (low & 0xFFFF);
    }
    // a < b·2^16, so each step's `b << i` is at most a where it is taken: no overflow.
    u32 q = 0;
    for (int i = 15; i >= 0; i--) {
        if ((a >> i) >= b) {
            a -= b << i;
            q |= (u32)1 << i;
        }
    }
    *rem = a;
    return q;
}

u32 __udivsi3(u32 a, u32 b) {
    u32 rem;
    return udivmod(a, b, &rem);
}

u32 __umodsi3(u32 a, u32 b) {
    u32 rem;
    udivmod(a, b, &rem);
    return rem;
}

/* Truncated, as C's and Lyra's `/` and `%`: the remainder takes the dividend's sign. */
long __divsi3(long a, long b) {
    u32 rem;
    u32 q = udivmod(a < 0 ? -(u32)a : (u32)a, b < 0 ? -(u32)b : (u32)b, &rem);
    return (long)((a < 0) != (b < 0) ? -q : q);
}

long __modsi3(long a, long b) {
    u32 rem;
    udivmod(a < 0 ? -(u32)a : (u32)a, b < 0 ? -(u32)b : (u32)b, &rem);
    return (long)(a < 0 ? -rem : rem);
}
