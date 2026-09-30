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

void *memcpy(void *to, const void *from, size_t n) {
    u8 *d = to;
    const u8 *s = from;
    while (n--) {
        *d++ = *s++;
    }
    return to;
}

void *memmove(void *to, const void *from, size_t n) {
    u8 *d = to;
    const u8 *s = from;
    if (d < s) {
        while (n--) {
            *d++ = *s++;
        }
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
    u16 cross = (u16)(mulu16(ah, bl) + mulu16(al, bh));
    return mulu16(al, bl) + ((u32)cross << 16);
}
