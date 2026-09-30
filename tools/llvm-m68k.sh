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
# The release matches Homebrew's LLVM; move both together. Needs cmake and ninja
# (`brew install cmake ninja`). About 20 minutes on 12 cores.
#
# Compile for the Genesis with the MEDIUM (or large) code model and static relocation:
#
#   llc -mtriple=m68k-unknown-elf -mcpu=M68000 -code-model=medium -relocation-model=static
#
# The small model, the default, writes a global through a PC-relative destination —
# `move.l d0, (counter,pc)` — which no 68000-family CPU accepts (an illegal instruction),
# and the Genesis's RAM at FF0000 is out of PC-relative reach from ROM anyway.
set -euo pipefail

readonly RELEASE="llvmorg-22.1.8"
readonly DIR="${LYRA_M68K_LLVM:-$HOME/Dev/llvm-m68k}"

for tool in cmake ninja git; do
  command -v "$tool" >/dev/null || { printf '%s not found (brew install cmake ninja)\n' "$tool" >&2; exit 1; }
done

mkdir -p "$DIR"
if [ ! -d "$DIR/src" ]; then
  git clone --depth 1 --branch "$RELEASE" https://github.com/llvm/llvm-project "$DIR/src"
fi

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
