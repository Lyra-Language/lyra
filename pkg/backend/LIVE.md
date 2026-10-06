# Live reload — design

**Status: proposed, 10/06.** The second stage of hot reload. The first, `lyrac run --watch`
(`cmd/lyrac/watch.go`), restarts the program on every save. This stage swaps changed code
into the running program, so its state — `main`'s locals, the heap, globals, and whatever
it holds through C (the window, the GPU device, the ImGui context) — survives the edit.

## What it does

```bash
lyrac run --live src/vega.lyra -- hero.vega
```

Everything `--watch` does, except that a good save is **applied** to the running program
at its next frame rather than restarting it. When applying would be unsafe, lyrac restarts
it as `--watch` does and says why, so `--live` is never worse than `--watch`:

```
lyrac: app.lyra changed; applied (3 functions)
lyrac: project.lyra changed; restarting: `Project` gained a field (`dirty`)
```

| an edit to… | live | why |
|---|---|---|
| a function's body (not `main`'s) | applied at the next frame | the common case: logic, layout, colours, a `const` it reads |
| a function's signature | applied, if `main` still compiles to the same code | callers in the new code agree with it; `main` is checked separately |
| a new function, type, or `import` | applied | nothing running knows it yet |
| a new top-level `let` | applied, initialized when applied | |
| a type's fields, constructors or their order | **restart** | values already in memory have the old shape |
| a top-level `let`'s type | **restart** | its storage has the old shape |
| a top-level `let`'s initializer | kept, with a note | the value is state; restarting is how to re-run it |
| `main` itself, or a `const` it reads | **restart** | `main` is the frame that never returns (below) |
| a new `extern` or `@link` | **restart** if the executable lacks the symbol | foreign code is loaded once |

Host target, macOS and Linux. A Genesis program has no dynamic loader; `--live` there is
`--watch` (see [Later](#later)).

## The shape of a program it serves

Vega's `main` is the model, and so are the SDL3 examples:

```lyra
let main = () -> u8 => {
  var app = new_app()                        // the state
  let Some(host) = create_host(…) else { … } // what C holds
  for frames != limit && app.run_frame(host) { frames += 1 }
  …
}
```

**One frame stays on the stack for the program's whole life — `main`'s — and everything it
calls returns every time round the loop.** That observation is the design. If the loop's
calls reach new code, the next frame is the new program; `main`'s own frame need never be
replaced, and the state in it never moves.

## Mechanism

### Generations

- **Generation 0** is the executable, built as today plus the live pieces below.
- **Generation N** (N ≥ 1) is a shared library of the **whole program**, compiled against
  generation 0: the same front end and backend, with a `LiveOptions{Gen, Base}` telling the
  backend what generation 0 defines.
- **A generation is never unloaded.** Old code stays mapped for as long as anything can
  reach it: a closure's lifted body and drop glue, a `Seq` coroutine's resume function, a
  string literal's pinned box, a C callback registered earlier. Unloading would mean proving
  none of those survives; keeping the code costs the pages of a library per reload, which
  are clean and file-backed.
- **Every applied generation is compatible with generation 0**, not just with the one
  before it. That is the invariant that makes skipping a generation (two saves while one
  frame ran) free: each is complete, and each is checked against the same base.

### The reload point: `main`'s loop back-edges

In a `--live` build, every loop back-edge in `main` tests one flag — a load and a
predictable branch — and calls `lyra_live_apply` when it is set. `main` is the only
function that polls, so:

- **The only frames on the stack at a reload are `main`'s.** No other function is mid-call
  when code changes, so nothing returns into a body that no longer matches its caller.
- **A tight loop elsewhere pays nothing.** A poll in every function would put a branch in
  every inner loop and apply reloads halfway through a frame.

A program whose frame loop lives in a function `main` calls (`run_game()`) never reaches a
poll. It is told so — after a second without an acknowledgement, lyrac restarts it and says
the loop must be in `main` — rather than mysteriously never updating. See [open
question 2](#open-questions).

### Indirect calls: only where the frame stays

Every call **in `main`** to a Lyra function goes through a slot: an exported global
`@lyra.live.slot.<symbol>` holding the function's address, loaded and called. Patching a
slot is the reload. Every other call stays direct, so the rest of the program keeps its
inlining, and `-O2` still means what it says.

- One funnel: a pass over the finished `ir.Module`, after lowering, rewrites `call @f` in
  `@main` where `@f` is a defined Lyra function (not an extern, not a `lyra_` runtime shim).
  Every call shape — plain, method, trait, specialization — is a `call` by then, so the
  rewrite cannot miss one, which a change at each lowering site could (rule 8).
- The slot's load is also what stops LLVM inlining `run_frame` into `main`, which would
  otherwise leave nothing to patch.
- Runtime shims stay direct and bound to generation 0: they are stateless, or their state is
  a global (below).

### Globals bind to generation 0

A generation N must see the state generation 0 created, never a fresh zeroed copy. So in a
generation N, **every mutable global that generation 0 defines is declared `external`** —
the `lyra_global_*` slots of top-level `let`/`var`s, and the runtime's own (`argc`/`argv`
for `program_args`, `tui.go`'s saved terminal state) — and the dynamic linker binds them
to the executable's when the library is loaded. One rule, no per-global bookkeeping.

A global **new** in generation N is defined there, and generation N's `lyra_live_patch`
(below) runs its initializer, in declaration order, before any slot is patched.

Generation 0 makes these mutable globals default-linkage and exports them, with its slots,
through an explicit export list (`-exported_symbols_list` on macOS, `--dynamic-list` on
Linux) — not `-rdynamic`, which would export every function too (see the next point).

### Each generation binds its own functions

On Linux a shared library's calls to its own exported functions can be **interposed** by a
same-named symbol in the executable — generation N's `run_frame` calling generation 0's
`draw_map` because the executable exports one. Generation N is linked
`-Wl,-Bsymbolic-functions`, and generation 0 exports no functions. macOS's two-level
namespace already binds a library's calls within it.

### Function values made from named functions

A named function used as a value already gets a thunk, `@name.closure` (`closures.go`). In
a `--live` build the thunk calls through generation 0's slot when there is one, so
`button.on_click = save_project` stored in `app` reaches the newest `save_project`.
(Generation 0 gets a slot for every function used as a value, as well as every function
`main` calls.)

**A lambda's lifted body is not patched**: a closure created before a reload keeps running
the code it was created with until the program makes a new one. Matching an anonymous
lambda across two compiles is guesswork; a closure is usually rebuilt within a frame anyway.

### Foreign libraries

- **Generation N links no libraries.** It is built with `-undefined dynamic_lookup` (macOS;
  Linux allows undefined symbols in a shared library by default), so every `extern`
  resolves at load to what the process already has — one SDL, one ImGui context. Linking a
  static archive into a generation would give it a **second copy of the archive's state**
  (ImGui's current context, the menu bar's delegate), and the program would split in two.
- **Generation 0 links static archives whole** (`-force_load` / `--whole-archive`) and
  exports their symbols, so a function generation N calls for the first time is there. An
  extern that is still missing makes the load fail, and lyrac restarts with the symbol named.

### The protocol

```
lyrac (watching)                          the program (generation 0)
────────────────                          ──────────────────────────
save seen; build gen N as a library
check gen N against gen 0  ── restart? ─→ (stop; start a new gen 0)
write "load <path>\n" to the live pipe
send SIGUSR1  ──────────────────────────→ handler sets the flag
                                          main's next back-edge: lyra_live_apply
                                            read the path; dlopen it
                                            call its lyra_live_patch:
                                              initialize new globals
                                              store new addresses in the slots
                                          ←── "applied N\n" (or "failed: <dlerror>\n")
report it, or restart on a failure
```

- The pipe is a pair of descriptors the program inherits (`cmd.ExtraFiles`), named in
  `LYRA_LIVE_FD`. A program not started by `--live` has no such variable, and generation
  0's live pieces stay inert.
- **The signal only sets a flag**; all the work happens at the back-edge, on the program's
  own stack, at a point the compiler chose. Nothing runs in the handler that is not
  async-signal-safe.
- **`lyra_live_patch` is generation N's, not a loader's.** lyrac knows at compile time which
  slots exist and which functions fill them, so the library carries its own patch list as
  code: stores of its functions' addresses into generation 0's (external) slots. The loader
  in generation 0 is three libc calls — `read`, `dlopen`, `dlsym` — emitted as a runtime
  shim like `read_line` (`declareLibc`), with no table format to parse.
- No acknowledgement within a second means the program is not reaching a poll (blocked in a
  `read_line`, or its loop is outside `main`); lyrac restarts it.

### The decision is lyrac's

Whether a generation may be applied is decided **in lyrac, before the program hears of it**,
from two builds it holds in memory — generation 0's and generation N's. The checks are Go,
unit-testable without running anything, and the program's half stays minimal.

| check | compares | on a difference |
|---|---|---|
| `main` | its IR, normalized (below) | restart |
| each type in both builds | its **Lyra shape**: fields' names and types in order; a `data` type's constructors in tag order with their payloads; recursively through generic arguments | restart, naming the type and the change |
| each top-level `let` in both | its type / its initializer | restart / a note |
| each slot | the callee's signature | the slot is left unpatched (only `main` calls it, and `main` is unchanged) |
| each `extern` | present in generation 0 | restart if not (or let `dlopen` find out) |

**The type check is on the Lyra shape, not the LLVM layout**: inserting a constructor in
the middle of a `data` type renumbers the tags without changing the union's size, and a
layout comparison would call that compatible.

**Normalizing `main`'s IR**: the backend numbers some globals program-wide —
`lyra_closure_N` (`closures.go`), `.str.N` (`strings.go`) — so adding a lambda in another
file renumbers `main`'s references and would force a restart. The fix, which belongs in the
backend regardless, is **stable names**: a closure named for its enclosing function and its
index there (`lyra_closure.<symbol>.<n>`), a string literal for its contents' hash. It also
helps the backend tests' binary cache, which keys on emitted IR and is invalidated today by
an unrelated lambda.

## What stays stale, by design

- **`main`'s body** — it is never replaced; changing it restarts.
- **Closures created before the reload**, and **`Seq` values** mid-iteration — they finish
  in the code that made them. Both are old code, still loaded, so this is stale, never
  unsafe.
- **C callbacks registered before the reload** (a function pointer handed to C) — the same.
- **A global's value** when its initializer changes — it is state.

## Pieces

| where | what |
|---|---|
| `cmd/lyrac/live.go` | `--live`; builds generation 0 and generation N; the checks above; the pipe and the signal; falling back to `watch.go`'s restart |
| `pkg/backend/llvm/live.go` | `LiveOptions`; the slot rewrite of `@main`; the back-edge polls; `lyra_live_apply` (gen 0) and `lyra_live_patch` (gen N); globals made external in gen N; thunks through slots |
| `pkg/backend/llvm` | stable closure and string-literal names (a prerequisite, useful alone) |
| `pkg/driver` or `cmd/lyrac` | the Lyra-shape fingerprint of each type, from the `SymbolTable` |
| `cmd/lyrac/main.go` | the link lines: gen 0 with an export list and whole archives; gen N `-shared -fPIC`, `-undefined dynamic_lookup`, `-Bsymbolic-functions` |

## Order of work

1. **Stable names** for closures and string literals. Standalone; checked by the backend
   suite and by a test that adding a lambda elsewhere leaves `main`'s IR unchanged.
2. **The compatibility check** in Go, over two `driver.Result`s and two modules, with unit
   tests for every row of its table. Usable before anything is loaded: `--live` could
   print what *would* apply while still restarting.
3. **Generation 0 and N and the protocol**, macOS first: slots in `main`, polls, the
   apply/patch pair, the link lines. A behavioural test: a loop in `main` printing a
   counter and `label()`; edit `label`; the counter carries on (state kept) and the label
   changes (code swapped); then change a type and see a restart.
4. **Function values through slots**, and new globals' initializers.
5. **Linux**, through `./asan.sh` — interposition, `--dynamic-list` and clang-15 are where it
   differs — and an ASan run across a reload: memory allocated by one generation is
   released by another's code.
6. **Vega**: `lyrac run --live src/vega.lyra` with an editor panel being changed. This is
   the forcing function, as lyrafmt is for the compiler.

## Alternatives considered

- **Every call through a slot.** Simpler — no "which frames stay" reasoning — but every call
  in the program becomes a load and an opaque call, and nothing inlines. Only `main`'s frame
  persists, so only `main` pays.
- **Patching machine code** (a jump written over the old function's first instructions, as
  Live++ does). Works with any stack shape, but needs writable code pages, which macOS on
  arm64 forbids outside a JIT entitlement.
- **A declared host/game split** (the program exports `update(state)` and a host owns the
  loop, as Handmade Hero does). Explicit and well proven, but every program would be
  restructured for it; the compiler already knows where the boundary is.
- **Saving the state and restarting.** Needs every value serialized, and a window, a GPU
  device or an ImGui context cannot be.
- **An in-process JIT** (LLVM ORC). A large dependency for lyrac, which shells out to clang
  today, and the same compatibility questions remain.

## Open questions

1. **`--live` as its own flag, or what `--watch` does on the host?** Proposed: a flag while
   it is new; once Vega has lived on it, `--watch` tries live first and restarts when it
   must.
2. **A frame loop outside `main`.** Proposed: say so and restart (above). The alternative is
   a `live_point()` in `std` that a program calls in its loop, which makes that function a
   persistent frame — polled, and calling through slots — at the cost of a language-visible
   API for a development tool.
3. **Added fields.** Restarting on a changed type is the safe first answer, and the one most
   often hit in game code. Two refinements, both later: (a) restart only when the type is
   reachable from what persists — `main`'s locals and the globals, through fields, payloads,
   arrays and generic arguments (a closure's environment and a raw pointer stop the walk);
   (b) migrate the values, matching fields by name and filling new ones from their defaults —
   which needs every live value found and rewritten, a heap walk Lyra's runtime has no means
   for today.

## Later

- **The Genesis, through Sheliak**: build the new ROM, and have the emulator swap it in while
  keeping RAM and the VDP's state, when the new ROM's RAM layout (`.data`/`.bss`) matches
  the old one. The same idea at the level of a whole machine.
- **Windows**: no `SIGUSR1`; a thread blocking on the pipe would set the flag instead. The
  host ABI is already `Unknown` there (`pkg/abi`).
