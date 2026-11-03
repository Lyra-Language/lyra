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
- **Compares**:
  `or a; sbc hl, rr; add hl, rr` compares and leaves HL as it was, its flags intact.
- **A and HL**: `Z80ConstrainedCopies` gives each operand that must be in one register its own
  short-lived virtual register. Without it a value pinned to A or HL makes allocation fail.
- **Far slots**: past `(ix+127)` IX is moved for the access, keeping every register and the flags
  (about 170 T-states each way). Large locals work, slowly.
- **Ports**: address space 1. `load`/`store` there is `in`/`out`, the pointer's value the port.
- **Compares**, signed: `LT` and `GE` are not instructions but conditions the printer writes as two
  jumps, testing S and V. A compare with zero is `or` or `bit 7`, with no subtraction.
- **Overflow checks** select to pseudos with the flag as a value, and `Z80ConstrainedCopies` makes the
  operation and the jump directly when that is all the flag does, as nearly always: `or a; adc hl, rr;
  jp pe`. `add hl, rr` sets no overflow flag, so a signed add is the `adc`.
- **Counters**: `Z80NarrowLoops` rewrites a counter ScalarEvolution can bound as an i16.
- **Flags**: nothing the allocator inserts changes them (`ld`, `inc rr`, `dec rr`).

## Testing

`test/run.py` runs `test/*.ll` on Sheliak's Z80 core (`sheliak/build/z80run`), each answer also
held to the host compiler's for the same IR. `test/fuzz.py [COUNT] [SEED]` does the same with
random programs. Both at -O2 and -O0. The helpers come from `runtime/sms/runtime.ll`.

## Not yet

- No assembler or disassembler in `llvm-mc`/`llvm-objdump`, no `jr`, no `ldir`.
- Register allocation can still fail on a very large function (1 in 600 fuzz runs, at -O0, which lyrac does not use).
- `opt` asserts on a constant address above 64 KB (its alignment is more than a 16-bit pointer holds), as
  Genesis IR has; IR emitted for the Z80 has none.
- Registers run out fast: a loop with four live 16-bit values spills, about 250 T-states a pass where
  hand-written code would take perhaps a quarter of that. Frames cost about 100 T-states a call.
- Nothing banks past 48 KB.
