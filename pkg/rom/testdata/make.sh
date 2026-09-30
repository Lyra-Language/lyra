#!/usr/bin/env bash
# Rebuild the linker tests' objects with the M68k LLVM (tools/llvm-m68k.sh), the way
# lyrac compiles C for the Genesis: through IR, medium code model, static relocation.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
BIN="${LYRA_M68K_LLVM:-$HOME/Dev/llvm-m68k}/build/bin"
for src in start program helper unused weak strong; do
  "$BIN/clang" --target=m68k-unknown-elf -mcpu=68000 -O2 -ffreestanding -fno-builtin -fno-common \
    -S -emit-llvm "$src.c" -o "$src.ll"
  "$BIN/llc" -mtriple=m68k-unknown-elf -mcpu=M68000 -code-model=medium -relocation-model=static \
    -filetype=obj -O2 "$src.ll" -o "$src.o"
  rm "$src.ll"
done
