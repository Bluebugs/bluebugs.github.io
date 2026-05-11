+++
date = '2026-04-15T10:08:00-07:00'
draft = true
title = 'We Built Cross-Lane SIMD Primitives. None of Them Helped.'
description = 'The most important negative result from our SPMD-for-Go proof of concept: explicit shuffles and rotations lost to compiler pattern detection on idiomatic Go'
featured_image = 'images/mountain-10.jpg'
featured_image_class = 'cover bg-center'
+++

We built six cross-lane SIMD primitives for our Go SPMD proof of concept. We benchmarked them across ten examples. None delivered a measurable win. Every example that shipped fast shipped without them.

<!--more-->

## What we built

The `lanes` package in our PoC included a full suite of cross-lane operations, modeled on what ISPC and Mojo provide:

- **`lanes.Rotate(v, k)`** -- full-width rotation by a compile-time-constant offset. Lowers to LLVM `shufflevector`.
- **`lanes.Swizzle(v, idx)`** -- arbitrary permutation by a runtime per-lane index vector. Lowers to per-lane extract/insert (there is no single-instruction runtime shuffle on most targets).
- **`lanes.RotateWithin(v, k, n)`** -- rotate within each group of `n` lanes.
- **`lanes.ShiftLeftWithin(v, k, n)`** / **`lanes.ShiftRightWithin(v, k, n)`** -- shift within groups.
- **`lanes.SwizzleWithin(v, idx, n)`** -- const-indexed permutation within groups of `n` lanes.

All of these compile. All of them pass correctness tests on WASM SIMD128, x86 SSE, and x86 AVX2, in both SIMD and scalar fallback modes. The implementation is clean -- const-only `shufflevector` lowering for the `*Within` family, AVX2 `vpshufb` table duplication for byte-lane lookups, scalar fallback for runtime `Swizzle`. We are not reporting a failure of implementation. They work. They just do not matter.

## What we measured

We benchmarked every example at multiple points during development. At the end of the project, every example had been rewritten to use zero cross-lane primitives -- or, at most, `Broadcast`, `Count`, and `Index`, which compile to a splat, a compile-time constant, and a constant vector respectively. Essentially free.

The centerpiece evidence is the base64 decoder.

**Version 1** used explicit cross-lane machinery: `lanes.CompactStore` for output compaction and `lanes.Rotate` tricks for packing bytes across lane boundaries. We built `SPMDMux` and `SPMDInterleaveStore` -- two new SSA-level optimizations, roughly 1500 lines of compiler code -- specifically to make these cross-lane patterns fast. v1 peaked at about 2x scalar throughput.

**Version 2** was a rewrite that used zero cross-lane operations. Four `go for` loops with plain Go arithmetic:

```go
func decodeAndPack(dst, src []byte) int {
    n := len(src)

    // Loop 1: decode ASCII -> 6-bit sextets via nibble LUT.
    sextets := make([]byte, n)
    go for i, ch := range src {
        s := ch + decodeLUT[ch>>4]
        if ch == byte('+') { s += 3 }
        sextets[i] = s
    }

    // Loop 2: merge pairs -> pmaddubsw pattern.
    halfLen := n / 2
    merged := make([]int16, halfLen)
    go for g := range merged {
        merged[g] = int16(sextets[g*2])*64 + int16(sextets[g*2+1])
    }

    // Loop 3: merge pairs -> pmaddwd pattern.
    quarterLen := halfLen / 2
    packed := make([]int32, quarterLen)
    go for g := range packed {
        packed[g] = int32(merged[g*2])*4096 + int32(merged[g*2+1])
    }

    // Loop 4: extract 3 bytes per int32.
    go for g := range packed {
        dst[g*3+0] = byte(packed[g] >> 16)
        dst[g*3+1] = byte(packed[g] >> 8)
        dst[g*3+2] = byte(packed[g])
    }

    return quarterLen * 3
}
```

No `SwizzleWithin`. No `RotateWithin`. No `CompactStore`. Just four loops with integer arithmetic. The compiler recognized the `int16(a)*64 + int16(b)` shape and emitted `vpmaddubsw`. It recognized `int32(a)*4096 + int32(b)` and emitted `vpmaddwd`. It recognized the stride-3 byte extraction and emitted `vpshufb`-based byte-decomposition stores. All automatically, from idiomatic Go.

v2 hit ~17 GB/s on AVX2 -- roughly 77% of simdutf (the best C++ SIMD base64 library) and about 9x faster than Go's `encoding/base64`. That is a roughly 5x improvement over v1, achieved by *removing* cross-lane operations.

The rest of the examples tell the same story. Hex-encode: 6-9x on WASM (varies by host), no cross-lane ops. Mandelbrot: 6.07x on AVX2, no cross-lane ops. lo-min: 7.27x on AVX2, no cross-lane ops. IPv4 parser: initially used `lanes.DotProductI8x16Add` as a custom builtin; we deleted it when the `vpmaddubsw` pattern detector subsumed it. Performance was unchanged.

We deleted the 1500 lines of `CompactStore` / `SPMDMux` / `SPMDInterleaveStore` compiler machinery. Nothing got slower.

## Why the wins came from elsewhere

Cross-lane primitives move values *within* a SIMD register. They do not change how much memory is loaded, how many arithmetic operations are performed, or whether the mask overhead can be eliminated. Register rearrangement is cheap on modern hardware. The things that are not cheap -- and where the real benchmark wins come from -- are elsewhere entirely:

**Contiguous access analysis.** When the compiler proves that `a[i]` inside a `go for` is a contiguous access, it emits a single vector load instead of per-lane gather. That is a 4-8x difference per memory operation. The recognizer (`spmdAnalyzeContiguousIndex`) traces through `BinOp ADD` chains and `ChangeType` wrappers to find the iteration phi. Adding one `ChangeType` unwrap case gave a 38% improvement. This is where the leverage is.

**Pattern detection.** The `vpmaddubsw` detector recognizes `int16(a[i*2])*C + int16(a[i*2+1])` and replaces 8+ instructions with 1. The byte-decomposition store detector recognizes stride-S byte extraction and replaces scalar stores with `vpshufb` + vector store. These detectors compound -- in base64 v2, four fire simultaneously. Cross-lane ops bypass all of them because the developer has already lowered the algorithm to specific shuffle instructions.

**Loop peeling.** Splitting the `go for` into a main body (all-ones mask) and a tail (runtime mask) means the main body's stores become direct vector stores -- one memory operation instead of the load-blend-store sequence that masked stores require. Roughly 3x fewer memory ops for the hot path.

**Decomposed index path.** At byte granularity on AVX2 (32 lanes), maintaining a 32-element index vector would consume 256 bytes of register state. The scalar-base + `<N x i8>` constant-offset decomposition makes byte-lane iteration tractable without crushing register pressure.

None of these optimizations involve moving values between lanes. They are all about reducing memory traffic, reducing instruction count, and eliminating masking overhead. Cross-lane primitives contribute to none of them.

They also make the source more opaque to the compiler, which means fewer opportunities for higher-level optimizations to fire.

## The mismatch

ISPC and Mojo ship rich cross-lane vocabularies, and for good reason. Their primary markets -- ray tracing, physics simulation, shader compilation -- are full of small kernels that need within-register rearrangement: butterfly operations in FFTs, neighborhood lookups in stencil computations, AoS-to-SoA transforms in particle systems.

Go's likely SPMD market is different. Encoding (base64, hex, JSON), parsing (HTTP headers, IPv4, CSV), numerical reductions over slices (`min`, `max`, `sum`, `mean`), image processing (per-pixel color math). These are contiguous-memory workloads. Data comes in from a slice, gets transformed element-wise or in small fixed-stride patterns, and goes back out to a slice. The values in lane 0 almost never need to visit lane 3.

This is not a criticism of ISPC or Mojo. They serve their markets well. It is a statement about Go's market: **the cross-lane vocabulary we built was designed for problems Go developers do not typically have.**

## What to ship in v1

Based on this evidence, a real Go SPMD implementation should ship three cross-lane operations:

- **`lanes.Broadcast(value, lane)`** -- free, compiles to a splat. Useful for lane-independent structure.
- **`lanes.Count[T]()`** -- compile-time constant. Essential for chunk sizing (the outer-loop pattern that feeds one register-width batch per inner `go for`).
- **`lanes.Index()`** -- constant vector `[0, 1, 2, ..., N-1]`. Useful for mask construction and position tracking. Better yet, express it as `iota` in a varying context.

That is it. Maybe add compile-time-const `lanes.Rotate` if a specific benchmark demands it -- circular buffer tricks are a candidate. But gate even that on concrete evidence.

Do not ship the `*Within` family. Do not ship runtime-indexed `Swizzle`. Even when some of these lower efficiently in the good cases, they cost real engineering to maintain: type-checker enforcement for const-only arguments, AVX2 table duplication interactions, scalar fallback paths, test matrices. Unused builtins are a tax on every future compiler change.

You can always add more primitives later when a real benchmark demands them. This runs against the "future-proof API surface" instinct, but the evidence says it is the right call: we built the full suite, measured everything, and deleted most of it.

## The lesson

If you are designing a SIMD API -- for Go or anything else -- invest in pattern detectors first, builtins second. The simplest code produced the fastest results, because the compiler's pattern detectors work best on idiomatic patterns. Complexity is not just a readability cost; it is a performance cost.

We wrote this up because negative results deserve documentation. We spent real engineering time on cross-lane primitives, learned they do not help for Go's workloads, and want to save others from the same detour.

For the technical details on which pattern detectors replaced the cross-lane primitives: [Pattern Matching Outperformed Hand-Written SIMD](../spmd-pattern-matching/). For the full benchmark results across all examples and targets: [SPMD for Go: What If Your Loops Were 9x Faster?](../spmd-results/).

---

*This is part of the [SPMD-for-Go blog series](/blogs/) documenting the results of building an SPMD compiler for Go.*
