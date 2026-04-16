+++
date = '2026-04-15T10:00:00-07:00'
draft = true
title = 'SPMD for Go: What If Your Loops Were 9x Faster?'
description = 'A proof of concept for language-level data parallelism in Go, with live WASM demos and real benchmark results'
featured_image = 'images/banff.jpg'
featured_image_class = 'cover bg-center'
+++

We wrote a base64 decoder in 40 lines of Go. It runs at roughly 17 GB/s on AVX2 -- about 9x faster than `encoding/base64` and within 77% of the best C++ SIMD library ([simdutf](https://github.com/simdutf/simdutf)). No assembly. No intrinsics. No `unsafe`. Just Go with a new loop keyword. Below are two live demos running real WebAssembly code in your browser.

<!--more-->

## See it: Mandelbrot

Two compilations of the same Go source -- scalar on the left, SPMD on the right. Both come from [`examples/mandelbrot/main.go`](https://github.com/Bluebugs/SPMD/blob/main/examples/mandelbrot/main.go). The only difference is the compiler flag: `-simd=false` vs `-simd=true`. Click "Run Benchmark" to see the gap.

The SPMD version uses `go for` to process multiple pixels per iteration and `lanes.Varying[float32]` for the complex-plane coordinates. The compiler handles the rest: vectorized arithmetic, per-lane break when a point diverges, and a masked tail for the last few pixels in each row.

{{< spmd-mandelbrot >}}

Here is the core of the SPMD kernel -- the part that runs per pixel:

```go
// file: examples/mandelbrot/main.go
func mandelSPMD(cRe, cIm lanes.Varying[float32], maxIter int) lanes.Varying[int] {
    var zRe lanes.Varying[float32] = cRe
    var zIm lanes.Varying[float32] = cIm
    var iterations lanes.Varying[int] = maxIter

    for iter := range maxIter {
        magSquared := zRe*zRe + zIm*zIm
        diverged := magSquared > 4.0

        if diverged {
            iterations = iter
            break  // per-lane: only diverged lanes exit
        }

        newRe := zRe*zRe - zIm*zIm
        newIm := 2.0 * zRe * zIm
        zRe = cRe + newRe
        zIm = cIm + newIm
    }
    return iterations
}
```

That `break` inside a varying `if` is the interesting part. Each lane breaks independently -- when a pixel diverges, its lane goes inactive while the others keep iterating. The compiler turns this into per-lane mask tracking: no branches, just predicated execution.

## See it: Base64

The base64 decoder is four `go for` loops with plain Go arithmetic. No SIMD intrinsics. No cross-lane shuffle operations. The compiler recognizes the multiply-add patterns and emits the right SIMD instructions for every target -- `vpmaddubsw`/`vpmaddwd` on x86, deinterleave-widen-multiply-add on WASM.

{{< spmd-base64 >}}

Here is the kernel:

```go
// file: examples/base64-decoder/main.go
func decodeAndPack(dst, src []byte) int {
    n := len(src)

    // Loop 1: decode ASCII → 6-bit sextets via nibble LUT.
    sextets := make([]byte, n)
    go for i, ch := range src {
        s := ch + decodeLUT[ch>>4]
        if ch == byte('+') { s += 3 }
        sextets[i] = s
    }

    // Loop 2: merge pairs → pmaddubsw pattern.
    halfLen := n / 2
    merged := make([]int16, halfLen)
    go for g := range merged {
        merged[g] = int16(sextets[g*2])*64 + int16(sextets[g*2+1])
    }

    // Loop 3: merge pairs → pmaddwd pattern.
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

The compiler sees `int16(a)*64 + int16(b)` and emits `vpmaddubsw`. It sees the stride-3 byte extraction and emits a `vpshufb`-based byte-decomposition store. Four pattern detectors fire simultaneously on this one function -- all from idiomatic Go that any developer can read.

## The 30-second explanation

Three concepts make SPMD work in Go:

**`go for`** marks a loop for data-parallel execution. The compiler vectorizes it: main body with all lanes active, plus a masked tail for the remainder.

**`lanes.Varying[T]`** is a value that differs across SIMD lanes. Inside a `go for`, the loop variable is automatically varying. Arithmetic on varying values produces varying results. Regular Go variables are uniform -- the same across all lanes.

**`reduce.Add`** (and `reduce.Min`, `reduce.Max`, etc.) collapse a varying value back to a scalar.

Here is the simplest useful example -- summing a slice:

```go
// file: examples/lo/sum/main.go
func sumSPMD(data []int32) int32 {
    var total lanes.Varying[int32] = 0
    go for _, v := range data {
        total += v
    }
    return reduce.Add(total)
}
```

You write a loop. The compiler vectorizes it. The type system tracks what is varying. The mask handles the tail. That is the entire mental model.

## Benchmark results

Real numbers from our test infrastructure, measured on an AMD Ryzen 7 6800U:

| Workload | Target | Speedup |
|---|---|---|
| Base64 decode | AVX2 | ~17 GB/s (~9x stdlib, ~77% simdutf C++) |
| Base64 decode | SSSE3 | ~8.5 GB/s |
| lo-min / lo-max | AVX2 8-wide | 7.27x / 7.18x |
| Mandelbrot | AVX2 | 6.07x |
| Mandelbrot | WASM | 3.03x |
| Hex-encode | WASM | 8.9x |
| Hex-encode | SSE | 6.31x |

Honest disclosure: not everything speeds up. Our IPv4 parser hit 0.58x with inner-SPMD -- actually *slower* than scalar. The input (7-15 bytes per address) was too small to amortize SIMD setup costs. The fix is outer-SPMD batching: vectorize *across* IP addresses instead of within one. The lesson is that SPMD shines on tight loops over contiguous memory, not on short, variable-length inputs processed one at a time.

## Why this belongs in the compiler

SPMD is not a library feature. It is a compiler feature. The core transforms -- predication (linearizing varying `if`/`else` into masked selects), loop peeling (splitting into an all-ones main body and a masked tail), and pattern detection (recognizing multiply-add, contiguous access, byte-decomposition stores) -- are SSA-level transformations that live at the heart of the compiler.

We learned this the hard way. Our first approach tried to bolt SPMD onto the TinyGo backend as an analysis pass, reconstructing masks from control-flow structure without touching the SSA representation. It worked for simple cases. Then varying switch, compound boolean chains, per-lane break, and inner scalar loops each demanded new special cases. Every bug was "the mask was wrong on this path." We deleted roughly 330 lines of mask-stack code and accepted what we should have known from the start: the varying-ness of control flow must be encoded in the SSA form itself.

The proof-of-concept adds three SPMD-aware SSA instructions (`SPMDLoad`, `SPMDStore`, `SPMDSelect`) and four metadata structures to `go/ssa`. With that foundation, predication and loop peeling become mechanical transforms, and mask correctness is guaranteed by construction rather than reconstructed by analysis.

**How this relates to `simd/archsimd` (Go 1.26).** The two approaches are complementary, not competing. `archsimd` is instruction-level: you pick `Int32x8`, you call `.Add()`, you get one instruction. It is "SIMD as `syscall`" -- architecture-specific, direct, and exactly right for `crypto` internals or `math/big` where you want a specific machine instruction. SPMD is loop-level: you write `go for`, the compiler picks the width and the instructions. It is "SIMD as `go for`" -- portable, automatic, and right for application code where you want the compiler to do the work.

The Go team has described a planned portable high-level API on top of `archsimd`. That would be a third point in the design space -- something like Google's Highway for C++. Our hypothesis is that a serious Go SIMD story wants all three: `archsimd` underneath for instruction-level control, a portable vector library for typed vector operations, and SPMD on top for loop-level data parallelism. They serve different audiences and different use cases.

For upstream Go, the SSA-level patterns we prototyped in `golang.org/x/tools/go/ssa` -- `SPMDLoopInfo`, explicit mask metadata, predication transforms, loop peeling -- would go into `cmd/compile/internal/ssa`. The 42 Phase-1 opcodes we built early on were the wrong shape (a flat list of vector operations) but pointed at the right location. The structured approach is what should replace them.

## Where SPMD would help in the stdlib

Two categories stand out as natural fits:

**Image processing.** `image/draw`, `image/color` conversions (RGB-to-YCbCr, alpha premultiplication), JPEG/PNG decode pipelines. Per-pixel arithmetic is the golden SPMD case: contiguous memory, uniform control flow, and many related kernels that share the same shape. A properly vectorized `draw.Draw` becomes one vector load and one vector store per lane-count pixels in the common path. The `image` family has many such kernels, and writing each one with instruction-level SIMD would mean a permanent per-architecture maintenance burden. SPMD compiles them all from one source.

**Byte parsing.** HTTP header scanning (`net/http`), JSON structural character detection, `go/scanner` tokenization, `encoding/hex`, `encoding/base64`. The proof-of-concept's hex-encode and base64 examples are proofs of concept for this whole category. Anything that is "scan a `[]byte`, classify each byte, act on the classification" fits the same pattern: byte-lane iteration, small-table lookup via `vpshufb`, and masked stores. These hot paths are well-tested, self-contained, and performance-visible in real services -- good properties for introducing a new compiler technology.

## Where to go from here

The proof of concept is open source. The full implementation spans three repositories: a Go fork (type system and SSA), a TinyGo fork (LLVM backend for WASM and x86), and a patched `golang.org/x/tools` (SSA-level predication and loop peeling). We have 90 end-to-end tests passing across WASM, SSE, and AVX2.

We would welcome feedback from the Go community -- whether you are a developer who would use this, a compiler engineer who sees how to do it better, or someone who spots a flaw in the design. The interesting conversation is not "should Go have SIMD?" (it should, and `archsimd` is already here) but rather "should Go have *loop-level* data parallelism, and if so, what should it look like?"

If you want to dig deeper, the rest of this series covers the details:

- [Writing SPMD Go: A Practical Guide](../writing-spmd-go/) -- the mental model, idioms, and worked examples
- [How SPMD Lives in the Compiler](../spmd-compiler-internals/) -- the mask-stack lesson, predicated SSA, and what we would do differently
- [Pattern Matching Beats Hand-Written SIMD](../spmd-pattern-matching/) -- why the simplest code produced the fastest output
- [Loop Peeling: Where Most of the Speed Comes From](../spmd-loop-peeling/) -- the single highest-leverage optimization
- [We Built Cross-Lane SIMD Primitives. None of Them Helped.](../spmd-cross-lane-negative-result/) -- the most important negative result

---
