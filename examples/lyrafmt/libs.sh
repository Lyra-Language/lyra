#!/usr/bin/env bash
#
# Build the Lyra grammar as a static library lyrafmt links against, into lyra/build/lib:
# tree-sitter-lyra/src/parser.c + scanner.c → libtree-sitter-lyra.a. The tree-sitter
# *runtime* is a system library (`brew install tree-sitter`, or libtree-sitter-dev), found
# through pkg-config; the copy the grammar's npm tooling vendors is too old for the parser
# it generates (language version 14 against 15), which is why it is not used.
#
#   examples/lyrafmt/libs.sh
#   LIBRARY_PATH=build/lib:$(pkg-config --variable=libdir tree-sitter) \
#     build/lyrac run examples/lyrafmt/lyrafmt.lyra -- examples/primes.lyra
#
# `@link` names a library and cannot say where it is (bindings/README.md); LIBRARY_PATH is
# how every binding here is found, the same way raylib's is.
set -euo pipefail
readonly LYRA="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
readonly GRAMMAR="$LYRA/../tree-sitter-lyra"
readonly OUT="$LYRA/build/lib"
if ! pkg-config --exists tree-sitter; then
  echo "libs.sh: no tree-sitter runtime (pkg-config tree-sitter); brew install tree-sitter" >&2
  exit 1
fi
mkdir -p "$OUT"
CC="${LYRA_CC:-clang}"
"$CC" -O2 -std=c11 -c "$GRAMMAR/src/parser.c" -I "$GRAMMAR/src" -o "$OUT/parser.o"
"$CC" -O2 -std=c11 -c "$GRAMMAR/src/scanner.c" -I "$GRAMMAR/src" -o "$OUT/scanner.o"
ar rcs "$OUT/libtree-sitter-lyra.a" "$OUT/parser.o" "$OUT/scanner.o"
rm -f "$OUT"/*.o "$OUT/libtree-sitter.a"
echo "libs.sh: wrote $OUT/libtree-sitter-lyra.a (runtime: $(pkg-config --variable=libdir tree-sitter))"
