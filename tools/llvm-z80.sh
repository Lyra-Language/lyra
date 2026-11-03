#!/usr/bin/env bash
#
# Build LLVM with Lyra's own Z80 target, for the Master System and Game Gear: the target's
# source is tools/llvm-z80/Z80 (linked into the tree), and tools/llvm-z80/patches tell LLVM
# the triple, its data layout, the ELF machine and its libcalls. See tools/llvm-z80/README.md.
#
#   tools/llvm-z80.sh            # clone (once), configure, build -> $LYRA_Z80_LLVM/build/bin
#
# LYRA_Z80_LLVM defaults to ~/Dev/llvm-z80, apart from the M68k tree (tools/llvm-m68k.sh) so
# that work on this target never rebuilds or disturbs the one Genesis games are built with.
# The same LLVM commit as that tree. Needs cmake and ninja; about 10 minutes on 12 cores.
set -euo pipefail

readonly REVISION="e7dd336e0f7884c34108a1e722205a16c3f5307b"
readonly DIR="${LYRA_Z80_LLVM:-$HOME/Dev/llvm-z80}"
readonly HERE="$(cd "$(dirname "$0")" && pwd)/llvm-z80"

for tool in cmake ninja git; do
  command -v "$tool" >/dev/null || { printf '%s not found (brew install cmake ninja)\n' "$tool" >&2; exit 1; }
done

mkdir -p "$DIR"
if [ ! -d "$DIR/src" ]; then
  git init -q "$DIR/src"
  git -C "$DIR/src" fetch -q --depth 1 https://github.com/llvm/llvm-project "$REVISION"
  git -C "$DIR/src" checkout -q FETCH_HEAD
fi

for patch in "$HERE"/patches/*.patch; do
  [ -e "$patch" ] || continue
  if git -C "$DIR/src" apply --reverse --check "$patch" 2>/dev/null; then
    continue
  fi
  git -C "$DIR/src" apply "$patch" || { printf '%s does not apply to %s\n' "$patch" "$REVISION" >&2; exit 1; }
  printf 'applied %s\n' "$(basename "$patch")"
done
ln -sfn "$HERE/Z80" "$DIR/src/llvm/lib/Target/Z80"

# Assertions on: a young backend's assertion is a far better report than a wrong ROM.
cmake -S "$DIR/src/llvm" -B "$DIR/build" -G Ninja \
  -DCMAKE_BUILD_TYPE=Release \
  -DLLVM_ENABLE_ASSERTIONS=ON \
  -DLLVM_TARGETS_TO_BUILD="" \
  -DLLVM_EXPERIMENTAL_TARGETS_TO_BUILD=Z80 \
  -DLLVM_DEFAULT_TARGET_TRIPLE=z80-unknown-elf \
  -DLLVM_INCLUDE_TESTS=OFF -DLLVM_INCLUDE_BENCHMARKS=OFF -DLLVM_INCLUDE_EXAMPLES=OFF \
  -DLLVM_ENABLE_ZSTD=OFF -DLLVM_ENABLE_ZLIB=OFF -DLLVM_ENABLE_LIBXML2=OFF

ninja -C "$DIR/build" llc opt llvm-readelf llvm-nm llvm-objdump
printf 'built: %s/build/bin/llc\n' "$DIR"
