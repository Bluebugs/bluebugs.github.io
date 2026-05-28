+++
date = '2026-04-15T10:00:00-07:00'
draft = false
title = 'SPMD for Go: What If Your Loops Were Just Faster?'
description = 'A proof of concept for language-level data parallelism in Go, with live WASM demos and real benchmark results'
featured_image = 'images/mountain-1.jpg'
featured_image_class = 'cover bg-center'
tags = ['golang', 'SPMD', 'SIMD', 'performance', 'benchmarks']
+++

About 40 lines of Go gets you a base64 decoder that runs at ~17 GB/s on AVX2 -- 9x faster than `encoding/base64` and within 77% of the best C++ SIMD library ([simdutf](https://github.com/simdutf/simdutf)). No assembly, no intrinsics, no `unsafe`. Just Go with a new loop keyword.

This is a proof of concept -- not a proposal or an upstream plan. I want to show that loop-level data parallelism can fit Go's style, compile to real SIMD on multiple targets, and deliver meaningful wins on real workloads. Below are two live demos running real WebAssembly code in your browser.

<!--more-->

## See it: Mandelbrot

Two compilations of the same Go source -- scalar on the left, Single Program Multiple Data (SPMD) on the right. Both come from [`examples/mandelbrot/main.go`](https://github.com/Bluebugs/go-spmd/blob/main/examples/mandelbrot/main.go). The only difference is the compiler flag: `-simd=false` vs `-simd=true`. Click "Run Benchmark" to see the gap.

The SPMD version uses `go for` to process multiple pixels per iteration and `lanes.Varying[float32]` for the complex-plane coordinates. The compiler handles the rest: vectorized arithmetic, per-lane break when a point diverges, and a masked tail for the last few pixels in each row.

{{< spmd-mandelbrot >}}

The full SPMD mandelbrot -- calling loop and kernel together:

```go
// file: examples/mandelbrot/main.go

// The driver: a scalar outer loop over rows, an SPMD inner loop over columns.
func mandelbrotSPMD(x0, y0, x1, y1 float32, width, height, maxIter int, output []int) {
    dx := (x1 - x0) / float32(width)
    dy := (y1 - y0) / float32(height)

    for j := 0; j < height; j++ {
        y := y0 + float32(j)*dy

        go for i := range width {
            x := x0 + lanes.Varying[float32](i)*dx
            iterations := mandelSPMD(x, y, maxIter)
            index := j*width + i
            output[index] = iterations
        }
    }
}

// The kernel: runs per pixel, receives a varying x and uniform y.
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

A scalar `for j` walks rows. Inside it, `go for i := range width` is the SPMD loop -- each lane handles a different x coordinate in parallel, all sharing the same y. The kernel `mandelSPMD` takes varying parameters and the compiler generates SIMD instructions from them transparently.

The `break` inside a varying `if` is where it gets interesting. Each lane breaks independently -- when a pixel diverges that lane goes inactive while the others keep going. The compiler turns this into per-lane mask tracking: no branches, just predicated execution.

`go for` is to data parallelism what `go func` is to control flow parallelism.

## See it: Base64

The base64 decoder is four `go for` loops with plain Go arithmetic. No SIMD intrinsics or cross-lane shuffle operations. The compiler recognizes the multiply-add patterns and emits the right SIMD instructions for every target -- `vpmaddubsw`/`vpmaddwd` on x86, deinterleave-widen-multiply-add on WASM.

{{< spmd-base64 >}}

The kernel, with notes on the SIMD instructions a hand-written version would use:

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

The compiler sees `int16(a)*64 + int16(b)` and emits `vpmaddubsw`. It sees the stride-3 byte extraction and emits a `vpshufb`-based byte-decomposition store. Four pattern detectors fire simultaneously on this one function -- all from ordinary Go any developer can read. Once you express data parallelism, the compiler can optimize.

## The 30-second explanation

SPMD in Go comes down to three things:

**`go for`** marks a loop for data-parallel execution. The compiler vectorizes it: a main body with all lanes active, plus a masked tail for the remainder. You pick loops whose iterations are independent -- same judgment call as `go func` for control-flow parallelism, just for data.

**`lanes.Varying[T]`** holds a value that differs across SIMD lanes. Inside a `go for`, the loop variable is automatically varying. Arithmetic on varying values stays varying. Regular Go variables are uniform -- the same on every lane.

**`reduce.Add`** (and `reduce.Min`, `reduce.Max`, etc.) collapses varying back to scalar.

The simplest useful example -- summing a slice:

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

Write a loop. The compiler vectorizes it. The type system tracks what's varying. The mask handles the tail. That's the model.

## Benchmark results

Real numbers from our test infrastructure, measured on an AMD Ryzen 7 6800U:

| Workload | Target | Speedup |
|---|---|---|
| Base64 decode | AVX2 | ~17 GB/s (~9x stdlib, ~77% simdutf C++) |
| Base64 decode | SSSE3 | ~8.5 GB/s |
| lo-min / lo-max | AVX2 8-wide | 7.27x / 7.18x |
| Mandelbrot | AVX2 | 6.07x |
| Mandelbrot | WASM | 2.5-3.6x (varies by host) |
| Hex-encode | WASM | 6-9x (varies by host) |
| Hex-encode | SSE | 6.31x |

One result from this table deserves more context. The `lo-min` / `lo-max` numbers are against scalar `samber/lo`. We also ran the same reductions against [`samber/lo/exp/simd`](https://github.com/samber/lo) -- Go's experimental `simd` intrinsics package -- and SPMD comes out 1.8x to 2.6x faster on sum, min, and contains, even though both emit AVX2 8-wide code. Part of the gap is how new intrinsics are in Go, but there's a class of optimization that requires knowing the whole loop structure. We walk through the disassembly in [Why a Reduction Loop Tells the Story](../spmd-vs-intrinsics-reduction/). With SPMD we control the loop and can [peel it](../spmd-loop-peeling/) to generate optimal code automatically -- something that needs manual work with intrinsics and explains the largest gap in our tests.

Where SPMD shines is loop-shaped problems. When there isn't really a loop -- say, parsing an IPv4 address where the whole thing fits in one 128-bit register -- intrinsics are probably the easier path. I tried building an SPMD IPv4 parser based on Daniel Lemire's [SIMD parser](https://lemire.me/blog/2023/06/08/parsing-ip-addresses-crazily-fast/) work and the best I got was 0.58x the Go standard library. No loop to vectorize meant SPMD had nothing to grab onto.

## Why TinyGo

Why build this on TinyGo instead of the main Go compiler? LLVM.

ISPC -- the closest existing SPMD compiler, and the one we learned the most from -- is built on LLVM. Every SIMD architecture we care about (WASM simd128, x86 SSE/AVX2/AVX-512, ARM NEON/SVE) already has mature vector instruction support in LLVM's backend. TinyGo uses LLVM. The main Go compiler does not. Building on TinyGo meant we could emit LLVM vector IR (`<4 x i32>`, `<32 x i8>`, masked loads and stores) and get correct code on every target without writing any architecture-specific codegen ourselves. The multi-arch support was already there; bolting on SPMD loop lowering was the only missing piece. That let us iterate and experiment without worrying whether the backend could handle the codegen.

Another benefit: TinyGo already had the browser-facing WebAssembly infrastructure needed for the live demos in this post. That mattered -- it let the PoC be something people can run and inspect, not just benchmark numbers in a table.

The catch is Go's duplicated compiler infrastructure. Go has _two_ type-checker implementations (`cmd/compile/internal/types2` and `go/types`), _two_ SSA representations (`cmd/compile/internal/ssa` and `golang.org/x/tools/go/ssa`), and _two_ parser implementations (`cmd/compile/internal/syntax` and `go/parser`). TinyGo uses the `go/` standard-library versions. The main compiler uses the `cmd/compile/internal/` versions. They're near-identical codebases maintained separately.

For the PoC this meant every frontend change -- every type-checker rule for `lanes.Varying[T]`, every parser extension for `go for`, every control-flow restriction -- had to be written in _both_ trees. The SPMD SSA work went into a patched `golang.org/x/tools/go/ssa` since that's what TinyGo consumes, but for upstream it'd go into `cmd/compile/internal/ssa`. We maintained three forked repositories (Go, TinyGo, and x-tools-spmd) and learned the hard way that this work really does span all three.

The duplication isn't a TinyGo problem. It's a Go ecosystem problem. Any tool that needs to understand Go at the type or SSA level -- gopls, staticcheck, go vet, TinyGo -- faces the same split. If the Go project ever unified `types2` and `go/types`, it would benefit every downstream consumer, not just this experimenting with this code base.

For this PoC, TinyGo was the right call: LLVM's vector infrastructure for free, SSA-level iteration without modifying the main compiler, and real executables to benchmark. The tradeoff was double-writing every frontend change and a bit of confusion.

## Why this belongs in the compiler

SPMD isn't a library feature -- it's a compiler feature. The core transforms -- predication (linearizing varying `if`/`else` into masked selects), loop peeling (splitting into an all-ones main body and a masked tail), and pattern detection (recognizing multiply-add, contiguous access, byte-decomposition stores) -- are SSA-level transformations that live at the heart of the compiler.

We found this out the messy way. Our first approach bolted SPMD onto the TinyGo backend as an analysis pass, reconstructing masks from control-flow structure without touching the SSA representation. It worked for simple cases. Then varying switch, compound boolean chains, per-lane break, and inner scalar loops each demanded new special cases. Every bug was "the mask was wrong on this path." We deleted roughly 330 lines of mask-stack code and accepted what we should have known from the start: the varying-ness of control flow must be encoded in the SSA form itself.

The proof-of-concept adds three SPMD-aware SSA instructions (`SPMDLoad`, `SPMDStore`, `SPMDSelect`) and four metadata structures to `go/ssa`. With that foundation, predication and loop peeling become mechanical transforms, and mask correctness is guaranteed by construction rather than reconstructed by analysis.

**How this relates to `simd/archsimd`.** These two approaches sit at different levels. `archsimd` is instruction-level: you pick `Int32x8`, you call `.Add()`, you get one instruction. It's explicit and architecture-facing. SPMD is higher-level: you write `go for`, the compiler picks the width and the instructions. One gives you instruction-level control. The other gives you portable loop-level data parallelism in ordinary Go code.

The Go team has also discussed a portable API layer on top of `archsimd`. That would be a third point in the design space, closer to something like Google's Highway for C++. My hypothesis is that a language approach would still be the most approachable form for application code: `archsimd` underneath for instruction-level control where teams really want it, and SPMD on top for loop-level data parallelism where readability and portability matter more than hand-selecting instructions.

## Where SPMD would help in the stdlib

Two categories stand out as natural fits:

**Image processing.** `image/draw`, `image/color` conversions (RGB-to-YCbCr, alpha premultiplication), JPEG/PNG decode pipelines. Per-pixel arithmetic is the golden SPMD case: contiguous memory, uniform control flow, and many related kernels that share the same shape. A properly vectorized `draw.Draw` becomes one vector load and one vector store per lane-count pixels in the common path. The `image` family has many such kernels, and writing each one with instruction-level SIMD would mean a permanent per-architecture maintenance burden. SPMD compiles them all from one source.

**Byte parsing.** HTTP header scanning (`net/http`), JSON structural character detection, `go/scanner` tokenization, `encoding/hex`, `encoding/base64`. The proof-of-concept's hex-encode and base64 examples are proofs of concept for this whole category. Anything that is "scan a `[]byte`, classify each byte, act on the classification" fits the same pattern: byte-lane iteration, small-table lookup via `vpshufb`, and masked stores. These hot paths are well-tested, self-contained, and performance-visible in real services -- good properties for introducing a new compiler technology.

## How this was built: six months of vibe coding

This whole PoC was built with Claude Code over roughly six months, starting in late 2025. The Go frontend, the TinyGo backend, the x-tools-spmd patches, and the E2E test infrastructure -- but not the examples -- all came out of a human-AI collaboration where I provided the direction and the AI wrote most of the code. I could not have built this alone on that timeline. The compiler engineering involved -- predicated SSA, loop peeling, pattern detection, LLVM IR generation across three SIMD targets -- spans too many domains for one person without deep LLVM and Go experience to execute at this pace.

It was not smooth. The biggest friction was that the model had never seen `go for` before. Every Go example it had ever trained on uses `go` followed by `func()`, never `go` followed by `for`. It consistently tried to rewrite SPMD code as goroutine launches or switched back to pure scalar "because they work", or inserted `go func()` wrappers around `for` loops. **All examples and tests had to be written by hand.** Every `go for` loop, every `lanes.Varying[T]` declaration, every `reduce.Add` call in the test suite -- I wrote those, because the model could not reliably generate valid SPMD Go from scratch. And gopls and my IDE were annoyingly in the way too, not knowing this syntax existed.

Strangely, the reverse worked well: once I gave the model the rules and the context (the type-checker restrictions, the ISPC semantics, the SSA generation strategy), it could _review_ the hand-written examples, virtually tests and find real bugs in the tests/examples before there was any compiler capable of running them. It caught mask-propagation errors, missing edge cases in control-flow rules, and type-checker omissions that I had missed. The model was a better reviewer than it was a writer for novel syntax.

The other persistent source of confusion was the duplicated Go infrastructure. The model regularly mixed up `cmd/compile/internal/types2` with `go/types`, `cmd/compile/internal/ssa` with `golang.org/x/tools/go/ssa`, and `cmd/compile/internal/syntax` with `go/parser`. It would confidently edit the wrong file, add imports from the wrong package, or reference APIs that existed in one SSA but not the other. With three forked repositories, each with its own branch, the context management was genuinely difficult especially before Opus 4.6. A significant fraction of the six months was spent correcting navigation errors rather than making progress.

Things changed in January 2026 when I switched to a structured agent workflow: a development agent writes the code, a separate reviewer agent checks it, and a commit agent handles the git work. The reviewer agent turned out to be the key -- it caught the navigation errors and the types2/go-types mix-ups that the development agent introduced, and it enforced consistency across the three repositories. Sometimes I added a final validation step to check that the result actually matched the goal, because agents love to "defer" work and the reviewer will not always catch that. From that point, the pace of progress changed dramatically. We got the mandelbrot example working in a few weeks.

**What should be reused from this PoC:** the learnings, the design (predicated SSA at the SSA level, explicit masks, pattern detection philosophy), the examples, and the test suite if their consensus on the syntax. **What should not be reused:** the compiler code itself. It was written to explore and validate, not to ship. A real implementation would start from the architectural lessons documented in these articles and build the transforms properly inside `cmd/compile/internal/`, not retrofit them from a TinyGo fork.

The PoC served its purpose: it proved that SPMD-for-Go is viable, identified the patterns that deliver performance, and documented the dead ends so the next person doesn't have to rediscover them.

## Where to go from here

The proof of concept is [open source](https://github.com/Bluebugs/go-spmd). The full implementation spans three repositories: a Go fork (type system and SSA), a TinyGo fork (LLVM backend for WASM and x86), and a patched `golang.org/x/tools` (SSA-level predication and loop peeling). We have 90 end-to-end run tests passing across WASM, SSE, and AVX2, plus compile-only coverage for rejected or incomplete cases. It's far from production-ready, but good enough as an experiment to show what's possible.

We'd welcome feedback from the Go community -- whether you're a developer who'd use this, a compiler engineer who sees how to do it better, or someone who spots a flaw in the design. The interesting question is: should Go have _loop-level_ data parallelism, and if so, what should it look like? (`archsimd` already answers "should Go have SIMD?" -- yes.)

**Go experiment the language change [here](https://gofor-tinygo.netlify.app/)!**

If you want to dig deeper, the rest of this series covers the details:

- [Writing SPMD Go: A Practical Guide](../writing-spmd-go/) -- the mental model, idioms, and worked examples
- [How SPMD Lives in the Compiler](../spmd-compiler-internals/) -- the mask-stack lesson, predicated SSA, and what we would do differently
- [Pattern Matching Outperformed Hand-Written SIMD](../spmd-pattern-matching/) -- why the simplest code produced the fastest output
- [Loop Peeling: Where Most of the Speed Comes From](../spmd-loop-peeling/) -- the single highest-leverage optimization
- [We Built Cross-Lane SIMD Primitives. None of Them Helped.](../spmd-negative-result/) -- the most important negative result
- [Why a Reduction Loop Tells the Story: SPMD vs Per-Op SIMD Intrinsics](../spmd-vs-intrinsics-reduction/) -- a disassembly walkthrough of the structural advantage of whole-loop vectorization

---
