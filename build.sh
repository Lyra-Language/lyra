#!/usr/bin/env bash
#
# Build the compiler and language server into build/, laid out the way an install is:
# the binaries with `std/` beside them.
#
#   ./build.sh
#
# That layout is not incidental. `lyrac` finds the standard library at `std/` next to
# its own executable (or wherever LYRA_STD points), which is the same convention Rust,
# Zig and Go use for their sysroot — so building this way exercises the resolution path
# every day instead of only at release time, and a program can use the prelude without
# any environment set up.
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

# Recreated each time so it cannot survive as a stale copy if std/ ever moves.
rm -f "$OUT/std"
ln -s ../std "$OUT/std"

printf 'built %s/{lyrac,lyra-lsp} with std -> ../std\n' "$OUT"
