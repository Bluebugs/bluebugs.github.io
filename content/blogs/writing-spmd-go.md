+++
date = '2026-04-15T10:01:00-07:00'
draft = true
title = 'Writing SPMD Go: A Practical Guide'
description = 'How to think about uniform vs varying, write go for loops, use reductions, and avoid the common pitfalls'
featured_image = 'images/lakelouise.jpg'
featured_image_class = 'cover bg-center'
+++

You have heard the pitch: a base64 decoder in 40 lines of Go that runs at ~17 GB/s on AVX2, about 9x faster than `encoding/base64` and within 77% of the best C++ SIMD library. If that got your attention, this article is where you learn how to write code like that yourself.

<!--more-->

I am going to walk through the mental model, the idioms that deliver wins, and the mistakes that will waste your time. Every code example here comes from the proof-of-concept repository and has been compiled and benchmarked. Nothing is hypothetical.

## The mental model: uniform vs varying

In SPMD, every value has one of two shapes:

- **Uniform.** A regular Go value. Same across all SIMD lanes. No annotation needed. Your `int`, your `float32`, your slice header --- all uniform unless you say otherwise.
- **Varying.** A vector of values, one per lane. Typed as `lanes.Varying[T]`. On WASM simd128 a `lanes.Varying[int32]` holds 4 values; on AVX2 it holds 8.

Uniform values are exactly what you are used to in Go. There is no runtime overhead --- they live in scalar registers, not vector registers.

Varying values are the new idea. They represent "this value has a different per-lane content." In generated code, a `lanes.Varying[int32]` is a vector register.

### Implicit broadcast

When you combine a uniform value with a varying one, the uniform is automatically broadcast to every lane:

```go
var v lanes.Varying[int32]
u := int32(10)        // uniform

result := v + u       // v + broadcast(u), result is Varying[int32]
```

You never write the broadcast explicitly. The compiler inserts it, and broadcasts are cheap --- they compile to a single `splat` instruction.

The compiler also keeps values uniform as long as possible. `x := 10 + 20` stays a uniform constant even inside a `go for` body. Only when a uniform meets a varying does broadcast happen. This matters for register pressure: the less you widen, the more room for genuinely varying values.

### The assignment rule

**Varying to uniform is forbidden.** A uniform variable cannot hold a varying value because it has no place to put the per-lane content:

```go
var v lanes.Varying[int32]
var u int32

u = v  // ERROR: cannot use varying value as uniform
```

The only way to go from varying to uniform is through a **reduction**:

```go
u = reduce.Add(v)   // OK: sums all lanes into a single int32
u = reduce.Max(v)   // OK: extracts the max lane
```

Going the other way --- uniform to varying --- is implicit (broadcast). So `v = u` is fine.

Intel's ISPC enforces the same rule for the same reason. Without it, you would silently drop information (which lane's value are you assigning?). Do not look for workarounds.

### Where varying values come from

Three sources:

1. **The iteration variable of `go for i := range N`.** Inside the loop, `i` is varying: `[0, 1, 2, 3]` on a 4-wide target in the first iteration, then `[4, 5, 6, 7]`, and so on.
2. **Loading from a slice inside a `go for`.** `x := slice[i]` where `i` is varying produces a varying `x`.
3. **`lanes.Index()`.** Returns the current per-lane index as a varying value. Equivalent to `lanes.Varying[int]{0, 1, 2, 3}` on a 4-wide target.

`lanes.Count[T]()` is **uniform** --- it is a compile-time constant equal to the lane count for element type T (4 for int32 on WASM, 8 on AVX2). Use it for batch sizing, never per-lane computation.

## Your first `go for`

First, the disambiguation: `go for` is an SPMD loop. `go func()` is a goroutine. The parser tells them apart by looking at the token after `go`. There is no ambiguity.

An SPMD loop is lowered by the compiler into a vectorized main body that processes `laneCount` elements per iteration, plus a masked tail that handles the remainder. You do not see this. You just write the loop.

Here is the simplest useful example --- summing a slice:

```go
// From examples/simple-sum/main.go
func sumSPMD(data []int) int {
    var total lanes.Varying[int] = 0

    go for _, value := range data {
        total += value
    }

    return reduce.Add(total)
}
```

Line by line:

- `var total lanes.Varying[int] = 0` declares a zero-initialized varying accumulator. Each lane starts at 0. The `0` is uniform; it broadcasts automatically.
- `go for _, value := range data` iterates the slice in SPMD. Inside the body, `value` is varying --- on a 4-wide target it is `[data[0], data[1], data[2], data[3]]` in the first main iteration.
- `total += value` adds the current vector of values into the accumulator. One `vpaddd` per main iteration.
- `return reduce.Add(total)` collapses the varying accumulator into a single scalar.

For a 1024-element slice on AVX2 (8 lanes of int), the main loop runs 128 iterations, each doing one load and one add. The scalar equivalent does 1024 of each. Roughly 8x fewer instructions.

## The golden pattern

Almost every loop that hits 5x or better speedup in the proof of concept has this shape:

```go
go for i, x := range in {
    out[i] = transform(x)
}
```

`in[i]` is a contiguous vector load. `out[i] = ...` is a contiguous vector store. In the peeled main body, the mask is all-ones, so the store is a single vector store --- no load-blend-store dance. The main body is typically 5--10 instructions: load, transform, store, pointer advance, branch back.

When writing new SPMD code, aim for this shape first. Complications come from varying-index access (gather/scatter instead of contiguous), strided stores (`out[i*3+k]`), and partial stores under a varying condition. The compiler handles all of these, but contiguous access is where the biggest wins live.

## Reductions and the `lanes.Index()` anti-pattern

Here is the trap. This innocent-looking code is wrong in a deep way:

```go
func findFirst(xs []int32, target int32) int {
    result := -1
    go for i, x := range xs {
        if x == target {
            result = int(i)  // BUG: i is varying, result is uniform
        }
    }
    return result
}
```

The problem: `i` is varying and `result` is uniform. The assignment should not typecheck (and the compiler rejects it). But even if it did, you would get "some lane's value of i," and which lane depends on the SIMD width of your target. On a 4-wide target you might get one answer; on 8-wide you get another.

**Any time your output depends on the SIMD width, you have a correctness bug.** This is not a performance issue --- it is a "your tests pass in one mode and fail in another" issue.

The broader anti-pattern: using `lanes.Index()` for per-lane computation and `reduce.From()` to inspect individual lane values. Both produce lane-count-dependent results.

The correct discipline is to produce scalar results via reductions: `reduce.Add`, `reduce.Min`, `reduce.Max`, `reduce.Or`, `reduce.And`, `reduce.Mask`. All of these give the same answer regardless of SIMD width.

`reduce.From(v)` exists --- it extracts all lanes into a Go slice. It is a code smell in hot paths: slow (N scalar extractions), lane-count-dependent, and a sign you are trying to do per-lane work on the CPU side. Reserve it for tests and debugging.

A possible future language refinement: `lanes.Index()` is really just `iota` --- "the sequence `0, 1, 2, ..., N-1`" --- promoted to vector form. A natural evolution would be to let `iota` carry that meaning when its result type is varying. The proof of concept does not implement this, but the conceptual fit is appealing.

**How to detect lane-count bugs today:** compile in dual mode and diff the output:

```bash
tinygo build -target=wasi -simd=true -o out-simd.wasm main.go
tinygo build -target=wasi -simd=false -o out-scalar.wasm main.go
diff <(wasmer run out-simd.wasm) <(wasmer run out-scalar.wasm)
```

If the outputs differ, you have a lane-count-dependent bug. In a real implementation this should be a static analyzer rule (the properties are syntactic and local), but dual-mode diffing is the practical workaround for now.

## Control flow rules

Most of Go's control flow works inside a `go for`:

- **`if` with varying condition** --- the compiler executes both branches under masks and merges via select. You write normal Go; the mask is invisible.
- **`switch` with varying tag** --- each case runs under its mask.
- **`&&` and `||` with varying operands** --- short-circuit semantics preserved via mask composition.
- **`continue`** --- always fine.
- **Inner scalar `for` loops** --- allowed. If the iteration count is varying (e.g., iterating per-lane slices of different lengths), the compiler runs up to `max(len_per_lane)` iterations and masks off lanes that finish early.

What is forbidden:

- **`return` under a varying condition.** "Which lanes would return?" has no clean answer. Rejected at compile time.
- **`break` under a varying condition.** Same reason. (`break` under a uniform condition is fine --- the mandelbrot example relies on this.)
- **`panic` inside a `go for`.** Varying panics are nonsensical. If you need per-lane error detection, set a sticky varying bool and check it after the loop with `reduce.Or`.
- **Nested `go for` inside `go for`.** Ambiguous lane count. Use outer batching instead.
- **`go for` inside an SPMD function** (one that takes varying parameters). Same reason.

The error messages for these are specific. The type checker knows which rule you violated and tells you.

One more restriction: **only private functions can take varying parameters.** Masks and lane counts are implementation details. A library that exports a function with varying parameters leaks that it uses SPMD. The idiomatic workaround is to wrap your SPMD kernel in a scalar-interface public function:

```go
func transformKernel(dst, src []float32) {    // private, uses go for internally
    go for i, x := range src { dst[i] = x*2 + 1 }
}

func Transform(src []float32) []float32 {     // public, normal Go signature
    dst := make([]float32, len(src))
    transformKernel(dst, src)
    return dst
}
```

## Performance patterns

### Cascading `go for` for widening multiply-add

This is the highest-impact idiom in the entire proof of concept. The compiler recognizes cascading `go for` loops of decreasing SIMD width --- byte to int16 to int32 --- each doing a constant-coefficient multiply-add, and emits `vpmaddubsw` / `vpmaddwd` on x86 (or equivalent on WASM).

From the base64 decoder (`examples/base64-decoder/main.go:41`):

```go
func decodeAndPack(dst, src []byte) int {
    n := len(src)

    // Loop 1 (byte-width): decode ASCII to 6-bit sextets via nibble LUT.
    sextets := make([]byte, n)
    go for i, ch := range src {
        s := ch + decodeLUT[ch>>4]
        if ch == byte('+') { s += 3 }
        sextets[i] = s
    }

    // Loop 2 (int16-width): merge sextet pairs. a*64 + b -> pmaddubsw.
    halfLen := n / 2
    merged := make([]int16, halfLen)
    go for g := range merged {
        merged[g] = int16(sextets[g*2])*64 + int16(sextets[g*2+1])
    }

    // Loop 3 (int32-width): merge int16 pairs. a*4096 + b -> pmaddwd.
    quarterLen := halfLen / 2
    packed := make([]int32, quarterLen)
    go for g := range packed {
        packed[g] = int32(merged[g*2])*4096 + int32(merged[g*2+1])
    }

    // Loop 4: extract 3 bytes per packed int32.
    go for g := range packed {
        dst[g*3+0] = byte(packed[g] >> 16)
        dst[g*3+1] = byte(packed[g] >> 8)
        dst[g*3+2] = byte(packed[g])
    }

    return quarterLen * 3
}
```

The programmer writes plain Go. No builtins, no intrinsics, no annotations. The compiler recognizes the patterns and emits the tightest available SIMD instructions for each target. The hot loop went from 14.3 instructions per byte (scatter-gather version) to 0.44 instructions per byte --- a 32x instruction reduction.

### Chunk sizing with `lanes.Count[T]()`

Still in the base64 decoder, the outer driver sizes its chunks to match the register width:

```go
var bv lanes.Varying[byte]
chunkSize := max(4, lanes.Count[byte](bv))

for off := 0; off+chunkSize <= hotBytes; off += chunkSize {
    n := decodeAndPack(dst[outOffset:], src[off:off+chunkSize])
    outOffset += n
}
```

`chunkSize` is 16 on SSE, 32 on AVX2, and 4 in scalar fallback mode. The `max(4, ...)` is load-bearing: the cascading byte → int16 → int32 structure needs at least 4 input bytes for every level to produce meaningful work (4 bytes → 2 int16 → 1 int32 → 3 output bytes). In scalar mode where `lanes.Count` returns 1, without the floor the int16 and int32 loops would be empty. In SIMD mode the lane count is already >= 16 so the `max` is a no-op.

The general pattern: for encoder/decoder kernels with cascading width reductions, use `max(algorithmicMinimum, lanes.Count[T]())` to size chunks. The minimum depends on the cascade depth. Dual-mode testing catches this if you forget.

### Byte-lane vs int-lane iteration

On AVX2, iterating at byte granularity gives you **32 lanes** per iteration. Iterating at int32 granularity gives you **8 lanes**. That is 4x more parallelism if your algorithm can be expressed at the byte level. Encoders, decoders, compressors, and hashes often can. Numerical kernels usually need the precision of int32 or float32.

Rule of thumb: if your algorithm is naturally byte-parallel, prefer byte lanes.

### Outer-SPMD batching

The IPv4 parser in the proof of concept is instructive for a different reason: it does **not** speed up with inner SPMD. Vectorizing the parse of a single IP address peaks at 0.58x scalar --- actually slower --- because a single IPv4 string has only 4--15 characters and the vector setup overhead eats the gains.

The architectural answer is **outer-SPMD batching**: vectorize across independent instances instead of within one instance. If you have 16 IPv4 strings to parse, run 16 parsers in parallel, one per lane.

The general rule: when one instance of your computation is too small or too irregular to vectorize internally, vectorize across instances. This is how ISPC's flagship use cases (ray tracers, physics solvers) work.

## Debugging

### `fmt.Printf` with `%v` on varying values

This is the fastest debugging tool:

```go
go for i := range 16 {
    v := i * 3
    if i%2 == 0 {
        fmt.Printf("%v\n", v)  // prints: [0 _ 6 _] then [12 _ 18 _]
    }
}
```

`%v` on a varying value prints `[value _ value _ ...]` where active lanes show their values and inactive lanes show `_`. The mask becomes immediately visible. Use it liberally while writing your first SPMD code.

### Dual-mode build and diff

Compile with both `-simd=true` and `-simd=false`, run both, diff the output. If they differ, you have a lane-count-dependent bug. The test script `test/e2e/spmd-e2e-test.sh` Level 8 automates this for every example.

### Reading generated code

When a loop is slower than you expected, read the generated assembly:

- **WASM:** `wasm2wat out.wasm | less`. Look for `v128.load`, `v128.store`, `i32x4.add`, `v128.swizzle`. If you see scalar `i32.load` in the hot loop, vectorization did not trigger.
- **x86-64:** `llvm-objdump -d out.elf | less`. Look for `vmovdqu` (load/store), `vpaddd` (add), `vpshufb` (byte shuffle), `vpmaddubsw` / `vpmaddwd` (the magic).
- If you see `pextrd` / `pinsrd` sequences dominating, the compiler is gathering/scattering when it should be doing contiguous ops. That means the contiguous access analyzer did not recognize your pattern. File a bug.

## Worked examples

### Hex-encode: two ways to write the same loop

From `examples/hex-encode/main.go`. The **dst-centric** version iterates over the destination:

```go
const hextable = "0123456789abcdef"

func Encode(dst, src []byte) int {
    go for i := range dst {
        v := src[i>>1]
        if i%2 == 0 {
            dst[i] = hextable[v>>4]
        } else {
            dst[i] = hextable[v&0x0f]
        }
    }
    return len(src) * 2
}
```

The iteration variable `i` is varying. `src[i>>1]` is a gather. `hextable[...]` is a 16-entry table lookup that compiles to `pshufb`/`v128.swizzle`. The `if i%2 == 0` is a varying conditional --- both branches compute, the mask selects. `dst[i] = ...` is a contiguous store. On WASM simd128 this hits **6-9x** scalar (varies by host/runtime). On x86 SSE, **6.31x**.

The **src-centric** version iterates over the source instead:

```go
func EncodeSrc(dst, src []byte) int {
    go for i := range src {
        dst[i*2]   = hextable[src[i]>>4]
        dst[i*2+1] = hextable[src[i]&0x0f]
    }
    return len(src) * 2
}
```

Same output, different shape. The strided stores `dst[i*2]` and `dst[i*2+1]` trigger the byte-decomposition store pattern: the compiler recognizes that they form a stride-2 interleaved write and emits a single bitcast + pshufb + store sequence.

On WASM the dst-centric form wins slightly; on AVX2 the difference is small. **This is a recurring theme in SPMD: the same algorithm can be expressed multiple ways, and the best one depends on the target.** Benchmark both.

### Mandelbrot: divergent iteration with SPMD function calls

From `examples/mandelbrot/main.go`. The kernel is an SPMD function --- it takes varying parameters and is called under a mask:

```go
func mandelSPMD(cRe, cIm lanes.Varying[float32], maxIter int) lanes.Varying[int] {
    var zRe lanes.Varying[float32] = cRe
    var zIm lanes.Varying[float32] = cIm
    var iterations lanes.Varying[int] = maxIter

    for iter := range maxIter {
        magSquared := zRe*zRe + zIm*zIm
        diverged := magSquared > 4.0

        if diverged {
            iterations = iter
            break
        }

        newRe := zRe*zRe - zIm*zIm
        newIm := 2.0 * zRe * zIm
        zRe = cRe + newRe
        zIm = cIm + newIm
    }

    return iterations
}
```

The inner `for iter := range maxIter` is a uniform loop. Every lane runs the same number of iterations, but the mask narrows as lanes diverge: when a lane's point escapes the set (`magSquared > 4.0`), `break` records that lane's iteration count and masks it off. The loop exits early when all lanes have diverged.

The driver calls the kernel from inside a `go for`:

```go
func mandelbrotSPMD(x0, y0, x1, y1 float32,
    width, height, maxIter int, output []int) {
    dx := (x1 - x0) / float32(width)
    dy := (y1 - y0) / float32(height)

    for j := 0; j < height; j++ {
        y := y0 + float32(j)*dy
        go for i := range width {
            x := x0 + lanes.Varying[float32](i)*dx
            iterations := mandelSPMD(x, y, maxIter)
            output[j*width+i] = iterations
        }
    }
}
```

The outer `for j` is scalar (rows). The inner `go for i := range width` is SPMD (columns). Each lane computes a different x coordinate; all lanes in a group share the same y. `mandelSPMD` receives the mask implicitly and carries it through its internal control flow.

Measured speedup: **6.07x** on AVX2, **3.71x** on SSE, **2.5-3.6x** on WASM simd128 (varies by host).

The lesson: divergent iteration counts --- different lanes finishing their work at different times --- are handled well by SPMD. Write the uniform loop with a varying break condition. The compiler tracks per-lane masks correctly. You do not manage any of this yourself.

## What to remember

Write the loop. Trust the compiler. If the generated code is bad, file a bug --- do not reach for a cross-lane builtin. Most of the time, the fix is in the compiler's pattern recognizer, not in your code.

The patterns that deliver wins: contiguous slice loads and stores (the golden case), cascading `go for` loops with constant-coefficient multiply-add, chunk sizing with `lanes.Count[T]()`, and varying accumulators collapsed with `reduce.Add` / `reduce.Max` / `reduce.Min`.

The anti-patterns that hurt: using `lanes.Index()` for per-lane computation, inspecting individual lanes with `reduce.From`, reaching for `Swizzle` or `*Within` operations before measuring, and vectorizing a single small string when you could batch across strings.

The proof of concept is open source. The full developer guide, the compiler internals, and every example referenced in this article are in the repository. If you want to try it yourself, `GOEXPERIMENT=spmd` and a forked TinyGo are all you need.

---

**Previous in series:** [SPMD for Go: What If Your Loops Were 9x Faster?](../spmd-results/) --- the pitch, with live demos and benchmark numbers.

**Further reading:** [Pattern Matching Beats Hand-Written SIMD](../spmd-pattern-matching/) --- why the base64 decoder's idiomatic Go outperforms explicit cross-lane operations. [We Built Cross-Lane Primitives. None of Them Helped.](../spmd-negative-result/) --- the most important negative result from the proof of concept.
