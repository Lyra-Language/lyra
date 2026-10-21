# AccessKit

`bindings/accesskit/build.sh` downloads and links **AccessKit's C bindings**
(<https://github.com/AccessKit/accesskit-c>, release 0.23.1, pinned by SHA-256), copyright
the AccessKit authors, licensed under either the **MIT License** or the **Apache License,
Version 2.0**, at your option — the copies ship in the release and are copied beside the
archive as `build/lib/accesskit-LICENSE-MIT` and `accesskit-LICENSE-APACHE`. Parts derived
from Chromium carry Chromium's BSD-style license (`LICENSE.chromium` in the release).

Nothing of AccessKit is in this repository; a program built with `bindings.accesskit` (every
`ui` program, through `ui.sdl3`) links its static library, so a distributed binary carries
those notices with it.
