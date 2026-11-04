# Lyra's Z80 target

An LLVM backend for the Zilog Z80, for Master System and Game Gear games. `Z80/` is the target,
linked into LLVM's tree; `patches/` tell LLVM the triple, data layout, ELF machine and libcalls.
`../llvm-z80.sh` builds it in `~/Dev/llvm-z80`, apart from the M68k tree Genesis games use.

## How code is shaped

- **Types**: i8 in `GR8`, i16 in `GR16` (HL, DE, BC). Wider integers are LLVM's splitting into
  i16. Multiplies, divides and variable shifts are calls.
- **Arithmetic** works on any pair and takes no pair for a constant, so values can stay where they
  are: `add hl, rr` when the value landed in HL, otherwise a byte at a time through A, whose carry and
  overflow are the whole's. Equality needs Z of the whole, so it has its own compare.
- **Spills**: a function that cannot be running twice at once keeps its pair-sized spills at fixed
  addresses (`ld (nn), hl`, 16 T-states against 38 through IX), proved from the call graph by
  `Z80StaticSpills`: not on a cycle, reached by one way in (`main` or one handler), its address never
  taken. It costs RAM, each such function's slots for good, as nothing shares them yet.
- **Calls**: two 16-bit arguments in HL and DE (or the bytes L and E), the rest on the stack.
  Results: i8 in A; i16s in HL, DE, BC; anything larger through a hidden pointer. BC carries an
  indirect call's target to `__z80_callbc` (`push bc; ret`). Everything but IX is caller-saved.
- **Frames**: SP does not move in a function's body, and IX equals it, or in a frame over 127 bytes
  sits 128 above it to reach 256, so every slot is `(ix+d)`. The prologue uses no general register, so
  arguments stay where they are.
- **Pointers**: a field through a pointer is `ld r, (iy+d)`. `Z80PointerBase` sets IY, not the
  allocator, so it is never spilled: `push rr; pop iy`, 25 T-states, only where IY might not hold that
  pointer already. A call takes it, except the helpers written by hand; the interrupt saves it.
- **A and HL**: `Z80ConstrainedCopies` gives each operand that must be in one register its own
  short-lived virtual register. Without it a value pinned to A or HL makes allocation fail.
- **Far slots**: past `(ix+127)` IX is moved for the access, keeping every register and the flags
  (about 170 T-states each way). Large locals work, slowly.
- **Ports**: address space 1. `load`/`store` there is `in`/`out`, the pointer's value the port.
- **Compares**: through A, leaving every pair as it was. Signed: `LT` and `GE` are not instructions but conditions the printer writes as two
  jumps, testing S and V. A compare with zero is `or` or `bit 7`, with no subtraction.
- **Overflow checks** select to pseudos with the flag as a value, and `Z80ConstrainedCopies` makes the
  operation and the jump directly when that is all the flag does, as nearly always: `or a; adc hl, rr;
  jp pe`. `add hl, rr` sets no overflow flag, so a signed add is the `adc`.
- **Counters**: `Z80NarrowLoops` rewrites a counter ScalarEvolution can bound as an i16.
- **Flags**: nothing the allocator inserts changes them (`ld`, `inc rr`, `dec rr`).

## Hand-written helpers

`pkg/rom/z80.go` holds `__mulhi3`/`__z80_umul32` and `__udivhi3`/`__umodhi3` as bytes, since `llc` cannot
assemble and the compiled loops took several times as long: about 900 T-states to multiply (half for
a multiplier under 256) and 1,200 to divide. `test/run.py` carries a copy to link with.

## Banks

When everything fits the 48 KB in view, a `@banked` table stays with the rest and its bank is 2, the
window's own, so a small game costs only the selecting call.

Not the target's business but the backend's and linker's: `selectBanks` (`pkg/backend/llvm/banks.go`)
makes each use of a `@banked` table go through `__z80_bank`, and `pkg/rom/z80.go` gives the tables their
banks, from 2 up, largest first. The bank's number is the "address" of `__lyra_bank.<table>`. The
interrupt saves and restores the current bank, from a copy in RAM at DFF8 since the register reads
as nothing.

## Finding where a frame goes

```bash
LYRA_Z80_MAP=game.map lyrac build -o game.sms game.lyra
smscheck game.sms --frames 200 --ppm out.ppm --work on --profile game.prof
```

`game.map` is each section's `address size name`, and `game.prof` each address's T-states. llc's
`-show-mc-encoding` gives each instruction's bytes, which line the two up with the assembly; its
comments mark spills that still go through a frame. Count T-states this way rather than instructions:
twice a count of instructions promised a win that a profile showed was in cold code.

## Testing

`test/run.py` runs `test/*.ll` on Sheliak's Z80 core (`sheliak/build/z80run`), each answer also
held to the host compiler's for the same IR. `test/fuzz.py [COUNT] [SEED]` does the same with
random programs. Both at -O2 and -O0. The helpers come from `runtime/sms/runtime.ll`.

## Not yet

- No assembler or disassembler in `llvm-mc`/`llvm-objdump`, no `jr`, no `ldir`.
- Register allocation can still fail on a very large function (1 in 600 fuzz runs, at -O0, which lyrac does not use).
- `opt` asserts on a constant address above 64 KB (its alignment is more than a 16-bit pointer holds), as
  Genesis IR has; IR emitted for the Z80 has none.
- Registers still run out: a loop with four live 16-bit values spills. Frames cost about 100
  T-states a call, where a function still has one.
- Fixed spill slots are not shared between functions that are never active together, which would
  give the RAM back.
