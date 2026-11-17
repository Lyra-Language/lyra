#!/usr/bin/env bash
#
# Build LLVM with its (experimental) M68k target: what `lyrac build` uses for a program
# whose `lyra.toml` names `target = "genesis"` (cmd/lyrac/genesis.go) — opt and llc for
# the program's IR, clang for the runtime (runtime/genesis) and compiler-rt's helpers.
# Apache-2.0 with the LLVM exception, so nothing here limits a game's licence.
#
#   tools/llvm-m68k.sh            # clone (once), configure, build   -> $LYRA_M68K_LLVM/build/bin
#
# LYRA_M68K_LLVM defaults to ~/Dev/llvm-m68k (outside the repository: the build is several
# GB), which is also where lyrac looks.
# **Pinned to a `main` commit, not a release** (09/30), and to that commit exactly:
#
#   - 22.1.8 compiled a loop whose counter copy sat between the compare and the branch,
#     and on the 68000 a `move` sets the condition codes — the branch tested the copy and
#     the loop never ended (LLVM #152816). e7dd336e0f78, pinned here, is the fix ("Prevent
#     COPY instruction from killing live condition flags"); no 22.x release has it.
#   - Later `main` (16c337766166, "Implement CLR instruction", 08/25) stores a zero as
#     `clr.w (a0)`, and a 68000's CLR *reads* its destination before writing it — so a
#     volatile zero to the VDP's control port also reads the port, which resets the VDP's
#     half-written command, and the Genesis shows nothing. A volatile store must not
#     become a read-modify-write; until upstream stops selecting CLR for one on a 68000,
#     the pin stays before it (so the later MOVEM/PHI and MOVX fixes wait too).
#
# **Patched, by `tools/llvm-m68k-patches/*.patch`**, applied in order on top of the pin:
#
#   - 0001: a load or store at a stack object plus a variable index (`xs[i]` on a local
#     array) was selected as the object alone — the index dropped, every `xs[i]` read
#     `xs[0]`. `matchAddressBase` set an index beside a frame-index base, which
#     `hasIndexReg()` does not see; the patch refuses one there (09/30; found by Vega's
#     animations, whose step table is copied to the stack). Not yet reported upstream.
#   - 0002: multiply-with-overflow (every checked `*` Lyra emits) was custom-lowered on
#     every CPU: an i32 one to `muls.l`, a 68020 instruction the 68000 faults on, and an
#     i8/i16 one to a word multiply whose V flag never sets, so it never trapped. On a
#     68000 or 68010 they now take LLVM's generic expansion (a high-half multiply,
#     compared — `__muldi3` for i32), and the branch and select lowering no longer
#     re-lower an expanded one through the flags (10/01; found by the hero's game
#     freezing — panicking — at a rock). Not yet reported upstream.
#   - 0003: the 16→32-bit sign- and zero-extend pseudos let their destination be an
#     address register, and expanded in place with `ext`/`and`, which take data
#     registers only: `and.l #65535, %aN` is no 68000 instruction, and llc's object
#     writer encodes it as another — the hero's position came out as garbage once his
#     game grew stars (10/01). They are data-register-only now, as the 8-bit ones were.
#   - 0004: a struct-return function (every Lyra function returning an aggregate on the
#     68000) popped four bytes for its hidden pointer on return, as if the caller had pushed
#     it — but `fastcc`, which the optimizer gives internal functions, passes it in A0, and
#     the caller made nothing up. The stack pointer crept a word a call into the caller's
#     locals until one read garbage (10/16; found by the music player crashing once LLVM
#     inlined less). Only a pointer passed on the stack is popped now. Reported as
#     llvm/llvm-project#232869.
#   - 0005: an i32 multiply on the 68000 and 68010 was always a call to __mulsi3, though
#     the chip multiplies words (`muls.w`, `mulu.w`: 16 x 16 -> 32 bits). It is lowered as
#     the cheapest of one word multiply (both operands a word, signed or unsigned), shifts
#     and adds (a constant of at most three set bits), two word multiplies (one operand an
#     unsigned word — an element's size in `xs[i]`), and __mulsi3 for the rest
#     (M68kTargetLowering::LowerMUL, the SMUL16/UMUL16 nodes; 11/11, found profiling the
#     hero's frame). TestGenesis_MultipliesOfEveryShapeAgreeWithTheHost checks every shape.
#   - 0006: a branch further than 16 bits has no form on the 68000 and 68010 (the 32-bit
#     displacement is the 68020's), and the assembler backend stopped at one with "Cannot
#     relax the instruction, value does not fit" — any function past 32 KB could (the
#     hero's `main`, 11/10). It now relaxes `bra.w` into `jmp` to the absolute address and
#     `b<cc>.w` into the opposite condition over that `jmp` (BRAfar, B<cc>far; 11/12).
#     TestGenesis_ABranchFurtherThanAWordReaches runs one; MC/M68k/Relaxations/branch-far.s
#     encodes them. Not yet reported upstream.
#
#   0007–0011 make the 68000's code cheaper (11/16, measured on the hero's vertical
#   blank: 59 lines a frame to 51, its worst 194 to 171, the ROM 9 KB smaller), none
#   changing what a program does. Not yet reported upstream.
#   - 0007: a spill or reload of one register was MOVEM, which keeps the flags — so a
#     spill may sit between a compare and its branch — at 4 to 8 cycles more than MOVE.
#     Where CCR is dead (asked of the block's liveness) it is a MOVE (ExpandMOVEM).
#   - 0008: an overflow check whose add folded a load came to the branch as the
#     overflow bit masked with 1 (promoting the i1 leaves it), which LowerBRCOND did not
#     see through: Scc, AND, CMP and a branch where one Bvs does.
#   - 0009: ADDQ and SUBQ were never made — every `x += 1` an ADDI, sixteen cycles
#     where eight do. Defined (MxQuick, no patterns) and made after register allocation
#     of an ADD or SUB of 1 to 8 (M68kInstrInfo::ExpandQuick, from M68kExpandPseudo);
#     a negative constant as the other only where CCR is dead (its carry differs), and
#     ADDA/SUBA of a larger one LEA (8 cycles to 16). MC/M68k/Arith/Classes/MxQuick.s.
#   - 0010: every compare was a SUB (x86's choice, so CSE shares a difference also
#     computed; x86 turns an unused one back into CMP, this target never did), so it
#     copied and destroyed a register: MOVE and SUBI where CMPI does. A CMP now, a SUB
#     only where the same SUB exists; and a 16-bit compare with an immediate is no
#     longer widened to 32 bits (x86 avoids 16-bit immediates; the 68000 prefers them).
#   - 0011: a COPY left between a compare and its branch (PHI elimination's) was
#     wrapped in MOVE from SR and MOVE to CCR, 18 cycles. MOVEA into an address register
#     sets no flags (MOVA32ar, without MOV32ar's CCR def), and EXG of two data registers
#     sets none, where the source dies (EXG32dd; MC/M68k/Data/Classes/MxExg.s).
#   - 0012: the 46 CodeGen/M68k tests those change, regenerated by
#     update_llc_test_checks.py; the five that failed before 0006 still do.
#   - 0013: TST was never made: a compare with zero was CMPI #0, eight cycles on a register
#     where TST's are four, the flags the same. Defined for a data register, (An),
#     (d16,An) and (d8,An,Xn) (not PC-relative, the 68020's), selected for (CMP 0, x);
#     455 in the hero. MC/M68k/Arith/Classes/MxTst.s.
#   - 0014: a copy between a compare and its branch whose source lives on (so no EXG) goes
#     ahead of the instruction that set the flags, where nothing between touches it and
#     that instruction does not read the flags: its MOVE's flags are the setter's to
#     replace. 90 flag saves in the hero to 37 (the rest the setter reads the copy's
#     destination).
#   - 0015: the seven CodeGen/M68k tests 0013–0014 change, regenerated.
#   - 0016: a branch on a boolean got it as (sign_extend_inreg x, i1) — the target's
#     booleans are 0 or all ones — which became AND #1, NEG and a test. It is nonzero
#     exactly where x's bit 0 is, so LowerBRCOND branches on that bit, and on x itself where
#     x is known 0 or 1 (a bool loaded from memory). Half the hero's NEG.Bs.
#   With them the hero's vertical blank is 49 lines a frame (59 before 0007), its worst 163.
#   - 0017: a spill 0007 makes a MOVE keeps its memory operand (the stack slot), which it
#     had dropped: later passes saw an unknown access, and `-print`/asm lost its "Folded
#     Spill" note. The hero's code is the same.
#   - 0018: post-increment, (An)+, which the target never made — `vdp.send_colors`' 64
#     colours were `move.w (0,%a0,%d4), $c00000; addq; cmpi.l #128; bne`, and a copy
#     stepped each address through a data register. Indexed loads and stores are legal
#     (getPostIndexedAddressParts: a whole byte, word or long, the address past it), and
#     MOVEs naming the address register's new value as a def tied to it (M68kInstrData.td,
#     `*_PI`) are selected by hand (selectPostIncrement), a store of such a load's value
#     reading it itself: `move.w (%a1)+, (%a0)+`. An M68k TTI (BasicTTIImpl's answers,
#     what the target had by default) asks loop strength reduction for post-increment
#     where every stream a loop steps through can be one: asked where a struct's fields
#     step by the struct, it made an address register of each, and the raster handler's
#     extra saves made the first colour late. And an ADD of -n with flags only Z, N or V
#     branches read is SUBQ, so the count down is `subq.l #1`. The hero's vertical blank
#     handler 53.7 → 49.6 lines a frame on average, its chip writes the same.
#   - 0019: a constant added to a pointer in an address register is LEA, where the selector
#     made every ADD into a data register and the pointer went out and back (`move.l %a0,
#     %d2; addq.l #6, %d2; movea.l %d2, %a0`, 89 times in the hero). M68kAddressAdds, before
#     register allocation: an ADD or SUB of a 16-bit constant, its flags unread, to a copy
#     of an address register becomes `lea (d16,An), Am`, and the sum stays there where
#     everything reading it takes an address register (a store, a copy, a phi). Copies from
#     address to data registers 163 → 79, the hero's code 206 bytes smaller, its vertical
#     blank 49.6 → 49.0 lines.
#   - 0020: a byte register spilled or reloaded where the flags are dead is a byte MOVE,
#     not MOVEM.W (which has no byte form): 0007 made MOVEs only of word and long
#     registers, and 816 of the hero's spills and reloads were bytes. The MOVE reaches the
#     word slot's low byte, one past it, so it agrees with a MOVEM.W of the same slot where
#     the flags are live; byte spill pseudos (MOVM8pm_P, MOVM8mp_P) give the register its
#     class, which the verifier refused in `vdp.dma_to_vram`. The vertical blank 49.0 →
#     48.6 lines, the code 1,522 bytes smaller.
#   **Tried and left out**: shrink-wrapping (enableShrinkWrapping) changed nothing in the
#   hero — every function needs its callee-saved registers from its entry. Rematerializing
#   constants and addresses (isReMaterializableImpl past the dead CCR def, a flag-keeping
#   MOVEA or saved-flags move where CCR is live) cut the hero's spills from 1,091 to 671 and
#   reloads from 1,664 to 865 but no time: a reload is 16 cycles, MOVE #imm 12, and the
#   flag-keeping moves cost more. Spills are only a quarter of the vertical blank's stack
#   traffic; the rest is Lyra's aggregates passed and kept in memory (lyra/todo.md).
#   **A change here moves the timing of every game**: Vega's scripted hero checks judged
#   one frame, which faster code reached later in the walk (rom_checks.JUDGED_FROM).
#   Run Vega's --check, not lyrac's tests alone.
#
# Move the pin deliberately, and rerun lyrac's Genesis tests with SHELIAK set when you do.
# Needs cmake and ninja (`brew install cmake ninja`). About 20 minutes on 12 cores; a
# changed pin re-clones the source.
#
# Compile for the Genesis with the MEDIUM (or large) code model and static relocation:
#
#   llc -mtriple=m68k-unknown-elf -mcpu=M68000 -code-model=medium -relocation-model=static
#
# The small model, the default, writes a global through a PC-relative destination —
# `move.l d0, (counter,pc)` — which no 68000-family CPU accepts (an illegal instruction),
# and the Genesis's RAM at FF0000 is out of PC-relative reach from ROM anyway.
set -euo pipefail

readonly REVISION="e7dd336e0f7884c34108a1e722205a16c3f5307b"
readonly DIR="${LYRA_M68K_LLVM:-$HOME/Dev/llvm-m68k}"

for tool in cmake ninja git; do
  command -v "$tool" >/dev/null || { printf '%s not found (brew install cmake ninja)\n' "$tool" >&2; exit 1; }
done

mkdir -p "$DIR"
# One commit, shallow: the source is 2.6 GB even so. A checkout at another revision
# (an older pin) is replaced.
if [ -d "$DIR/src" ] && [ "$(git -C "$DIR/src" rev-parse HEAD 2>/dev/null)" != "$REVISION" ]; then
  rm -rf "$DIR/src"
fi
if [ ! -d "$DIR/src" ]; then
  git init -q "$DIR/src"
  git -C "$DIR/src" fetch -q --depth 1 https://github.com/llvm/llvm-project "$REVISION"
  git -C "$DIR/src" checkout -q FETCH_HEAD
fi

# The patches, each once: one that already applies in reverse is in. Ninja rebuilds what
# a patch touches, and lyrac's runtime cache is keyed on llc, so nothing else is needed.
for patch in "$(cd "$(dirname "$0")" && pwd)"/llvm-m68k-patches/*.patch; do
  [ -e "$patch" ] || continue
  if git -C "$DIR/src" apply --reverse --check "$patch" 2>/dev/null; then
    continue
  fi
  git -C "$DIR/src" apply "$patch" || { printf '%s does not apply to %s\n' "$patch" "$REVISION" >&2; exit 1; }
  printf 'applied %s\n' "$(basename "$patch")"
done

# Assertions on: the backend is experimental, and an assertion is a far better report of
# a miscompile than a ROM that does the wrong thing. Only the M68k target, and clang (for
# the C half of a runtime, and to compare its code with Lyra's).
cmake -S "$DIR/src/llvm" -B "$DIR/build" -G Ninja \
  -DCMAKE_BUILD_TYPE=Release \
  -DLLVM_ENABLE_ASSERTIONS=ON \
  -DLLVM_ENABLE_PROJECTS=clang \
  -DLLVM_TARGETS_TO_BUILD="" \
  -DLLVM_EXPERIMENTAL_TARGETS_TO_BUILD=M68k \
  -DLLVM_DEFAULT_TARGET_TRIPLE=m68k-unknown-elf \
  -DLLVM_INCLUDE_TESTS=OFF -DLLVM_INCLUDE_BENCHMARKS=OFF -DLLVM_INCLUDE_EXAMPLES=OFF \
  -DCLANG_ENABLE_STATIC_ANALYZER=OFF -DCLANG_ENABLE_ARCMT=OFF \
  -DLLVM_ENABLE_ZSTD=OFF -DLLVM_ENABLE_ZLIB=OFF -DLLVM_ENABLE_LIBXML2=OFF

ninja -C "$DIR/build" llc llvm-mc llvm-objdump llvm-readelf llvm-nm opt clang
printf 'built: %s/build/bin/llc\n' "$DIR"
