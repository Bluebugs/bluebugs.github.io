+++
date = '2026-04-15T10:03:00-07:00'
draft = true
title = 'Loop Peeling: Where Most of the Speed Comes From'
description = 'How SSA-level loop peeling enables the all-ones mask fast path that delivers ~2x of SPMD benchmark wins'
featured_image = 'images/lakelouise.jpg'
featured_image_class = 'cover bg-center'
+++

If you took every optimization in our SPMD-for-Go proof of concept and ranked them by benchmark impact, loop peeling would be at the top. Not pattern detection. Not contiguous access analysis. Not the decomposed index path. Peeling. It is the structural foundation that everything else is built on, and the reason our hot loops run at one memory operation per store instead of three.

<!--more-->

## The structural split

Every `go for` loop in the PoC gets split into two phases by a function called `peelSPMDLoops` in `x-tools-spmd/go/ssa/spmd_peel.go`. The transform creates four blocks:

- **Main body.** Executes `floor(N / laneCount)` iterations. The mask is statically all-ones -- every lane is active, every iteration.
- **Tail check.** A single branch: are there leftover elements?
- **Tail body.** Executes at most once. The mask is a runtime-computed bitmap that selects only the remaining lanes.
- **Trampoline.** Routes phi values -- accumulators, loop-carried varying values -- from main to tail to done.

Consider a loop over 1000 elements with a lane count of 4. The main body runs 250 iterations, each processing 4 elements with a full mask. If the count were 1003, the main body would still run 250 iterations, and the tail would run once with a 3-lane mask. The common case is fast; the edge case is correct.

The aligned bound is computed with a mask-and:

```
alignedBound = bound & ^(laneCount - 1)
```

The main body increments by `laneCount` per iteration instead of by 1. The tail, if it runs at all, executes a single iteration with a partial mask. That is the entire structure.

## Why this has to happen at the SSA level

A reasonable question: why not let LLVM handle this? LLVM has a loop unroller and a vectorizer. Both do forms of peeling. Neither can help here, for three reasons.

First, LLVM's loop unroller operates on scalar IR. It does not know about masks. If you unroll a masked loop N times, each unrolled iteration still carries the mask it started with. There is no mechanism inside the unroller to collapse "iterations 1 through N-1 have all-ones masks, iteration N has a computed tail mask."

Second, LLVM's auto-vectorizer works on unannotated scalar loops -- loops that do not already know they are going to be vectorized. By the time we hand the IR to LLVM, the loop is already in explicit vector form with masked intrinsics. The vectorizer sees it as "a loop with opaque intrinsic calls" and leaves it alone.

Third, runtime tail masks are hard for LLVM to materialize correctly. The tail mask depends on `bound % laneCount`, which requires knowledge of the SPMD loop's bound value and lane count -- metadata that lives in our SSA representation but has no equivalent in LLVM IR. We always got better generated code by building the tail mask structurally at the SSA level than by trying to coax LLVM into computing the right shape.

At the SSA level, peeling is a straightforward block-creation transform: create four new blocks, rewire phis through them, replace the original loop header with a branch to the main body. The TinyGo backend consumes the peeled SSA without needing to know it was peeled. It just compiles each block in isolation.

## The all-ones fast path

This is where the money is.

Without peeling, every `SPMDStore` in the loop must do three memory operations:

1. **Load** the existing vector at the store address.
2. **Blend** in the new values at mask-active positions (a select or bitwise OR).
3. **Store** the blended vector back.

Three memory operations for one logical store. On WASM, we managed to find a trick. Always take 16bytes out of the end of the memory heap to guarantee that all read or store are safe. On x86, a `vpblendvb` is cheaper than branches but still requires the load.

With peeling, the main body's mask is statically known to be `ConstAllOnes`. The backend checks this at `spmdFullStoreWithBlend` (around line 4644 of `tinygo/compiler/spmd.go`):

```go
// Fast path: all-ones mask -- direct store, no blend needed.
if b.spmdIsConstAllOnesMask(mask) {
    elemAlign := int(b.targetData.TypeAllocSize(val.Type().ElementType()))
    st := b.CreateStore(val, ci.scalarPtr)
    st.SetAlignment(elemAlign)
    return
}
```

One store instruction. No load. No blend. One memory operation instead of three.

Every hot loop in every example -- hex-encode, base64, mandelbrot, lo-min, lo-max, lo-sum -- spends roughly 90% of its iterations in the peeled main body. Every `SPMDStore` in that main body takes this fast path. For a store-heavy loop like hex-encode or base64, this alone accounts for approximately a 2x win. The tail body still pays the full load-blend-store cost, but it executes at most once, so its cost is amortized across the entire loop.

The same logic applies to loads. An `SPMDLoad` in the main body becomes a direct vector load -- no masking, no per-lane gather. In the tail, the load uses a mask to avoid reading past the end of the slice. The main body gets speed; the tail gets correctness.

## Accumulator phi trampolining

There is one structural subtlety that is easy to get wrong. A loop with a varying accumulator -- a running sum, a per-lane minimum, a break result -- has a phi at the loop header that combines the initial value (from outside the loop) with the updated value from the back-edge. After peeling, this phi needs to work across three phases:

1. **Entry to main body.** The initial value (say, zero) feeds main's phi.
2. **Main body to tail body.** Main's final value becomes tail's initial value.
3. **Tail body to done block.** Tail's final value is the loop result.

The done block therefore needs a phi that selects between "main's final value, if there was no tail" and "tail's final value, if there was." We call this the **done-block phi trampoline**. It is built inside `peelSPMDLoop` and referenced by the predication pass via `loop.MainIterPhi` and `loop.TailIterPhi` on `SPMDLoopInfo`.

This is the kind of plumbing that is invisible when it works and catastrophic when it does not. We fixed a trampoline bug during the pointer-varying work and it cost us a full day of debugging. If you implement peeling, test accumulator phis exhaustively -- they are one of the two bug dens (the other being break results under varying conditions).

## Scalar fallback: skip peeling when it does not help

The PoC supports a scalar fallback mode (`-simd=false`) where the lane count is 1. In scalar mode, every iteration processes one element. There is no main/tail distinction because there is no vectorization. Peeling would create four blocks that behave identically to the original single block -- pure overhead.

So we skip it:

```go
func peelSPMDLoops(fn *Function) {
    for _, loop := range fn.SPMDLoops {
        if loop.LaneCount <= 1 {
            continue // scalar fallback -- peeling is a no-op
        }
        peelSPMDLoop(fn, loop)
    }
}
```

A scalar build has zero overhead from the peeling transform. This matters because scalar mode is our correctness oracle: we compile every example in both SIMD and scalar mode and diff the outputs. If peeling added cost to the scalar path, we would be benchmarking our testing infrastructure, not our optimization.

## If you implement one optimization, implement peeling

The SPMD-for-Go PoC has dozens of optimizations: contiguous access analysis, pattern detection for `vpmaddubsw`, byte-decomposition stores, decomposed index paths, store merging, LICM. They all matter. But they all build on top of the structural split that peeling provides. Without peeling, the all-ones fast path does not exist, and every store in the hot loop pays 3x. With peeling, the common case is fast by construction and every other optimization compounds on that foundation.

If you are building an SPMD compiler and you can only ship one optimization, ship peeling.

---

**Further reading:** [SPMD for Go: What If Your Loops Were 9x Faster?](../spmd-results/) for the motivation and benchmark numbers. [How SPMD Lives in the Compiler](../spmd-compiler-internals/) for the full SSA architecture that peeling plugs into.
