+++
date = '2026-04-15T10:00:00-07:00'
draft = true
title = 'SPMD for Go: What If Your Loops Were Just Faster?'
description = 'A proof of concept for language-level data parallelism in Go, with live WASM demos and real benchmark results'
featured_image = 'images/banff.jpg'
featured_image_class = 'cover bg-center'
+++

We wrote a base64 decoder in 40 lines of Go. It runs at roughly 17 GB/s on AVX2 -- about 9x faster than `encoding/base64` and within 77% of the best C++ SIMD library ([simdutf](https://github.com/simdutf/simdutf)). No assembly. No intrinsics. No `unsafe`. Just Go with a new loop keyword. Below are two live demos running real WebAssembly code in your browser.

<!--more-->

## See it: Mandelbrot

Two compilations of the same Go source -- scalar on the left, Single Program Multiple Data (SPMD) on the right. Both come from [`examples/mandelbrot/main.go`](https://github.com/Bluebugs/go-spmd/blob/main/examples/mandelbrot/main.go). The only difference is the compiler flag: `-simd=false` vs `-simd=true`. Click "Run Benchmark" to see the gap.

The SPMD version uses `go for` to process multiple pixels per iteration and `lanes.Varying[float32]` for the complex-plane coordinates. The compiler handles the rest: vectorized arithmetic, per-lane break when a point diverges, and a masked tail for the last few pixels in each row.

{{< spmd-mandelbrot >}}

Here is the full SPMD mandelbrot -- the calling loop and the kernel together:

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

The structure: a scalar `for j` iterates rows. Inside it, `go for i := range width` is the SPMD loop -- each lane computes a different x coordinate in parallel, all sharing the same y. The kernel `mandelSPMD` is an SPMD function (it takes varying parameters); the compiler can generate SIMD instruction transparently thanks to the developer expressing the data parallelism.

The `break` inside a varying `if` is the interesting part. Each lane breaks independently -- when a pixel diverges, its lane goes inactive while the others keep iterating. The compiler turns this into per-lane mask tracking: no branches, just predicated execution.

`go for` is to data parallelism what `go func` is to control flow parallelism.

## See it: Base64

The base64 decoder is four `go for` loops with plain Go arithmetic. No SIMD intrinsics. No cross-lane shuffle operations. The compiler recognizes the multiply-add patterns and emits the right SIMD instructions for every target -- `vpmaddubsw`/`vpmaddwd` on x86, deinterleave-widen-multiply-add on WASM.

{{< spmd-base64 >}}

Here is the kernel (with reference to expected SIMD instruction when implemented in assembly):

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

The compiler sees `int16(a)*64 + int16(b)` and emits `vpmaddubsw`. It sees the stride-3 byte extraction and emits a `vpshufb`-based byte-decomposition store. Four pattern detectors fire simultaneously on this one function -- all from idiomatic Go that any developer can read. This is exactly what a compiler is designed for. Once you express data parallelism, the compiler can optimize.

## The 30-second explanation

Three concepts make SPMD work in Go:

**`go for`** marks a loop for data-parallel execution. The compiler vectorizes it: main body with all lanes active, plus a masked tail for the remainder. It is the duty of the developer to make sure the code can compute data in parallel, like it is the duty of the developer to make sure a `go func` is safe to execute in a parallel thread as a parallel control flow.

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
| Mandelbrot | WASM | 2.5-3.6x (varies by host) |
| Hex-encode | WASM | 6-9x (varies by host) |
| Hex-encode | SSE | 6.31x |

Honest disclosure: not everything speeds up. Our IPv4 parser hit 0.58x with inner-SPMD -- actually _slower_ than scalar while Daniel Lemire manage to do [so](https://lemire.me/blog/2023/06/08/parsing-ip-addresses-crazily-fast/) pretty well. It might be a skill issue on my part. Also very often the scalar generated version of a SPMD code can be slightly slower than current Go stdlib. This is often because you do write algorithm differently when you expect things to be processed in parallel than when you expect them not to.

## Why TinyGo

A fair question: why build this on TinyGo rather than the main Go compiler?

The short answer is LLVM. ISPC -- the closest existing SPMD compiler, and the one we learned the most from -- is built on LLVM. Every SIMD architecture we care about (WASM simd128, x86 SSE/AVX2/AVX-512, ARM NEON/SVE) already has mature vector instruction support in LLVM's backend. TinyGo uses LLVM. The main Go compiler does not. Building on TinyGo meant we could emit LLVM vector IR (`<4 x i32>`, `<32 x i8>`, masked loads and stores) and get correct code on every target without writing a single line of architecture-specific codegen ourselves. The multi-architecture support was already there; bolting on SPMD loop lowering was the only missing piece. This helped iterate and experiment as we know the underlying compiler would handle the code generation just fine.

Another benefit is the tinygo project has developed a playground which can run code in your browser using webassembly. Which is exacly what enable us to provide this [here](FIXME) for you to experiment with!

That said, TinyGo has its own costs. The biggest one is the Go compiler's duplicated infrastructure. Go has _two_ type-checker implementations (`cmd/compile/internal/types2` and `go/types`), _two_ SSA representations (`cmd/compile/internal/ssa` and `golang.org/x/tools/go/ssa`), and _two_ parser implementations (`cmd/compile/internal/syntax` and `go/parser`). TinyGo uses the `go/` standard-library versions of all three. The main Go compiler uses the `cmd/compile/internal/` versions. They are near-identical codebases maintained separately.

For the PoC, this meant every frontend change -- every type-checker rule for `lanes.Varying[T]`, every parser extension for `go for`, every control-flow restriction -- had to be written in _both_ trees. The SPMD SSA work went into a patched `golang.org/x/tools/go/ssa` because that is what TinyGo consumes, but for an upstream Go implementation the same patterns would go into `cmd/compile/internal/ssa`. We ended up maintaining three forked repositories (Go, TinyGo, and x-tools-spmd) and made bad decision along the way as we learned we really had to make change in all those repository.

The duplication is not a TinyGo problem. It is a Go ecosystem problem. Any tool that needs to understand Go at the type or SSA level -- gopls, staticcheck, go vet, TinyGo -- faces the same split. If the Go project ever unified `types2` and `go/types`, or converged the two SSA representations, it would benefit every downstream consumer, not just SPMD.

For the PoC's purposes, TinyGo was the right choice. It gave us LLVM's vector infrastructure for free, it let us iterate on the SSA-level transforms without modifying the main Go compiler, and it produced real executables we could benchmark on real hardware. The tradeoff was the double-write tax on frontend work.

## Why this belongs in the compiler

SPMD is not a library feature. It is a compiler feature. The core transforms -- predication (linearizing varying `if`/`else` into masked selects), loop peeling (splitting into an all-ones main body and a masked tail), and pattern detection (recognizing multiply-add, contiguous access, byte-decomposition stores) -- are SSA-level transformations that live at the heart of the compiler.

We learned this the hard way. Our first approach tried to bolt SPMD onto the TinyGo backend as an analysis pass, reconstructing masks from control-flow structure without touching the SSA representation. It worked for simple cases. Then varying switch, compound boolean chains, per-lane break, and inner scalar loops each demanded new special cases. Every bug was "the mask was wrong on this path." We deleted roughly 330 lines of mask-stack code and accepted what we should have known from the start: the varying-ness of control flow must be encoded in the SSA form itself.

The proof-of-concept adds three SPMD-aware SSA instructions (`SPMDLoad`, `SPMDStore`, `SPMDSelect`) and four metadata structures to `go/ssa`. With that foundation, predication and loop peeling become mechanical transforms, and mask correctness is guaranteed by construction rather than reconstructed by analysis.

**How this relates to `simd/archsimd` (Go 1.26).** The two approaches are complementary, not competing. `archsimd` is instruction-level: you pick `Int32x8`, you call `.Add()`, you get one instruction. It is "SIMD as `syscall`" -- architecture-specific. It build up infrastructure and support for assembly instruction not otherwise available. It is a necessary step to enable higher level. SPMD is that higher level of abstraction: you write `go for`, the compiler picks the width and the instructions. It is "SIMD as `go for`" -- portable, automatic, and right for application code where you want the compiler to do the work with readable idiomatic Go.

The Go team has described a planned portable high-level API on top of `archsimd`. That would be a third point in the design space -- something like Google's Highway for C++. Our hypothesis is that a language approach will likely outperform such approach and enable more developers to use it: `archsimd` underneath for instruction-level control for teams that really want hand written non portable assembly for that last 20% speed up and SPMD on top for loop-level data parallelism for everyone else. They serve different audiences and different use cases.

## Where SPMD would help in the stdlib

Two categories stand out as natural fits:

**Image processing.** `image/draw`, `image/color` conversions (RGB-to-YCbCr, alpha premultiplication), JPEG/PNG decode pipelines. Per-pixel arithmetic is the golden SPMD case: contiguous memory, uniform control flow, and many related kernels that share the same shape. A properly vectorized `draw.Draw` becomes one vector load and one vector store per lane-count pixels in the common path. The `image` family has many such kernels, and writing each one with instruction-level SIMD would mean a permanent per-architecture maintenance burden. SPMD compiles them all from one source.

**Byte parsing.** HTTP header scanning (`net/http`), JSON structural character detection, `go/scanner` tokenization, `encoding/hex`, `encoding/base64`. The proof-of-concept's hex-encode and base64 examples are proofs of concept for this whole category. Anything that is "scan a `[]byte`, classify each byte, act on the classification" fits the same pattern: byte-lane iteration, small-table lookup via `vpshufb`, and masked stores. These hot paths are well-tested, self-contained, and performance-visible in real services -- good properties for introducing a new compiler technology.

## How this was built: six months of vibe coding

I want to be transparent about how this proof of concept came together, because the process itself is part of the story.

This entire PoC was built with Claude Code over roughly six months, starting in late 2025. The Go frontend, the TinyGo backend, the x-tools-spmd patches, the E2E test infrastructure, but not the examples -- all of it was produced through a human-AI collaboration where I provided the direction and the AI wrote the code. I could not have built this alone. Not in six months, probably not ever. The compiler engineering involved -- predicated SSA, loop peeling, pattern detection, LLVM IR generation across three SIMD targets -- spans too many domains for one person without deep LLVM experience to execute at this pace.

That said, it was not smooth. The biggest friction was that the model had never seen `go for` before. Every Go example it had ever trained on uses `go` followed by `func()`, never `go` followed by `for`. It consistently tried to rewrite SPMD code as goroutine launches or switched back to pure scalar "because they work", or inserted `go func()` wrappers around `for` loops. **All examples and tests had to be written by hand.** Every `go for` loop, every `lanes.Varying[T]` declaration, every `reduce.Add` call in the test suite -- I wrote those, because the model could not reliably generate valid SPMD Go from scratch. And gopls and my IDE also were annoyingly in the way as they were not aware of this syntax.

Strangely, the reverse worked well: once I gave the model the rules and the context (the type-checker restrictions, the ISPC semantics, the SSA generation strategy), it could _review_ the hand-written examples and tests and find real bugs. It caught mask-propagation errors, missing edge cases in control-flow rules, and type-checker omissions that I had missed. The model was a better reviewer than it was a writer for novel syntax.

The other persistent source of confusion was the duplicated Go infrastructure. The model regularly mixed up `cmd/compile/internal/types2` with `go/types`, `cmd/compile/internal/ssa` with `golang.org/x/tools/go/ssa`, and `cmd/compile/internal/syntax` with `go/parser`. It would confidently edit the wrong file, add imports from the wrong package, or reference APIs that existed in one SSA but not the other. With three forked repositories, each with its own branch, the context management was genuinely difficult. A significant fraction of the six months was spent correcting navigation errors rather than making progress.

Things changed in January 2026 when I switched to a structured agent workflow: a development agent writes the code, a separate reviewer agent checks it, and a commit agent handles the git work. The reviewer agent turned out to be the key -- it caught the navigation errors and the types2/go-types mix-ups that the development agent introduced, and it enforced consistency across the three repositories. We sometime added a last one that validated that the goal achieved matched the goal given. A few times, the agent would love to "defer" things to the future and the reviewer won't catch that. From that point, the pace of progress was dramatic. We got the mandelbrot example working in a few weeks.

**What should be reused from this PoC:** the learnings, the design (predicated SSA at the SSA level, explicit masks, pattern detection philosophy), the examples, and the test suite. **What should not be reused:** the compiler code itself. It was written to explore and validate, not to ship. A real implementation would start from the architectural lessons documented in these articles and build the transforms properly inside `cmd/compile/internal/ssa`, not retrofit them from a TinyGo fork.

The PoC served its purpose: it proved that SPMD-for-Go is viable, it identified the patterns that deliver performance, and it documented the dead ends so the next person does not have to rediscover them. That is enough.

## Where to go from here

The proof of concept is open source. The full implementation spans three repositories: a Go fork (type system and SSA), a TinyGo fork (LLVM backend for WASM and x86), and a patched `golang.org/x/tools` (SSA-level predication and loop peeling). We have 90 end-to-end tests passing across WASM, SSE, and AVX2. It work good enough for a proof of concept and an experiment to show what is possible.

We would welcome feedback from the Go community -- whether you are a developer who would use this, a compiler engineer who sees how to do it better, or someone who spots a flaw in the design. The interesting conversation is not "should Go have SIMD?" (it should, and `archsimd` is already here) but rather "should Go have _loop-level_ data parallelism, and if so, what should it look like?"

**Go experiment the language change [here](FIXME)!**

If you want to dig deeper, the rest of this series covers the details:

- [Writing SPMD Go: A Practical Guide](../writing-spmd-go/) -- the mental model, idioms, and worked examples
- [How SPMD Lives in the Compiler](../spmd-compiler-internals/) -- the mask-stack lesson, predicated SSA, and what we would do differently
- [Pattern Matching Beats Hand-Written SIMD](../spmd-pattern-matching/) -- why the simplest code produced the fastest output
- [Loop Peeling: Where Most of the Speed Comes From](../spmd-loop-peeling/) -- the single highest-leverage optimization
- [We Built Cross-Lane SIMD Primitives. None of Them Helped.](../spmd-negative-result/) -- the most important negative result

---
