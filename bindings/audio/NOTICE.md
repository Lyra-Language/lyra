# Vendored decoders

`bindings.audio`'s C half (`audio.c`) compiles these, each a single file kept here as
released. Each is public domain, or at the user's choice MIT-0 — the text is at the end of
each file — so nothing linking them takes on a licence beyond Lyra's own MIT.

| File | Version | From |
|---|---|---|
| `dr_mp3.h` | v0.7.4 (2026-10-31) | github.com/mackron/dr_libs at `aec2dd20667f54acfdb66031217df6d240f56c52` |
| `dr_flac.h` | v0.13.4 (2026-10-31) | github.com/mackron/dr_libs at `806ee9ada8ca9827b22f76827f739c290f37ba48` |
| `dr_wav.h` | v0.14.6 (2026-10-31) | github.com/mackron/dr_libs at `35b5c392cc15456736c440a68dbe073178bbffe0` |
| `stb_vorbis.c` | v1.23 | github.com/nothings/stb at `52a3128f493138d01204272e030926ee693f5f2b` |

To move one on, fetch its file at a released commit (its third line names the version and
date), replace it here, update this table, and run `go test ./cmd/lyrac -run Audio`.
