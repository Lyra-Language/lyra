#!/usr/bin/env bash
#
# Build the C half of `bindings.imgui` into <out>/lib/liblyra-imgui.a:
# Dear ImGui (docking branch), dear_bindings' C API over it, ImGui's SDL3 and SDL_GPU
# backends, host.cpp (with host_macos.m on macOS), and generated.cpp — the field accessors
# gen/gen.lyra writes beside generated.lyra.
#
#   bindings/imgui/build.sh <out>        # ./build.sh passes build/
#
# **Downloaded, not vendored**: ImGui is ~2 MB of C++ that would otherwise sit in this
# repo. The versions are pinned below with a SHA-256 each, so a download that is not
# byte-for-byte the pinned one is refused rather than built. They land in
# <out>/deps/, and a second run with the archive already built for this pin downloads
# and compiles nothing.
#
# **Bumping ImGui** means moving IMGUI_TAG and DEAR_BINDINGS_TAG together (dear_bindings
# publishes a release per ImGui version, `…_ImGui_v<ver>-docking`), updating every hash,
# and regenerating `imgui.lyra` from the new dcimgui.json (gen/README.md).
#
# **No C++ runtime**: compiled with -fno-exceptions -fno-rtti -fno-threadsafe-statics,
# ImGui references nothing from libc++/libstdc++ (only `__cxa_atexit`, which libc has),
# so the archive links with the plain C driver lyrac runs — one `@link` on every platform.
#
# Needs curl, tar, pkg-config's `sdl3` package and a C++-capable clang ($LYRA_CC, else
# clang). Exits non-zero, with the reason on stderr, when any is missing.
set -euo pipefail

readonly IMGUI_TAG="v1.92.9b-docking"
readonly IMGUI_SHA256="90ded916bd57db2e0e171b6b098940a47c6f5042725dcdc67fb19940ca8bfdcc"

readonly DEAR_BINDINGS_TAG="DearBindings_v0.24_ImGui_v1.92.9b-docking"
readonly DCIMGUI_CPP_SHA256="c2cab9b75e52556ec3e38f599f83ec7477d4674a3a13ed5c60475855e39669ad"
readonly DCIMGUI_H_SHA256="433e891701a060bc88691664dc988251c72f0e4ad9d234c090121d108deb8bfb"
readonly DCIMGUI_JSON_SHA256="fb69b0460915d6fdd9dfacea74efef21b5398a17e45781c68789b1f71a2918d2"

readonly HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly OUT="$(mkdir -p "${1:?usage: build.sh <out>}" && cd "$1" && pwd)"
readonly DEPS="$OUT/deps"
readonly IMGUI_DIR="$DEPS/imgui-${IMGUI_TAG#v}"
readonly DB_DIR="$DEPS/$DEAR_BINDINGS_TAG"
readonly ARCHIVE="$OUT/lib/liblyra-imgui.a"
readonly STAMP="$OUT/lib/liblyra-imgui.pin"
readonly CC="${LYRA_CC:-clang}"

die() { printf 'bindings/imgui: %s\n' "$*" >&2; exit 1; }

for tool in curl tar pkg-config; do
  command -v "$tool" >/dev/null 2>&1 || die "$tool not found"
done
pkg-config --exists sdl3 2>/dev/null || die "SDL3 not found by pkg-config (package sdl3)"

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1"; else shasum -a 256 "$1"; fi |
    cut -d' ' -f1
}

# fetch <url> <dest> <sha256>: download to <dest> unless it is already there with the
# right hash. A partial or wrong download is removed, never left to be trusted later.
fetch() {
  if [ -f "$2" ] && [ "$(sha256 "$2")" = "$3" ]; then return; fi
  mkdir -p "$(dirname "$2")"
  curl -fsSL --retry 2 -o "$2.part" "$1" || { rm -f "$2.part"; die "download failed: $1"; }
  local got
  got="$(sha256 "$2.part")"
  if [ "$got" != "$3" ]; then
    rm -f "$2.part"
    die "checksum mismatch for $1 (expected $3, got $got)"
  fi
  mv "$2.part" "$2"
}

# The pin covers the downloads and the host's sources: a change to any rebuilds.
readonly PIN="$IMGUI_TAG $DEAR_BINDINGS_TAG $(sha256 "$HERE/host.cpp") $(sha256 "$HERE/host_macos.m") $(sha256 "$HERE/generated.cpp")"
if [ -f "$ARCHIVE" ] && [ -f "$STAMP" ] && [ "$(cat "$STAMP")" = "$PIN" ]; then
  exit 0
fi

fetch "https://github.com/ocornut/imgui/archive/refs/tags/$IMGUI_TAG.tar.gz" \
  "$DEPS/imgui-$IMGUI_TAG.tar.gz" "$IMGUI_SHA256"
if [ ! -f "$IMGUI_DIR/imgui.h" ]; then
  tar -xzf "$DEPS/imgui-$IMGUI_TAG.tar.gz" -C "$DEPS"
fi
readonly DB_URL="https://github.com/dearimgui/dear_bindings/releases/download/$DEAR_BINDINGS_TAG"
fetch "$DB_URL/dcimgui.cpp" "$DB_DIR/dcimgui.cpp" "$DCIMGUI_CPP_SHA256"
fetch "$DB_URL/dcimgui.h" "$DB_DIR/dcimgui.h" "$DCIMGUI_H_SHA256"
fetch "$DB_URL/dcimgui.json" "$DB_DIR/dcimgui.json" "$DCIMGUI_JSON_SHA256"

readonly OBJ="$DEPS/imgui-obj"
rm -rf "$OBJ"
mkdir -p "$OBJ" "$OUT/lib"
# shellcheck disable=SC2046 # pkg-config's flags are meant to split
for src in \
  "$IMGUI_DIR/imgui.cpp" "$IMGUI_DIR/imgui_draw.cpp" "$IMGUI_DIR/imgui_tables.cpp" \
  "$IMGUI_DIR/imgui_widgets.cpp" "$IMGUI_DIR/imgui_demo.cpp" \
  "$IMGUI_DIR/backends/imgui_impl_sdl3.cpp" "$IMGUI_DIR/backends/imgui_impl_sdlgpu3.cpp" \
  "$DB_DIR/dcimgui.cpp" "$HERE/host.cpp" "$HERE/generated.cpp"; do
  "$CC" -x c++ -std=c++11 -O2 -fno-exceptions -fno-rtti -fno-threadsafe-statics \
    -I"$IMGUI_DIR" -I"$IMGUI_DIR/backends" $(pkg-config --cflags sdl3) \
    -c "$src" -o "$OBJ/$(basename "${src%.*}").o" &
done
# The macOS half (host_macos.m has why); -fmodules records AppKit and libobjc in the object.
if [ "$(uname -s)" = Darwin ]; then
  "$CC" -O2 -fobjc-arc -fmodules -c "$HERE/host_macos.m" -o "$OBJ/host_macos.o" &
fi
wait_all() {
  local failed=0
  for job in $(jobs -p); do wait "$job" || failed=1; done
  [ "$failed" = 0 ] || die "compile failed"
}
wait_all

rm -f "$ARCHIVE"
ar rcs "$ARCHIVE" "$OBJ"/*.o
rm -rf "$OBJ"
printf '%s' "$PIN" >"$STAMP"
