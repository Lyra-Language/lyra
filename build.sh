#!/usr/bin/env bash
#
# Build the compiler and language server into build/, laid out the way an install is:
# the binaries with `std/` and `bindings/` beside them.
#
#   ./build.sh
#
# That layout is not incidental. `lyrac` finds the standard library at `std/` next to
# its own executable (or wherever LYRA_STD points), which is the same convention Rust,
# Zig and Go use for their sysroot — so building this way exercises the resolution path
# every day instead of only at release time, and a program can use the prelude without
# any environment set up.
#
# **`bindings/` is linked for the same reason and was missed when it was added**: a module
# path resolves under a *root*, and the root is this directory — so `bindings.sdl3` is
# `<root>/bindings/sdl3/` exactly as `std.prelude` is `<root>/std/prelude/`. Without the
# link, `examples/sdl3_window.lyra` could only be compiled with `LYRA_STD` pointed at the
# source tree, which is not how anyone runs the compiler.
#
# `std` is a **symlink** to the tracked sources, not a copy. A copy drifts silently: you
# would edit std/prelude.lyra, rebuild, and still get the old prelude. Every confusing
# failure this project has hit from staleness — a cached parser object, a cached test
# binary, a leftover compiler — presented as a behaviour difference rather than as
# staleness, which is exactly what makes them expensive. A real install would copy;
# development should not.
set -euo pipefail

readonly ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly OUT="$ROOT/build"

mkdir -p "$OUT"
go build -o "$OUT/lyrac" ./cmd/lyrac
go build -o "$OUT/lyra-lsp" ./cmd/lyra-lsp

# Recreated each time so neither can survive as a stale copy if its source ever moves.
for dir in std bindings; do
  rm -f "$OUT/$dir"
  ln -s "../$dir" "$OUT/$dir"
done

# **The C half of `bindings.menubar`**, into `build/lib/`, which lyrac searches: the real
# menu bar (`menubar.m`, AppKit autolinked through `-fmodules`) on macOS and a do-nothing
# stub elsewhere, so a program using the binding links on every platform. It needs only
# the C compiler lyrac already does.
readonly MENUBAR="$ROOT/bindings/menubar"
mkdir -p "$OUT/lib"
if [ "$(uname -s)" = Darwin ]; then
  "${LYRA_CC:-clang}" -O2 -fobjc-arc -fmodules -c "$MENUBAR/menubar.m" -o "$OUT/lib/menubar.o"
else
  "${LYRA_CC:-clang}" -O2 -std=c11 -c "$MENUBAR/menubar_stub.c" -o "$OUT/lib/menubar.o"
fi
rm -f "$OUT/lib/liblyra-menubar.a"
ar rcs "$OUT/lib/liblyra-menubar.a" "$OUT/lib/menubar.o"
rm -f "$OUT/lib/menubar.o"

# **lyrafmt is built when it can be**, and skipped with a note when it cannot. The
# formatter is a Lyra program that links the tree-sitter runtime and the grammar
# (examples/lyrafmt/libs.sh), so it needs a C compiler, `libtree-sitter` and the sibling
# tree-sitter-lyra checkout — none of which the compiler itself needs. Skipping keeps
# `./build.sh` working on a machine that has none of them.
#
# It lands **beside `lyra-lsp`**, which is where the language server looks for it after
# `$LYRA_FMT` and `$PATH`: that is what gives both editor extensions `Format Document`
# without either of them knowing the formatter exists.
fmt_note='(skipped lyrafmt)'
if [ -f "$ROOT/../tree-sitter-lyra/src/parser.c" ] && pkg-config --exists tree-sitter 2>/dev/null; then
  if "$ROOT/examples/lyrafmt/libs.sh" >/dev/null 2>&1 &&
    LIBRARY_PATH="$OUT/lib:$(pkg-config --variable=libdir tree-sitter)" \
      "$OUT/lyrac" build -o "$OUT/lyrafmt" "$ROOT/examples/lyrafmt/lyrafmt.lyra" >/dev/null 2>&1; then
    fmt_note='and lyrafmt'
  else
    fmt_note='(lyrafmt failed to build)'
  fi
fi

# **The C half of `bindings.imgui`**, into `build/lib/liblyra-imgui.a`, when it can be —
# the lyrafmt rule again. It downloads a pinned Dear ImGui and dear_bindings on first
# use (`bindings/imgui/build.sh` has the pins and why), and needs SDL3; a machine
# offline or without SDL3 builds everything else and says why this was skipped.
if imgui_err=$("$ROOT/bindings/imgui/build.sh" "$OUT" 2>&1); then
  imgui_note='lib/liblyra-imgui.a'
else
  imgui_note="no liblyra-imgui.a (${imgui_err##*: })"
fi

printf 'built %s/{lyrac,lyra-lsp} %s, lib/liblyra-menubar.a and %s, with std -> ../std, bindings -> ../bindings\n' "$OUT" "$fmt_note" "$imgui_note"
