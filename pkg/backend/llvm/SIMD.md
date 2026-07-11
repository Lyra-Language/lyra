# SIMD / vectorization — roadmap

**Status: direction, not scheduled.** This records the design decision to give Lyra
explicit SIMD via first-class vector types, and the two-layer plan. It is
deliberately sequenced **after the scalar backend works** (types, arithmetic,
control flow, functions) — nothing here blocks or complicates the first lowering.
Companion to ALLOCATION.md / DATA_LAYOUT.md, but those are settled specs; this is
a roadmap with parts still open.

## Why explicit vectors (the determinism argument)

There are three ways to get SIMD out of LLVM: (1) auto-vectorization of scalar
loops, (2) first-class vector types (`<N x T>` + elementwise ops), (3) target
intrinsics (non-portable). For a language promising deterministic
netcode/physics/replay (`det`, `fixed<>`), **auto-vectorization is disqualifying**:
FP auto-vectorization reassociates adds, contracts FMAs, and reorders reductions,
so results differ from the scalar version and change with optimizer version/flags —
non-determinism hidden in the optimizer.

Explicit vector types flip this: a `<4 x f32> fadd` (without fast-math) is
bit-identical to four scalar adds, and because LLVM legalizes wide vectors by
splitting, `<8 x f32>` means the same thing regardless of hardware width — **result
independent of the target**, which is exactly the determinism property we want. So
explicit SIMD *serves* determinism rather than fighting it, and it matches "expensive
paths are loud" (SIMD is visible and greppable, not an invisible optimizer decision).

## Decided direction

- **An explicit, first-class fixed-width vector type** `simd<T, N>` (surface syntax
  TBD): a register value, `T` a primitive (incl. `fixed<>`), `N` a small power of two,
  lowering to `<N x T>`.
- **Distinct from `[N]T` arrays.** An array is addressable memory; a vector is a value
  in registers (cf. Zig's `[N]T` vs `@Vector(N,T)`). `StaticArrayType` stays storage;
  `simd<>` is the value type. Do **not** make `[4]f32` secretly SIMD.
- **No reliance on auto-vectorization; no target intrinsics as the surface** (portable
  vector ops only; intrinsics, if ever, behind `unsafe`/a platform module).
- **SoA over AoS for component storage.** Bulk SIMD wants struct-of-arrays (each field
  contiguous → cheap vector loads); array-of-structs needs gather/scatter. This is a
  storage-layout decision that can be made now, independent of codegen.

## Two layers

### Layer 1 — the `simd<T, N>` primitive (foundation; earlier)

A clean additive feature once scalar codegen exists. Provides:
- elementwise `+ - * /`; comparisons → **mask vectors** `<N x i1>`; `select(mask,a,b)`; `min`/`max`;
- lane ops: splat/broadcast, extract/insert, shuffle/swizzle, and **reductions**;
- math types built on top: `vec4 = simd<f32,4>`, `quat`, matrices as structs of vectors.

Plugs into `SizeAndAlign` as one more case: size `N*sizeof(T)`, align = size (up to a cap).

### Layer 2 — data-parallel over component arrays (the games win; later, bigger)

`simd<T,N>` becomes the *lowering target*, not the surface: apply a `pure`/`det`
function to each element of a component array, and the compiler widens the body to
process N per iteration with a scalar remainder. The purity guarantee unlocks **both**
axes — vectorization within a core *and* job-system scheduling across cores — from the
same property ("ECS systems as pure functions"). A real pass; built entirely on Layer 1.

## Open decisions

- **Surface syntax** — `simd<T,N>` vs `vec<T,N>` vs a `@Vector`-style form.
- **Reduction order — the one place determinism needs a *specified shape*.** Elementwise
  ops are trivially deterministic, but a horizontal `sum`/`dot` differs left-to-right vs.
  tree-shaped. Pick one (lean: a fixed tree shape) and make it part of the language spec,
  not the optimizer's choice. No fast-math by default; no FMA contraction unless opted in.
- **`vec3` padding** — store as `simd<f32,4>` with a dead 4th lane (better alignment/ops,
  ignore lane 3)? Leaning yes; decide once, globally.
- **Alignment cap, the builtin set** (dot/cross/length/shuffle/…), and the **Layer-2
  parallel-map surface**.

## Sequencing

1. Scalar backend first (currently at `main => 42`).
2. Layer 1 (`simd<T,N>`) — additive, once scalar types/arithmetic/control-flow work.
3. Layer 2 (data-parallel map) — research-y pass on top of Layer 1.

SoA-for-components can be decided at any point (storage-layout call, not codegen).
