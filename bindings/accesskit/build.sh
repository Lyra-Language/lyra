#!/usr/bin/env bash
#
# Build `bindings.accesskit`'s C half into <out>/lib/liblyra-accesskit.a: accesskit.m — the
# snapshot and action queue over AccessKit's macOS adapter — with AccessKit's own static
# library merged in, so a program links one archive. Where that cannot be done (another
# platform, no network, no SDL3) the archive is accesskit_stub.c instead, and says why on
# stderr; a `ui` program links either way, and with the stub no assistive technology hears it.
#
#   bindings/accesskit/build.sh <out>        # ./build.sh passes build/
#
# **Downloaded, not vendored**: AccessKit's prebuilt C release (every platform's libraries,
# ~67 MB zipped) is pinned below by version and SHA-256 and lands in <out>/deps/; a second run
# with the archive built for this pin downloads and compiles nothing. AccessKit is MIT or
# Apache-2.0 (NOTICE.md); its license files are copied beside the archive.
#
# Needs curl, unzip, pkg-config's `sdl3` package and clang ($LYRA_CC, else clang).
set -euo pipefail

readonly ACCESSKIT_VERSION="0.23.1"
readonly ACCESSKIT_SHA256="35b7ca8a6f1e038b5da35e1e9e5a0adaed9bfcf21e1496d29598fbbadcc7043f"
readonly URL="https://github.com/AccessKit/accesskit-c/releases/download/${ACCESSKIT_VERSION}/accesskit-c-${ACCESSKIT_VERSION}.zip"

readonly HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly OUT="$(mkdir -p "${1:?usage: build.sh <out>}" && cd "$1" && pwd)"
readonly DEPS="$OUT/deps"
readonly ZIP="$DEPS/accesskit-c-${ACCESSKIT_VERSION}.zip"
readonly DIR="$DEPS/accesskit-c-${ACCESSKIT_VERSION}"
readonly ARCHIVE="$OUT/lib/liblyra-accesskit.a"
readonly STAMP="$OUT/lib/liblyra-accesskit.pin"
readonly CC="${LYRA_CC:-clang}"
readonly WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$OUT/lib"

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1"; else shasum -a 256 "$1"; fi |
    cut -d' ' -f1
}

# stub <why>: the archive that does nothing, and why on stderr — exit 1 so build.sh says so.
stub() {
  "$CC" -O2 -std=c11 -c "$HERE/accesskit_stub.c" -o "$WORK/stub.o"
  rm -f "$ARCHIVE" "$STAMP"
  ar rcs "$ARCHIVE" "$WORK/stub.o"
  printf 'bindings/accesskit: %s; built the stub\n' "$1" >&2
  exit 1
}

[ "$(uname -s)" = Darwin ] || stub "AccessKit is bridged on macOS only so far"
for tool in curl unzip pkg-config; do
  command -v "$tool" >/dev/null 2>&1 || stub "$tool not found"
done
pkg-config --exists sdl3 2>/dev/null || stub "SDL3 not found by pkg-config (package sdl3)"
case "$(uname -m)" in
  arm64) arch=arm64 ;;
  x86_64) arch=x86_64 ;;
  *) stub "no AccessKit library for $(uname -m)" ;;
esac

readonly PIN="$ACCESSKIT_VERSION $arch $(sha256 "$HERE/accesskit.m")"
if [ -f "$ARCHIVE" ] && [ "$(cat "$STAMP" 2>/dev/null)" = "$PIN" ]; then exit 0; fi

if ! [ -f "$ZIP" ] || [ "$(sha256 "$ZIP")" != "$ACCESSKIT_SHA256" ]; then
  mkdir -p "$DEPS"
  curl -fsSL --retry 2 -o "$ZIP.part" "$URL" || { rm -f "$ZIP.part"; stub "download failed: $URL"; }
  got="$(sha256 "$ZIP.part")"
  if [ "$got" != "$ACCESSKIT_SHA256" ]; then
    rm -f "$ZIP.part"
    stub "checksum mismatch for $URL (expected $ACCESSKIT_SHA256, got $got)"
  fi
  mv "$ZIP.part" "$ZIP"
fi
[ -d "$DIR" ] || unzip -q -o "$ZIP" -d "$DEPS"

"$CC" -O2 -fmodules -fobjc-arc $(pkg-config --cflags sdl3) -I"$DIR/include" \
  -c "$HERE/accesskit.m" -o "$WORK/accesskit.o"
rm -f "$ARCHIVE"
libtool -static -o "$ARCHIVE" "$WORK/accesskit.o" "$DIR/lib/macos/$arch/static/libaccesskit.a" 2>/dev/null
cp "$DIR/LICENSE-MIT" "$OUT/lib/accesskit-LICENSE-MIT"
cp "$DIR/LICENSE-APACHE" "$OUT/lib/accesskit-LICENSE-APACHE"
printf '%s' "$PIN" > "$STAMP"
