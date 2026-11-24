#!/usr/bin/env bash
# Fetches resvg's SVG test suite into cmd/lyrac/testdata/svg/resvg/ (not vendored: 25 MB of
# SVGs, the PNGs they should render as, the images they link to and the fonts the text tests'
# references were drawn in). Pinned by commit, which git checks as it fetches; a run with that
# commit already there does nothing, so the workspace's setup.sh runs it every time.
#
#   tools/resvg-tests.sh            # needs git
#
# A newer suite: change COMMIT and run this.
set -euo pipefail

readonly COMMIT="1938e970a643b0585dea33f47b3a33d85f5f8b36"
readonly REPO="https://github.com/linebender/resvg.git"
readonly SUITE="crates/resvg/tests"

cd "$(dirname "$0")/.."
out="cmd/lyrac/testdata/svg/resvg"
stamp="$out/COMMIT"
if [[ -f "$stamp" ]] && [[ "$(cat "$stamp")" == "$COMMIT" ]]; then
  echo "resvg tests: up to date (${COMMIT:0:7})"
  exit 0
fi

# A sparse, shallow clone of the one commit: only the suite's blobs are downloaded.
clone="build/deps/resvg-${COMMIT:0:12}"
if [[ ! -d "$clone/.git" ]]; then
  rm -rf "$clone"
  mkdir -p "$clone"
  git -C "$clone" init -q
  git -C "$clone" remote add origin "$REPO"
  git -C "$clone" sparse-checkout set --no-cone "/$SUITE/tests/" "/$SUITE/resources/" \
    "/$SUITE/fonts/" /LICENSE-MIT /LICENSE-APACHE
  if ! git -C "$clone" fetch -q --depth 1 --filter=blob:none origin "$COMMIT"; then
    rm -rf "$clone"
    echo "resvg-tests.sh: could not fetch $COMMIT from $REPO" >&2
    exit 1
  fi
  git -C "$clone" checkout -q FETCH_HEAD
fi

rm -rf "$out.part"
mkdir -p "$out.part"
cp -R "$clone/$SUITE/tests" "$clone/$SUITE/resources" "$clone/$SUITE/fonts" "$out.part/"
cp "$clone/LICENSE-MIT" "$clone/LICENSE-APACHE" "$out.part/"
cat > "$out.part/README.md" <<EOF
# resvg's test suite

Fetched by \`tools/resvg-tests.sh\` from $REPO at commit $COMMIT
(\`$SUITE/\`), MIT or Apache-2.0 (\`LICENSE-MIT\`, \`LICENSE-APACHE\`). The fonts carry their
own licences in \`fonts/\`. Not tracked: the script makes this folder again.

- \`tests/<area>/<element or property>/<case>.svg\`, each beside \`<case>.png\`, the expected render.
- \`resources/\` holds the images and SVGs the tests link to (\`../../../resources/…\`).
- \`fonts/\` holds the fonts the text tests' references were rendered with.
EOF
echo "$COMMIT" > "$out.part/COMMIT"
rm -rf "$out"
mv "$out.part" "$out"
echo "resvg tests: $(find "$out/tests" -name '*.svg' | wc -l | tr -d ' ') SVGs at ${COMMIT:0:7} in $out"
