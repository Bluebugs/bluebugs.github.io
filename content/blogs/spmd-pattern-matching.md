+++
date = '2026-04-15T10:04:00-07:00'
draft = false
title = 'Pattern Matching Outperformed Hand-Written SIMD'
description = 'How compiler pattern detection on idiomatic Go outperformed explicit cross-lane SIMD builtins in our SPMD proof of concept'
featured_image = 'images/mountain-4.jpg'
featured_image_class = 'cover bg-center'
tags = ['SPMD', 'compiler', 'SIMD', 'pattern-detection']
+++

Our base64 decoder was implemented twice. Version 1 used explicit cross-lane operations --- shuffles, rotations, compact stores. It peaked at roughly 2x scalar performance. Version 2 used four plain `go for` loops with no cross-lane operations at all. It hit approximately 17 GB/s on AVX2 --- about 77% of simdutf C++ and 9x faster than Go's `encoding/base64`. The simpler code outperformed the clever code by a wide margin.

<!--more-->

This is the story of how we learned to trust the compiler more than our SIMD intuition, and why we think pattern detection should come before builtins in any SIMD API design.

## The base64 story

The [original blog post](../cross-lane-communication/) in this series explored base64 decoding as a cross-lane communication problem, drawing on Miguel Young de la Sota's excellent ["Designing a SIMD Algorithm from Scratch"](https://mcyoung.xyz/2023/11/27/simd-base64/). The approach was top-down: start from the SIMD algorithm, translate it into Go syntax with explicit cross-lane operations. It looked something like this (condensed):

```go
// v1: explicit cross-lane approach
offsetTable := []byte{255, 16, 19, 4, 191, 191, 185, 185}
offsets := lanes.SwizzleWithin(lanes.From(offsetTable), hashes, 8)
sextets := ascii + offsets

shiftPattern := lanes.From([]uint16{2, 4, 6, 8})
shifted := lanes.ShiftLeftWithin(sextets, shiftPattern, 4)
decodedChunks := shiftedLo | lanes.RotateWithin(shiftedHi, 1, 4)
output := lanes.SwizzleWithin(decodedChunks, pattern, 4)
```

The developer is thinking about shuffles. They know which data lives in which lane, they know where it needs to go, and they tell the compiler exactly how to move it. This is how you write an intrinsics library --- you know the target instructions and you express them in your source language.

We built all of this. `SwizzleWithin`, `RotateWithin`, `ShiftLeftWithin`, `CompactStore` --- a full cross-lane vocabulary. It compiled, it passed tests, and it produced correct base64 output. Then we benchmarked it and it hit roughly 2x scalar.

So we tried a completely different approach. Instead of translating a SIMD algorithm into Go, we wrote the algorithm in the most natural Go we could and let the compiler figure out the instructions. This second version was inspired by Mula and Lemire's base64 technique, expressed as four cascading `go for` loops:

```go
// v2: four plain go for loops (from examples/base64-decoder/main.go:41)
func decodeAndPack(dst, src []byte) int {
    n := len(src)

    // Loop 1: decode ASCII to 6-bit sextets via nibble LUT.
    sextets := make([]byte, n)
    go for i, ch := range src {
        s := ch + decodeLUT[ch>>4]
        if ch == byte('+') { s += 3 }
        sextets[i] = s
    }

    // Loop 2: merge adjacent pairs.
    halfLen := n / 2
    merged := make([]int16, halfLen)
    go for g := range merged {
        merged[g] = int16(sextets[g*2])*64 + int16(sextets[g*2+1])
    }

    // Loop 3: merge int16 pairs.
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

No `SwizzleWithin`. No `RotateWithin`. No `ShiftLeftWithin`. No output pattern. No explicit cross-lane anything. Just four `go for` loops with plain Go arithmetic. And it hit 77% of the best C++ SIMD library. There is an obvious ceiling here: TinyGo optimizes for size, so we are not getting aggressive loop unrolling, and simdutf's AVX2 decoder processes 64 bytes per iteration where ours processes 32. So 77% is not the end of the road so much as the current result under a size-oriented compiler.

The original blog post even asked the right question: "Is the added complexity worth it? Perhaps the real question is whether we need the full suite of cross-lane operations, or if reduction alone would cover the majority of practical use cases." The answer turned out to be more dramatic than we expected.

## The vpmaddubsw/vpmaddwd detector

Look at loop 2 again:

```go
// from examples/base64-decoder/main.go:59
go for g := range merged {
    merged[g] = int16(sextets[g*2])*64 + int16(sextets[g*2+1])
}
```

This is a widening multiply-add: take two adjacent bytes, widen to `int16`, multiply each by a constant, and add the results. On x86, there is a single instruction that does exactly this across an entire vector: `vpmaddubsw` (Packed Multiply and Add Unsigned and Signed Bytes to Words). It takes a vector of unsigned bytes and a vector of signed byte multipliers, multiplies adjacent pairs, and adds the products into 16-bit results.

The compiler's pattern detector (`spmdTryEmitPmadd` in `tinygo/compiler/spmd.go`) recognizes the `int16(a[i*2]) * C + int16(a[i*2+1])` shape and emits `vpmaddubsw` directly. The same pattern at the next width level --- `int32(a[i*2]) * C + int32(a[i*2+1])` on `int16` inputs --- emits `vpmaddwd`.

There is a subtlety: `vpmaddubsw` takes signed-byte multipliers, so weights above 127 do not fit. The constant 4096 in loop 3 obviously exceeds this. The detector handles it through constant decomposition --- splitting large weights into partial products that each fit in a signed byte, emitting two partial `vpmaddubsw` instructions combined with adds. The developer writes `*4096`; the compiler figures out the decomposition.

On WASM, where there is no `pmaddubsw` instruction, the detector falls back to a deinterleave + widen + multiply + add sequence. The developer's code does not change. The same four `go for` loops produce target-optimal instructions on every platform.

The result: the base64 hot loop went from **14.3 instructions per byte** (the v1 scatter-gather approach) to **0.44 instructions per byte** with the detector --- a 32x instruction reduction. That is what gets AVX2 performance to 77% of simdutf C++.

## Byte-decomposition store

Now look at loop 4:

```go
// from examples/base64-decoder/main.go:79
go for g := range packed {
    dst[g*3+0] = byte(packed[g] >> 16)
    dst[g*3+1] = byte(packed[g] >> 8)
    dst[g*3+2] = byte(packed[g])
}
```

Three byte stores at stride-3 positions, each extracting a different byte from a 32-bit value. Without pattern detection, this is a disaster: three separate masked byte stores per iteration, scalarized to one `mov` per lane per position. On AVX2 with 8 lanes, that is 24 individual byte stores per iteration.

The compiler's byte-decomposition store detector recognizes the stride-S pattern and emits three operations:

1. A **bitcast** of the `int32` vector to a flat byte vector (`<N*4 x i8>`).
2. A **`vpshufb`** with a compile-time constant permutation table that places the extracted bytes at the correct stride-3 positions.
3. A **single contiguous masked store** of the result.

Three operations, independent of the number of lanes. On WASM, `v128.swizzle` does the same job. On SSE, `pshufb`. The detection pass is roughly 300 lines of compiler code.

Here is the part that still amazes me: the byte-decomposition store detector replaced approximately 1,500 lines of `CompactStore`, `SPMDMux`, and `SPMDInterleaveStore` infrastructure we had built for the v1 decoder. We added those three features --- explicit cross-lane builtins with SSA-level optimization passes, diagonal-extraction shuffles, and per-lane selection logic. Four days later, we deleted all of it. One recognizer, 300 lines, did the same job better.

If this optimization were to be carried forward into a production compiler, covering stride-2 through stride-4 cases first would likely capture most of the practical wins. Hex encoding, base64, and a lot of image-manipulation code all fall into that range.

## Why simpler code wins

After living with both approaches for several months, we see four reasons the pattern-detection approach outperforms the explicit-builtin approach.

**The compiler sees more than the developer.** The `vpmaddubsw` detector handles any constant-coefficient widening multiply-add, not just base64. It automatically decomposes weights above 127. It falls back to a correct (and still fast) sequence on WASM. It will handle future patterns that developers write without knowing the detector exists. A builtin, by contrast, handles exactly the cases the API designer anticipated.

**Pattern detection compounds.** The base64 v2 decoder benefits from four pattern detectors firing simultaneously in a single function: the pmadd detector on loops 2 and 3, the byte-decomposition store on loop 4, contiguous access analysis on every load and store, and loop peeling with the all-ones mask fast path on the main body. Each detector is independent, but their effects multiply. The explicit-cross-lane approach from v1 bypasses all of them, because the developer has already lowered the algorithm to specific operations --- there are no patterns left for the compiler to find.

**Cross-lane operations have hidden costs.** `SwizzleWithin` with a variable index compiles to per-lane extract/insert --- one scalar operation per lane, because there is no hardware instruction for a runtime-variable swizzle. `RotateWithin` needs a compile-time constant offset to emit a `shufflevector`; if the compiler cannot prove the offset is constant, it falls back to per-lane operations too. The v1 blog post assumed these would be free. They are not.

**The developer writes less code, and that code is easier to review.** The v2 base64 kernel is roughly 40 lines. A full implementation of the v1 approach would be 80 or more lines of explicit shuffles, rotations, and output patterns. Less code means fewer bugs. It also means that a reviewer who understands Go arithmetic can verify correctness without understanding SIMD lane semantics.

The blog's approach treated the compiler as a translator --- "emit the instructions I tell you." The PoC's winning approach treated the compiler as an optimizer --- "here's what I want to compute; you pick the instructions." The optimizer won.

## The DotProductI8x16Add cautionary tale

Before the pmadd detector existed, we needed a widening multiply-add for the IPv4 parser's decimal digit conversion. So we added a builtin: `lanes.DotProductI8x16Add`. It took byte inputs, multiplied by constant weights, and summed the products. It worked. The IPv4 parser used it.

Then we built the pmadd pattern detector for the base64 decoder. The detector recognized the same `int16(a)*C + int16(b)` shape that `DotProductI8x16Add` had been designed for. The IPv4 decimal conversion is just `int16(digit[0])*10 + int16(digit[1])` --- the exact pattern the detector handles.

So we deleted the builtin. One commit: `1df19e8`. Approximately 163 lines of compiler code removed --- type checking, LLVM lowering for three targets, test infrastructure. The IPv4 parser continued to work without changes, because it had never needed the builtin; it just needed the compiler to recognize a multiply-add when it saw one.

The moral is simple: **pattern detectors generalize; builtins do not.** Every builtin is a tax on future compiler changes --- it has to be type-checked, lowered on every target, documented, and maintained in a stable API. A pattern detector is pure compiler internals. It can be improved, generalized, or deleted without affecting user source code. When a detector subsumes a builtin, delete the builtin.

## Closing

If you are designing a SIMD API --- whether for Go, Rust, Mojo, or anything else --- consider investing in pattern detectors first, builtins second. Ship `Broadcast` (it is free --- just a splat). Ship `Count[T]()` (it is a compile-time constant). Ship `Index()` (it is a constant vector). That might be enough.

The evidence from this proof of concept says: four pattern detectors on idiomatic Go delivered 77% of the performance of handwritten C++ SIMD, from source code that any Go developer can read. A full suite of cross-lane builtins delivered 20%. The simpler code, the one that trusts the compiler, was the faster code.

Ship fewer primitives, not more. Let the compiler do the thinking.

---

**Further reading:**

- [We Built Cross-Lane SIMD Primitives. None of Them Helped.](../spmd-negative-result/) --- the full negative result on cross-lane operations.
- [SPMD for Go: What If Your Loops Were 9x Faster?](../spmd-results/) --- benchmark results and live demos.
- [How SPMD Lives in the Compiler](../spmd-compiler-internals/) --- the SSA architecture that makes pattern detection possible.
