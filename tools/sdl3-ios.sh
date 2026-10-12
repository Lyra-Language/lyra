#!/usr/bin/env bash
#
# Build SDL3 for iOS and the iOS Simulator: what `lyrac build --target ios|ios-simulator`
# links a program importing `bindings.sdl3` against — `std.game`'s games among them. zlib
# licence, so nothing here limits a game's.
#
#   tools/sdl3-ios.sh [out]        # out defaults to build/  -> out/lib/{ios,ios-simulator}/
#
# Each platform's SDL3 is installed under `<out>/lib/<platform>/` — `lib/libSDL3.a`,
# `include/SDL3/`, `lib/pkgconfig/sdl3.pc` — which is where lyrac looks for a platform's
# libraries (`platformLibDir`), and whose `sdl3.pc` answers `@link(…, pkg: "sdl3")` for it
# with every framework the static library needs (`linkFlags`).
#
# **Static, not the Xcode project's framework**: a framework is a dynamic library, which an
# app must embed, sign and find at run time; a static `libSDL3.a` is linked into the program
# and needs nothing beside it. CMake builds it (docs/README-cmake.md, "Building for iOS").
#
# **Pinned to a release, SHA-256 checked**: the release Homebrew's `sdl3` is (3.4.x, what
# `bindings.sdl3` is written against), not a development checkout. A download that is not
# byte for byte the pinned one is refused. Move SDL_VERSION and SDL_SHA256 together, with
# Homebrew's (`brew info --json=v2 sdl3`).
#
# **Without HIDAPI** (`-DSDL_HIDAPI=OFF`): CMake's iOS build links CoreBluetooth for it,
# and an app touching Bluetooth without an `NSBluetoothAlwaysUsageDescription` is killed
# by iOS. Game controllers come through Apple's GameController framework regardless.
#
# Needs Xcode (the iOS SDKs), cmake and ninja (`brew install cmake ninja`) and curl. About
# a minute a platform; a second run with the pin unchanged builds nothing.
set -euo pipefail

readonly SDL_VERSION="3.4.18"
readonly SDL_SHA256="9c75cf16330322c217dedd2e0609f1124f1b54b8633e763467b4684d0f4334a3"
# The oldest iOS a program runs on: the platforms' triples' (pkg/target/platform.go).
readonly IOS_MIN="17.0"

readonly HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly OUT="$(mkdir -p "${1:-$HERE/../build}" && cd "${1:-$HERE/../build}" && pwd)"
readonly DEPS="$OUT/deps"
readonly SRC="$DEPS/SDL3-$SDL_VERSION"

die() { printf 'tools/sdl3-ios: %s\n' "$*" >&2; exit 1; }

for tool in curl tar cmake ninja xcrun; do
  command -v "$tool" >/dev/null 2>&1 || die "$tool not found"
done

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1"; else shasum -a 256 "$1"; fi |
    cut -d' ' -f1
}

# fetch <url> <dest> <sha256>: download to <dest> unless it is already there with the right
# hash. A partial or wrong download is removed, never left to be trusted later.
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

# build <platform> <sdk>: SDL3 configured, built and installed for one platform, unless the
# pin says it already is.
build() {
  local platform="$1" sdk="$2"
  local prefix="$OUT/lib/$platform"
  local stamp="$prefix/sdl3.pin"
  local pin="$SDL_VERSION $IOS_MIN"
  if [ -f "$prefix/lib/libSDL3.a" ] && [ -f "$stamp" ] && [ "$(cat "$stamp")" = "$pin" ]; then
    printf 'tools/sdl3-ios: SDL3 %s for %s is built\n' "$SDL_VERSION" "$platform"
    return
  fi
  xcrun --sdk "$sdk" --show-sdk-path >/dev/null 2>&1 || die "no $sdk SDK: install Xcode"
  local work="$DEPS/sdl3-$platform"
  rm -rf "$work" "$prefix"
  cmake -S "$SRC" -B "$work" -G Ninja \
    -DCMAKE_BUILD_TYPE=Release \
    -DCMAKE_SYSTEM_NAME=iOS \
    -DCMAKE_OSX_SYSROOT="$sdk" \
    -DCMAKE_OSX_ARCHITECTURES=arm64 \
    -DCMAKE_OSX_DEPLOYMENT_TARGET="$IOS_MIN" \
    -DCMAKE_INSTALL_PREFIX="$prefix" \
    -DSDL_SHARED=OFF -DSDL_STATIC=ON \
    -DSDL_HIDAPI=OFF \
    -DSDL_TESTS=OFF -DSDL_EXAMPLES=OFF >"$work.log" 2>&1 ||
    die "configuring for $platform failed; see $work.log"
  cmake --build "$work" >>"$work.log" 2>&1 || die "building for $platform failed; see $work.log"
  cmake --install "$work" >>"$work.log" 2>&1 || die "installing for $platform failed; see $work.log"
  printf '%s\n' "$pin" >"$stamp"
  printf 'tools/sdl3-ios: SDL3 %s for %s in %s\n' "$SDL_VERSION" "$platform" "$prefix"
}

fetch "https://github.com/libsdl-org/SDL/releases/download/release-$SDL_VERSION/SDL3-$SDL_VERSION.tar.gz" \
  "$DEPS/SDL3-$SDL_VERSION.tar.gz" "$SDL_SHA256"
if [ ! -f "$SRC/CMakeLists.txt" ]; then
  tar -xzf "$DEPS/SDL3-$SDL_VERSION.tar.gz" -C "$DEPS"
fi

build ios-simulator iphonesimulator
build ios iphoneos
