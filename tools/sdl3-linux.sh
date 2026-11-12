#!/usr/bin/env bash
#
# Build SDL3 for Linux from a Mac: what `lyrac build --target linux-arm64|linux-x86_64` links
# a program importing `bindings.sdl3` against — `std.game`'s games among them. zlib licence,
# so nothing here limits a game's.
#
#   tools/sdl3-linux.sh [out] [platform…]   # out defaults to build/; platforms to both
#                                           #   -> out/lib/{linux-arm64,linux-x86_64}/
#
# Each platform's SDL3 is installed under `<out>/lib/<platform>/` — `lib/libSDL3.a`,
# `include/SDL3/`, `lib/pkgconfig/sdl3.pc` — where lyrac looks for a platform's libraries
# (`platformLibDir`), as `tools/sdl3-ios.sh` installs iOS's; `build.sh` then builds
# `bindings.audio`'s C half there too (with `zig cc`, as lyrac links).
#
# **Built in Debian 12** (`debian:bookworm`, by Docker for the platform's processor — x86_64
# under emulation on an Apple Silicon Mac): the glibc lyrac links Linux programs against
# (`glibcVersion`, 2.36) and the image `lyrac run` runs them in. The output directory is
# mounted at its own path, so `sdl3.pc`'s prefix names it on the Mac too.
#
# **Static, and its video and audio back ends loaded at run time**: SDL is configured with
# X11, Wayland, ALSA, PulseAudio and PipeWire, each `dlopen`ed when the program starts (SDL's
# default), so a program built here links no desktop library and runs on any desktop with one
# of them — or none, under SDL's dummy drivers, as Vega's Run and checks play it in Docker.
#
# **Its atomics inline on arm64** (`-mno-outline-atomics`): GCC's default calls libgcc's
# `__aarch64_cas4_sync` and friends, which `zig cc`, linking with its own compiler-rt, has not.
#
# **Pinned to the release `tools/sdl3-ios.sh` is**, SHA-256 checked. Move both together.
#
# Needs Docker and curl. Minutes a platform (x86_64's emulated, longer); a second run with
# the pin unchanged builds nothing.
set -euo pipefail

readonly SDL_VERSION="3.4.18"
readonly SDL_SHA256="9c75cf16330322c217dedd2e0609f1124f1b54b8633e763467b4684d0f4334a3"
readonly IMAGE="debian:bookworm"

readonly HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly OUT="$(mkdir -p "${1:-$HERE/../build}" && cd "${1:-$HERE/../build}" && pwd)"
shift || true
readonly DEPS="$OUT/deps"
readonly SRC="$DEPS/SDL3-$SDL_VERSION"

die() { printf 'tools/sdl3-linux: %s\n' "$*" >&2; exit 1; }

for tool in curl tar docker; do
  command -v "$tool" >/dev/null 2>&1 || die "$tool not found"
done
docker info >/dev/null 2>&1 || die "Docker is not running"

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

# build <platform>: SDL3 configured, built and installed for one platform in a container,
# unless the pin says it already is.
build() {
  local platform="$1"
  local arch cflags=""
  case "$platform" in
    linux-arm64) arch=arm64 cflags="-mno-outline-atomics" ;;
    linux-x86_64) arch=amd64 ;;
    *) die "no such platform: $platform (linux-arm64 or linux-x86_64)" ;;
  esac
  local prefix="$OUT/lib/$platform"
  local stamp="$prefix/sdl3.pin"
  local pin="$SDL_VERSION $IMAGE inline-atomics"
  if [ -f "$prefix/lib/libSDL3.a" ] && [ -f "$stamp" ] && [ "$(cat "$stamp")" = "$pin" ]; then
    printf 'tools/sdl3-linux: SDL3 %s for %s is built\n' "$SDL_VERSION" "$platform"
    return
  fi
  local work="$DEPS/sdl3-$platform"
  rm -rf "$work" "$prefix"
  mkdir -p "$work" "$prefix"
  printf 'tools/sdl3-linux: building SDL3 %s for %s in %s (log: %s.log)\n' \
    "$SDL_VERSION" "$platform" "$IMAGE" "$work"
  docker run --rm --platform "linux/$arch" -v "$OUT:$OUT" "$IMAGE" bash -euc "
    export DEBIAN_FRONTEND=noninteractive
    apt-get update -qq
    apt-get install -y -qq --no-install-recommends build-essential cmake ninja-build pkg-config \
      libx11-dev libxext-dev libxrandr-dev libxcursor-dev libxi-dev libxss-dev libxfixes-dev libxtst-dev \
      libxkbcommon-dev libwayland-dev wayland-protocols libegl-dev libgl-dev libdrm-dev libgbm-dev \
      libasound2-dev libpulse-dev libpipewire-0.3-dev libudev-dev libdbus-1-dev >/dev/null
    cmake -S '$SRC' -B '$work' -G Ninja \
      -DCMAKE_BUILD_TYPE=Release \
      -DCMAKE_POSITION_INDEPENDENT_CODE=ON -DCMAKE_C_FLAGS='$cflags' \
      -DCMAKE_INSTALL_PREFIX='$prefix' -DCMAKE_INSTALL_LIBDIR=lib \
      -DSDL_SHARED=OFF -DSDL_STATIC=ON \
      -DSDL_TESTS=OFF -DSDL_EXAMPLES=OFF
    cmake --build '$work'
    cmake --install '$work'
  " >"$work.log" 2>&1 || die "building for $platform failed; see $work.log"
  printf '%s\n' "$pin" >"$stamp"
  printf 'tools/sdl3-linux: SDL3 %s for %s in %s\n' "$SDL_VERSION" "$platform" "$prefix"
}

fetch "https://github.com/libsdl-org/SDL/releases/download/release-$SDL_VERSION/SDL3-$SDL_VERSION.tar.gz" \
  "$DEPS/SDL3-$SDL_VERSION.tar.gz" "$SDL_SHA256"
if [ ! -f "$SRC/CMakeLists.txt" ]; then
  tar -xzf "$DEPS/SDL3-$SDL_VERSION.tar.gz" -C "$DEPS"
fi

if [ "$#" -eq 0 ]; then set -- linux-arm64 linux-x86_64; fi
for platform in "$@"; do
  build "$platform"
done
