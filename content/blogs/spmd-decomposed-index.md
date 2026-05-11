+++
date = '2026-04-15T10:05:00-07:00'
draft = true
title = 'Byte Iteration at 32 Lanes: The Decomposed Index Path'
description = 'How to iterate a []byte on AVX2 without drowning in index-register pressure'
featured_image = 'images/mountain-8.jpg'
featured_image_class = 'cover bg-center'
tags = ['SPMD', 'compiler', 'AVX2', 'x86']
+++

When we set out to make `for i, b := range byteSlice` fast on AVX2, the first thing that went wrong was the index vector. This article explains what happened, the technique we used to fix it, and the chain of bugs the fix resolved along the way.

<!--more-->

## The problem: 256 bytes of index

On AVX2, a 256-bit register holds 32 bytes. If you iterate a `[]byte` at byte granularity, that is 32 lanes of work per iteration -- four times the parallelism of `int32` lanes at the same register width. Byte-granular iteration is exactly what you want for hex encoding, base64 decoding, and byte-level parsers.

But byte-granular iteration over a `[]byte` requires computing 32 memory addresses per iteration. The naive compilation materializes a vector of 32 `i64` indices:

```
<32 x i64> = [base, base+1, base+2, ..., base+31]
```

That is **256 bytes of register state just for the index**, before you have loaded any actual data. On AVX-512 with 64 byte-lanes, it doubles to 512 bytes. LLVM's GEP vectorization path struggles with this: it scalarizes the gather into 32 individual loads, and the "vectorized" loop is slower than the scalar one. Solving this problem is critical to make SPMD and gopher loops useful in Go.

## The technique: scalar base + constant lane offset

The key observation is that in a `for i, b := range slice` loop, every lane's index differs from the next by exactly 1. The index vector is always `[base, base+1, base+2, ..., base+N-1]` for some scalar `base`. We can factor that into two values:

1. A **scalar base pointer**, living in a general-purpose register, incremented by `laneCount` at the bottom of each iteration.
2. A **constant `<N x i8>` lane offset** -- the vector `[0, 1, 2, ..., N-1]` -- loaded once and kept alive for the life of the loop.

Every memory operation combines the two at the GEP site. In the compiler, the pair is tracked as a small struct:

```go
// tinygo/compiler/spmd.go:922-926
type spmdDecomposedIndex struct {
    scalarBase    llvm.Value // Scalar i32 component (uniform across lanes)
    varyingOffset llvm.Value // <N x i8> component (varying per lane)
    laneCount     int        // Number of lanes (e.g., 16 for byte)
    loop          *spmdActiveLoop
}
```

The base is i32 on WASM (where all pointers are 32-bit) and i64 on x86-64, truncated to i32 before combining when the addressing mode allows. Since GEP displacement fields on x86-64 are typically 32-bit, this truncation is free.

The offset vector is 32 bytes on AVX2 -- one register, loaded once. The scalar base is one GPR. Total index cost: one vector register plus one scalar register, regardless of lane count.

## Why this enables "as many lanes as bytes"

The constraint that made byte iteration expensive was the index vector's width scaling with `laneCount * pointer_size`. The decomposed path breaks that coupling. The offset is `laneCount * 1 byte` -- it scales with the SIMD register width, which is exactly what you have room for. The base is a scalar constant cost.

This means `[]byte` iteration on AVX2 uses 32 lanes, on SSE uses 16 lanes, and on AVX-512 would use 64 lanes, all without index-register pressure. The lane count matches the register width exactly, which is the theoretical optimum for byte processing.

At 4 lanes or fewer, a `<4 x i32>` index vector is 16 bytes -- trivially cheap -- and it enable efficient/usable loop on small type. Above 4, the decomposed path is strictly better. It originally had a `spmdIsWASM()` gate because we developed it for WASM first, but we removed that restriction on 2026-03-31. The technique is correct and profitable on every target.

## Power-of-2 modulo: a clean bonus

When the loop index is a decomposed `<N x i8>` offset, modulo by a power of 2 becomes a single bitmask operation on the offset vector:

```
i % 16  -->  lane_offset & 0x0F    (one vpand with a splatted constant)
```

The compiler handles this algebraically in `spmdDecomposedBinOp`: for `AND`, `REM`, or `QUO` with a power-of-2 constant, it operates on the `i8` offset directly without materializing the full index. The same optimization would work on a full `<N x i64>` index vector, but on the decomposed path it operates on 8-bit values -- smaller registers, less throughput consumed, and it composes naturally with subsequent byte-width operations.

In general all the math you actually need operate cleanly and neatly on this decomposed index for all practical purpose. Of course, you can still have to fallback to the slow path if an operation on the index can not be decomposed itself. This is something to later decide, allow all operation and let a linter catch bad practice or make the index a bit of a special type that restrict operation to only what can be decomposed.

## Build this from day one

The decomposed index path is not an optimization you bolt on after the fact. It is a representation choice that determines whether byte-granular iteration is tractable at wide SIMD widths. Without it, you either pay the gather/scatter cost (32 individual loads per iteration on AVX2) or you drop to narrower loops and leave parallelism on the table.

The implementation is roughly 300 lines of Go in the TinyGo compiler. The conceptual move is what matters: recognize that range-over-slice admits a clean factorization into a scalar base and a constant offset, make the compiler track that pair explicitly, and propagate it algebraically through arithmetic operations. If you are building an SPMD compiler or an auto-vectorizing frontend and you want byte-level iteration to be fast, start here.

---

*This article is part of a series on the SPMD-for-Go proof of concept. For the overall pitch, see [Data Parallelism: simpler solution for Golang?](../go-data-parallelism/). For how pattern detection replaced hand-written SIMD primitives, see the pattern matching article (forthcoming).*
