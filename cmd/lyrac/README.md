# `cmd/lyrac`

Compiler CLI on `pkg/driver`. Build with `go build ./cmd/lyrac` (or `./build.sh`).

| command | does |
|---|---|
| `lyrac check <file>` | parse + typecheck; exit 1 on any error |
| `lyrac build <file>` | check, resolve the entry point, emit IR, link an executable |
| `lyrac run <file>` | build into a temp dir and exec; `--watch` rebuilds and restarts on save; `--live` swaps the change into the running program when it can |
| `lyrac doc <file>` | Markdown documentation, one page per module |

Diagnostics print as `path:line:col: severity[code]: message` (`line:col` omitted when
there is no location). On a terminal `severity[code]` is coloured — bold red for an error,
bold yellow for a warning; piped output stays plain. `NO_COLOR` turns colour off,
`FORCE_COLOR` on (`NO_COLOR` wins).

## `build`

```bash
lyrac build prog.lyra                        # -> ./prog, no IR left behind
lyrac build -o build/prog prog.lyra          # executable elsewhere
lyrac build --keep-ll prog.lyra              # executable *and* prog.ll
lyrac build --keep-ll -o build/prog prog.lyra # -> build/prog and build/prog.ll
lyrac build --emit-llvm prog.lyra            # prog.ll only; needs no C compiler
lyrac build --emit-llvm -o out.ll prog.lyra  # -o names the .ll
lyrac build -O0 prog.lyra                    # default -O2
lyrac build --cc /path/to/clang …            # else $LYRA_CC, else clang on PATH
lyrac build --progress prog.lyra             # "progress: 40% compiling for the 68000" lines on stderr
lyrac build --target ios-simulator prog.lyra # another platform: ios, ios-simulator, linux-arm64, linux-x86_64, macos
lyrac run --target linux-arm64 prog.lyra     # run there: the Simulator, or Docker for Linux
```

- Links with `clang <ir> -lm -o <exe>` plus `-l` flags from `@link` (`linkFlags`); `-lm` is
  unconditional, matching the backend tests. Library directories come first: the install's
  `<root>/lib` when it exists (`installLibDir` — where `build.sh` puts a binding's shim
  archive), then pkg-config's for each `pkg:`. A shared library in that `lib/` adds an rpath
  to it (`holdsSharedLibrary`), so the program finds it when it runs: Vega's release ships
  SDL3 there on macOS. The workspace's `lib/` holds archives only. **On Linux the libraries
  are one group** (`-Wl,--start-group … --end-group`): the links are a sorted set, and two
  static archives where one calls the other (FreeType and HarfBuzz, as Termina's release links
  them) need no order then.
- **Built in parts, each kept** (`objects.go`, `llvm.EmitParts`): the program is split — a
  part for each module's own code (a large module in pieces), program parts for what it
  makes for itself, `globals` — and each part is compiled on a core of its own and kept in
  `$LYRA_CACHE_DIR/objects` (else `lyra/objects` in the user's cache directory) under the
  hash of its text, the compiler and the flags. A rebuild compiles only the parts whose
  text changed, and `std`/`ui` parts are shared by every program. **`-O0` links ordinary
  objects; an optimized build links by ThinLTO** (`-flto=thin`, the linker's own cache in
  `objects/lto`), so code still inlines across parts. Vega at -O2: 230s whole, 72s cold in
  parts, 27s with nothing changed (the front end), 39s after a one-line edit.
  - **Built whole instead**, as before: `--emit-llvm`, `--keep-ll`, `run --live`, another
    platform or an app; an optimized build where the linker does no ThinLTO (Apple's ld64
    beside Apple clang does, or `ld.lld` on the PATH); `LYRA_NO_OBJECT_CACHE=1`; a module
    `EmitParts` refuses (`ErrNotSplittable`); and — with a note — a part that fails to
    compile, which is a split the backend got wrong.
  - Objects past 2 GB go least recently used first, looked at once an hour; ThinLTO's
    cache is pruned by its linker. `LYRA_DUMP_PARTS=<dir>` writes each part's IR, to see
    why a part compiled again.
- **The IR states the target's data layout** (`datalayout.go`), asked of the clang compiling
  it once per compiler and target: read without one, LLVM aligned an `alloca` by its default
  layout (`i128` to 8) and generated code by the target's (16 on x86_64), and a store to the
  slot faulted. `--emit-llvm` writes none.
- **`-o` and the `.ll`** (`llPath`): the `.ll` goes beside the executable `-o` names; under
  `--emit-llvm`, `-o` names the `.ll` itself.
- **Default `-O2`**: no debug info is emitted at any level, so `-O0` buys nothing. The level
  (`-O` plus anything) is passed to clang unexamined. "Compile it with" hints include it.
- **The compiler must accept `.ll`**, so plain `cc` is not a fallback. With no compiler found
  the build fails (exit 1) but still writes `<name>.ll` beside the source and prints the
  clang line.
- **`--progress`** prints a line on stderr as each stage starts (`o.report`): checking,
  then for the Genesis generating code, optimizing, compiling for the 68000, each runtime
  object a first build compiles (the slow part, 50–90%), linking. Percentages are a
  stage's place, not measured time; a build's end is the process ending. `run` reports the
  build's stages, `installing` for an app, and last `100% running` as the program starts —
  how Vega knows an iOS Run has become the game. Vega reads them to show a game's build.
- A sequence value needs clang ≥ 15 (`CheckCoroutineSupport`); lyrac refuses by name and
  keeps the `.ll`.
- The entry point (`driver.ResolveEntryPoint`) is checked only by `build`/`run`.

### For another platform (`platform.go`)

`--target` builds a `host` program for one of `pkg/target`'s platforms; without it, for this
machine's (`target.Machine()`), as before. Another platform's build is two steps: clang
`--target=<triple> [-isysroot <sdk>] -O2 -c prog.ll`, then its linker — Xcode's clang with the
platform's SDK (`xcrun --sdk iphonesimulator --show-sdk-path`) for iOS, `zig cc -target
aarch64-linux-gnu.2.36` (`$LYRA_ZIG`, else the PATH) for Linux. The triple is passed, never
written in the IR (clang normalises and warns). Libraries are `<root>/lib/<platform>/` and its
`lib/`, and a `pkg:` is answered by the platform's own `.pc` files there (`PKG_CONFIG_LIBDIR`,
`--static --libs`, in full — SDL3 for iOS brings its frameworks; `platformPkgFlags`), never
this machine's. `tools/sdl3-ios.sh` installs SDL3 for iOS and the Simulator so. `run --target` runs through the platform's runner (`runCommand`):

- `ios-simulator`: `xcrun simctl spawn <device> <exe> args`, the device `$LYRA_SIMULATOR`,
  else the first booted, else the first iPhone available, booted and left booted;
- `linux-*`: `docker run --rm -i --platform linux/<arch> -v <dir>:/lyra <image>`, the image
  `$LYRA_LINUX_IMAGE`, else `debian:bookworm-slim` (glibc 2.36, what programs are linked
  against);
- `ios`: an app installed and launched on a connected iPhone or iPad (`device.go`); a
  command-line program is refused, since only an app runs there.

**A program over SDL3 is an app on iOS** (`app.go`): its entry emitted as `lyra_main`,
`<root>/runtime/ios/main.c` (`SDL_RunApp`, then `exit` with the status) compiled and linked
beside it, `-o x` writing `x.app/` (the program, an `Info.plist` — identifier `--bundle-id`,
else `dev.lyra.x`; iPhone and iPad, landscape, `UILaunchScreen` — signed ad hoc). `run`
installs it in the Simulator and launches it with `simctl launch --console-pty`, `SDL_*` and
`TZ` passed as `SIMCTL_CHILD_…`; the app's status is its last line, `lyra-exit: N`
(`appOutput`), since `simctl` answers 0; an interrupt or SIGTERM to lyrac terminates the app
(`stopApp`) — the launcher alone would leave it running, and Vega's Stop is a SIGTERM
(`TestRun_AStoppedRunEndsItsAppInTheSimulator`).

**A device's app** (`device.go`) is signed with a provisioning profile: `$LYRA_IOS_PROFILE`,
else the newest unexpired one Xcode keeps (`~/Library/Developer/Xcode/UserData/Provisioning
Profiles/`) whose app identifier covers the app's — exact before a wildcard `TEAM.*` — read
with `security cms -D` and a small XML plist reader (`parsePlist`). The identity is the
keychain's whose certificate the profile carries (SHA-1, `security find-identity`); the
entitlements are the app's identifier, the team and `get-task-allow`; the profile goes in as
`embedded.mobileprovision`. `run` installs with `devicectl device install app` and launches
with `devicectl device process launch --console --terminate-existing`, `SDL_*` and `TZ` as
`DEVICECTL_CHILD_…`, on `$LYRA_IOS_DEVICE` or the first iPhone or iPad connected; an
interrupt terminates the app by its process. `device_test.go` checks a device app's signature
(`codesign --verify --strict`), skipping without a profile.

`--target` on a `genesis` program is refused. `platform_test.go` builds and runs a program
reading a directory (the platform's `struct dirent`) on each, skipping without Xcode, zig or
Docker.

### For the Genesis (`genesis.go`)

A program whose `lyra.toml` names `target = "genesis"` builds a cartridge instead
(`prog.bin`, or `-o`): the IR through `opt` (everything but `main` internalized, so the
prelude's host code drops out) and `llc` from the M68k LLVM (`tools/llvm-m68k.sh`, found at
`$LYRA_M68K_LLVM` or `~/Dev/llvm-m68k`), `<root>/runtime/genesis/runtime.c` and compiler-rt's
helpers compiled once and cached (`<user cache>/lyra-genesis`), linked by `pkg/rom`.
`--emit-llvm` stops at the IR. `run` plays the ROM in Sheliak (`$SHELIAK`, else the PATH),
arguments after `--` going to the emulator. `genesis_test.go` builds `examples/genesis` and a
panicking program when the toolchain is there, and runs them when `$SHELIAK` is set.

## `run`

- **`--` ends lyrac's arguments**; the rest reach `program_args()` (name at index 0).
  Required because `parseBuildArgs` accepts flags on either side of the source path.
  `build` refuses program arguments.
- Artifacts go in a temp dir (`buildOptions.ephemeral`), which also suppresses the
  missing-compiler `.ll` fallback. `-o`/`--emit-llvm`/`--keep-ll` are refused.
- **Prints no build summary** (`lowerAndEmit` returns the path; the caller reports), and the
  program's exit status is the command's.
- **`--watch`** (`watch.go`) rebuilds and restarts on a change to any file the build read
  (imports, prelude and `std` included), polling mtime and size every 250 ms; works for a
  Genesis program too, restarting Sheliak. A failed build prints its diagnostics and leaves
  the running program alone; the old one gets SIGTERM, then a kill after 2 s. Each build
  has its own temp dir. A module created *after* an import of it failed is noticed on the
  next save of a watched file. Restart only — state is not kept.
- **`--live`** (`live.go`; design `pkg/backend/LIVE.md`) is `--watch` that keeps the
  program's state: a save is built as the next **generation**, checked against generation
  0, and loaded into the running program at its next loop turn in `main`, which answers
  over a pipe (fds 3 and 4, `LYRA_LIVE` set — and unset at start-up, so a program it starts
  does not take them, with `LYRA_LIVE_PID` left naming the process for it to read: `ui.app`
  caps its wait for input then). **Built at -O0 unless an `-O` level is given**: each save
  compiles the whole program, and clang's optimizer was nearly all of it (24 s at -O2 against
  1 s at -O0 for 7GUIs' temperature converter). `lyrac: x.lyra changed; applied`, or
  `restarting: <why>` and a restart as `--watch` does — when `main` changed (its IR,
  normalized by the backend), a type's Lyra shape (`typeShapes`: fields, constructors in
  order) or LLVM layout changed, a top-level `let` is retyped, the link flags changed, the
  bundle would not link or load, or no answer came within 2 s (the loop is not in `main`,
  or it is blocked). A changed initializer is applied with a note: the value is kept. A
  new top-level `let` is applied, with a note that it was initialized, and recorded in
  `liveBase.added`, so later generations bind to it rather than define it again
  (`liveBase.process` is what the backend is given). A generation is `clang -bundle -bundle_loader <gen 0>
  -Wl,-undefined,dynamic_lookup`, linking no libraries, so a static archive's state is
  never duplicated; it lives in the run's temp dir and is never unloaded. **macOS only**
  (`liveAvailable`): elsewhere, and for a Genesis program, it says so once and is
  `--watch`. `live_test.go` runs two sessions (a body applied with the count kept, a
  reordered `data` type and a changed `main` restarting; a callback stored in `main`'s
  state reaching an edited body, and a new `var` initialized on apply and kept through the
  next swap; an idle `ui.app` program in a real SDL window taking a change, not restarted)
  and the check's every row.

## `doc`

Renders into `-o` (default `./docs`) with Starlight frontmatter, for
`lyra-website/src/content/docs/reference/`. `--private` (unexported), `--deps` (follow
imports), `--prelude` (implies `--deps`), `--strict` (non-zero on a gap). Model and renderer
are `pkg/docgen`.

- Refuses a program that does not type-check (signatures come from resolved types).
- An undocumented public declaration is listed with its signature; coverage prints every run.
- The prelude needs `--prelude` even under `--deps` (still documented when it is the entry).
- Impl methods are not counted as gaps.
- Page files are `std-prelude.md`, not `std.prelude.md` (site generators strip dots from
  slugs); the title keeps the dotted path.
