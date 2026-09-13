# SIMD / vectorization — roadmap

**Status: direction, not scheduled.** Nothing here is implemented.

## Why explicit vectors

Lyra promises deterministic results (`det`, `fixed<>`). **Auto-vectorization is
disqualifying**: FP auto-vectorization reassociates adds, contracts FMAs and reorders
reductions, so results change with optimizer version and flags. An explicit `<N x T>`
`fadd` (no fast-math) is bit-identical to N scalar adds, and LLVM legalizes wide vectors
by splitting, so the result is target-independent.

## Decided direction

- **A first-class fixed-width vector type** `simd<T, N>` (syntax TBD): a register value,
  `T` primitive (incl. `fixed<>`), `N` a small power of two, lowering to `<N x T>`.
- **Distinct from `[N]T`.** An array is addressable storage; a vector is a value. Do not
  make `[4]f32` secretly SIMD.
- **No reliance on auto-vectorization; no target intrinsics as the surface** (if ever,
  behind `unsafe` or a platform module).
- **SoA over AoS** for component storage — a storage-layout call, independent of codegen.

## Two layers

1. **`simd<T, N>` primitive** — elementwise `+ - * /`; comparisons → `<N x i1>` masks;
   `select`, `min`/`max`; splat, extract/insert, shuffle, reductions. `SizeAndAlign`:
   size `N*sizeof(T)`, align = size up to a cap. Math types (`vec4`, `quat`, matrices)
   build on it.
2. **Data-parallel map over component arrays** — the compiler widens a `pure`/`det`
   function applied per element to N per iteration plus a scalar remainder. Built on
   layer 1; the same purity enables job-system scheduling across cores.

## Open decisions

- Surface syntax (`simd<T,N>` / `vec<T,N>` / `@Vector`-style).
- **Reduction order** — horizontal `sum`/`dot` must have a specified shape (lean: fixed
  tree) in the language spec. No fast-math or FMA contraction by default.
- `vec3` as `simd<f32,4>` with a dead lane (leaning yes; decide globally).
- Alignment cap, builtin set, layer-2 surface.
