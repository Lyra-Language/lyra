#!/usr/bin/env bash
# Regenerates std/unicode/grapheme_table.lyra from the Unicode Character Database: downloads
# the three files tools/unicode/gen.lyra reads into build/deps/unicode-<version>/ (not
# vendored; each pinned with a SHA-256, so a changed download is refused), then runs it.
#
#   tools/unicode/unicode.sh            # needs curl and build/lyrac (./build.sh)
#
# A new Unicode version: change VERSION and the sums, run this, copy the version's
# GraphemeBreakTest.txt over cmd/lyrac/testdata/unicode/'s, and run
# `go test ./cmd/lyrac -run Unicode`, which also checks the table is what gen.lyra writes.
set -euo pipefail

readonly VERSION="17.0.0"
readonly BASE="https://www.unicode.org/Public/${VERSION}/ucd"
readonly FILES=(
  "auxiliary/GraphemeBreakProperty.txt d6b51d1d2ae5c33b451b7ed994b48f1f4dc62b2272a5831e7fd418514a6bae89"
  "emoji/emoji-data.txt 2cb2bb9455cda83e8481541ecf5b6dfda66a3bb89efa3fa7c5297eccf607b72b"
  "DerivedCoreProperties.txt 24c7fed1195c482faaefd5c1e7eb821c5ee1fb6de07ecdbaa64b56a99da22c08"
)

cd "$(dirname "$0")/../.."
dir="build/deps/unicode-${VERSION}"
mkdir -p "$dir"
for entry in "${FILES[@]}"; do
  path="${entry% *}"
  sum="${entry##* }"
  file="$dir/$(basename "$path")"
  if [[ ! -f "$file" ]] || [[ "$(shasum -a 256 "$file" | cut -d' ' -f1)" != "$sum" ]]; then
    curl -sSfL -o "$file.part" "$BASE/$path"
    got="$(shasum -a 256 "$file.part" | cut -d' ' -f1)"
    if [[ "$got" != "$sum" ]]; then
      rm -f "$file.part"
      echo "unicode.sh: $path has SHA-256 $got, not the pinned $sum" >&2
      exit 1
    fi
    mv "$file.part" "$file"
  fi
done
build/lyrac run tools/unicode/gen.lyra -- "$dir" > std/unicode/grapheme_table.lyra.new
mv std/unicode/grapheme_table.lyra.new std/unicode/grapheme_table.lyra
echo "std/unicode/grapheme_table.lyra: Unicode ${VERSION}"
