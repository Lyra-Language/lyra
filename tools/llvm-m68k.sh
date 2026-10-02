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
