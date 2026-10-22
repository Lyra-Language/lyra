#!/usr/bin/env bash
#
# Build `bindings.gpu`'s C half into <out>/lib/liblyra-gpu.a: gpu.c — `ui.gpu`'s renderer over
# SDL3's GPU API, its Metal shaders compiled by SDL at run time — on macOS with SDL3; else
# gpu_stub.c, whose `lyra_gpu_create` answers NULL so `ui.gpu` falls back to SDL's 2D
# renderer, and says why on stderr. A `ui` program links either way.
#
#   bindings/gpu/build.sh <out>        # ./build.sh passes build/
set -euo pipefail

readonly HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly OUT="$(mkdir -p "${1:?usage: build.sh <out>}" && cd "$1" && pwd)"
readonly ARCHIVE="$OUT/lib/liblyra-gpu.a"
readonly CC="${LYRA_CC:-cc}"
readonly WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$OUT/lib"

stub() {
  "$CC" -O2 -std=c11 -c "$HERE/gpu_stub.c" -o "$WORK/stub.o"
  rm -f "$ARCHIVE"
  ar rcs "$ARCHIVE" "$WORK/stub.o"
  printf 'bindings/gpu: %s; built the stub\n' "$1" >&2
  exit 1
}

[ "$(uname -s)" = Darwin ] || stub "the GPU renderer's shaders are Metal's so far"
command -v pkg-config >/dev/null 2>&1 || stub "pkg-config not found"
pkg-config --exists sdl3 2>/dev/null || stub "SDL3 not found by pkg-config (package sdl3)"
"$CC" -O2 -std=c11 $(pkg-config --cflags sdl3) -c "$HERE/gpu.c" -o "$WORK/gpu.o"
"$CC" -O2 -fmodules -fobjc-arc $(pkg-config --cflags sdl3) -c "$HERE/window.m" -o "$WORK/window.o"
rm -f "$ARCHIVE"
ar rcs "$ARCHIVE" "$WORK/gpu.o" "$WORK/window.o"
