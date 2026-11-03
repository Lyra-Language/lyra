# Lyra's Z80 target

An LLVM backend for the Zilog Z80, for Master System and Game Gear games. `Z80/` is the target,
linked into LLVM's tree; `patches/` tell LLVM the triple, data layout, ELF machine and libcalls.
`../llvm-z80.sh` builds it in `~/Dev/llvm-z80`, apart from the M68k tree Genesis games use.

## How code is shaped

- **Types**: i8 in `GR8`, i16 in `GR16` (HL, DE, BC). Wider integers are LLVM's splitting into
  i16 over `add/adc/sbc hl`. Multiplies, divides and variable shifts are calls.
- **Calls**: two 16-bit arguments in HL and DE (or the bytes L and E), the rest on the stack.
  Results: i8 in A; i16s in HL, DE, BC; anything larger through a hidden pointer. BC carries an
  indirect call's target to `__z80_callbc` (`push bc; ret`). Everything but IX is caller-saved.
- **Frames**: SP does not move in a function's body, and IX equals it, so every slot is
  `(ix+d)`. The prologue uses no general register, so arguments stay where they are.
- **Compares**: the Z80 tests Z and C only, so signed compares flip both sign bits.
  `or a; sbc hl, rr; add hl, rr` compares and leaves HL as it was, Z and C intact.
- **A and HL**: `Z80ConstrainedCopies` gives each operand that must be in one register its own
  short-lived virtual register. Without it a value pinned to A or HL makes allocation fail.
- **Flags**: nothing the allocator inserts changes them (`ld`, `inc rr`, `dec rr`).

## Testing

`test/run.py` runs `test/*.ll` on Sheliak's Z80 core (`sheliak/build/z80run`), each answer also
held to the host compiler's for the same IR. `test/fuzz.py [COUNT] [SEED]` does the same with
random programs. Both at -O2 and -O0. `test/rt.ll` stands in for the runtime's helpers.

## Not yet

- Slots beyond (ix+127) are a fatal error: large local arrays and structs will hit it.
- No assembler or disassembler in `llvm-mc`/`llvm-objdump`, no `jr`, no `ldir`, no port I/O.
- Code quality is a first pass: no peepholes, and signed compares and overflow checks are slow.
- `lyrac` does not target it: no `sms` target, no linker for these objects, no runtime.
- Register allocation can still fail on a very large function (1 in 600 fuzz runs, seed 1199 -O0).
- `opt -O2` asserts in `computeKnownBitsMul` on IR lyrac emitted for the Genesis (not reduced).
