# `cmd/lyrac`

Compiler CLI on `pkg/driver`. Build with `go build ./cmd/lyrac` (or `./build.sh`).

| command | does |
|---|---|
| `lyrac check <file>` | parse + typecheck; exit 1 on any error |
| `lyrac build <file>` | check, resolve the entry point, emit IR, link an executable |
| `lyrac run <file>` | build into a temp dir and exec |
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
```

- Links with `clang <ir> -lm -o <exe>` plus `-l` flags from `@link` (`linkFlags`); `-lm` is
  unconditional, matching the backend tests. Library directories come first: the install's
  `<root>/lib` when it exists (`installLibDir` — where `build.sh` puts a binding's shim
  archive), then pkg-config's for each `pkg:`.
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
  stage's place, not measured time; the process ending is the end. Vega reads them to show
  a game's build.
- A sequence value needs clang ≥ 15 (`CheckCoroutineSupport`); lyrac refuses by name and
  keeps the `.ll`.
- The entry point (`driver.ResolveEntryPoint`) is checked only by `build`/`run`.

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
