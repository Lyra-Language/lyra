#!/usr/bin/env bash
#
# Make the 7z archives std.sevenzip is tested against (llvm_sevenzip_test.go), with 7-Zip
# itself (`7zz`, 26.03 when these were made). Re-run only to add a case: the archives are
# committed so the tests need no 7-Zip, and 7-Zip's output is not byte-stable across
# versions (the test compares what the archives hold, never their bytes).
#
# The files are made from formulas the test repeats (sevenzipFiles), so the test knows
# what every entry must extract to without keeping a second copy.
set -euo pipefail

readonly HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

python3 - "$WORK" <<'PY'
import os, sys
work = sys.argv[1]
def noise(n, seed):
    x, out = seed, bytearray()
    for _ in range(n):
        x = (x * 1103515245 + 12345) % (1 << 31)
        out.append((x >> 16) & 0xFF)
    return bytes(out)
text = "".join(f"line {i}: the quick brown fox {i * i % 977} jumps\n" for i in range(3000)).encode()
files = {
    "dir/text.txt": text,
    "dir/noise.bin": noise(6000, 1),
    "dir/sub/zeros.bin": bytes(100000),
    "mixed.bin": text[:20000] + noise(3000, 2) + text[:20000],
    "empty.txt": b"",
    "héllo wörld.txt": "héllo".encode(),
    "emoji 🎮.txt": b"game",
}
for name, data in files.items():
    path = os.path.join(work, "all", name)
    os.makedirs(os.path.dirname(path), exist_ok=True)
    open(path, "wb").write(data)
os.makedirs(os.path.join(work, "all", "emptydir"))
os.makedirs(os.path.join(work, "small"))
open(os.path.join(work, "small", "short.txt"), "wb").write(b"a short file\n" * 20)
PY

make() {
  local name="$1" set="$2"
  shift 2
  rm -f "$HERE/$name.7z"
  (cd "$WORK/$set" && 7zz a -bso0 -bsp0 "$@" "$HERE/$name.7z" .)
}

make default all
make ultra all -mx=9
make lzma all -m0=lzma
make lzma-lclp all -m0=lzma:lc=0:lp=2:pb=0
make deflate all -m0=deflate
make nonsolid all -ms=off
make plain-header all -mhc=off
make multithread all -mmt=4 -m0=lzma2:c=64k
make copy small -mx=0
make bzip2 small -m0=bzip2
make ppmd small -m0=ppmd
make bcj small -mf=BCJ
make encrypted small -psecret
make encrypted-header small -psecret -mhe=on
