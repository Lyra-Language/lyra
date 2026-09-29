# RAR 5 test archives

`std.rar` is tested against these (`../../llvm_rar_test.go`). Two sources:

- **libarchive's test archives** (libarchive 3.8.9, `libarchive/test/test_read_format_rar5_*.rar.uu`,
  uudecoded and renamed without the prefix; `rar4.rar` is `test_read_format_rar.rar`, for the
  older format's refusal). They include deliberately malformed archives, which must be
  refused without a crash or a hang.
- **`filter-*.rar`**, made by `make_filters.py` here: one hand-built block of literals behind a
  filter command of each kind (x86 E8, E8/E9, ARM, delta). No RAR tool writes these on demand,
  and the libarchive archives exercise only the ARM and delta filters.

The expected results in the test were checked against libarchive itself — its `bsdtar`
(libarchive 3.7.4, the one macOS ships) extracting every archive it reads, and libarchive
3.8.9's own test expectations where the two differ (3.7.4 predates fixes for
`main_block_extra_bytes` and `only_crypt_exfld`). Homebrew's 7-Zip is no oracle here: it is
built without RAR decompression.

The libarchive archives are distributed under libarchive's licence:

```
Copyright (c) 2003-2018 <author(s)>
All rights reserved.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions
are met:
1. Redistributions of source code must retain the above copyright
   notice, this list of conditions and the following disclaimer
   in this position and unchanged.
2. Redistributions in binary form must reproduce the above copyright
   notice, this list of conditions and the following disclaimer in the
   documentation and/or other materials provided with the distribution.

THIS SOFTWARE IS PROVIDED BY THE AUTHOR(S) ``AS IS'' AND ANY EXPRESS OR
IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED WARRANTIES
OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE DISCLAIMED.
IN NO EVENT SHALL THE AUTHOR(S) BE LIABLE FOR ANY DIRECT, INDIRECT,
INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT
NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF
THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```
