+++
date = '2026-05-10T10:00:00-07:00'
draft = false
title = 'What a Reduction Loop Reveals About SPMD vs Per-Op Intrinsics'
description = 'A side-by-side disassembly of the same AVX2 reduction reveals a structural advantage of whole-loop vectorization over per-operation intrinsics'
featured_image = 'images/mountain-2.jpg'
featured_image_class = 'cover bg-center'
tags = ['SPMD', 'SIMD', 'AVX2', 'intrinsics', 'benchmarks']
+++

On three identical AVX2 reductions over `[]int32` -- sum, min, contains -- our SPMD-compiled code is 1.8x to 2.6x faster than the same algorithms written against [`samber/lo/exp/simd`](https://github.com/samber/lo), the experimental Go library built on Go's new `simd` intrinsics package. Both run AVX2 8-wide. Both issue roughly the same number of vector ops in the body. The runtime gap is not about ISA choice. It is about what each compiler can see when it codegens the loop, and that turns out to be a structural property of how the intrinsic API is shaped.

<!--more-->

This post walks through the disassembly for two of the three kernels and explains where the cycles go. The takeaway: **per-operation SIMD intrinsics that return a vector through the ABI return register cannot keep a loop-carried accumulator live across a call boundary.** And there are a few other places where owning the whole loop gives the compiler optimizations the intrinsic user cannot reach from the library side without cumbersome code structure.

## The setup

Same hardware (AMD Ryzen 7 6800U, Zen3). Same workload: a 1024-element `[]int32`. Three implementations of each kernel:

1. **scalar `samber/lo`** -- generic Go, our baseline.
2. **`samber/lo/exp/simd`** -- the experimental library that wraps Go's new `simd` intrinsics. Compiled with stock `go`.
3. **SPMD** -- a `go for` loop with a `lanes.Varying[int32]` accumulator and a final `reduce.Add`/`reduce.Min`/`reduce.Any`. Compiled with our TinyGo+SPMD fork targeting `amd64-avx2`.

The numbers:

| Kernel        | lo/simd (Go intrinsics) | SPMD (TinyGo+SPMD) | SPMD ratio |
|---------------|------------------------:|-------------------:|-----------:|
| Sum           | 329 ns/op               | 181 ns/op          | 1.82x      |
| Min           | 337 ns/op               | 160 ns/op          | 2.11x      |
| Contains (x8) | 178 ns/op               | 69 ns/op           | 2.57x      |

Both binaries emit AVX2. Both have hot loops that consume 8 i32 lanes per iteration. So why the gap?

## Sum: the accumulator goes to the stack and back, every iteration

Here is the hot loop of `lo/simd.SumInt32x8`, trimmed to the essentials (about 26 instructions per 8 elements):

```asm
vmovdqu  [rsp+0x38], ymm0       ; spill the accumulator
mov      ebx, 0x8
CALL     LoadInt32x8Slice        ; returns the loaded <8xi32> in ymm0
vmovdqu  ymm1, [rsp+0x38]        ; reload the accumulator
vpaddd   ymm0, ymm1, ymm0        ; accumulate
mov      rcx, [rsp+0x88]         ; reload 3 loop variables the call may have clobbered
mov      rdx, [rsp+0x60]
mov      rax, [rsp+0x58]
lea/cmp/jb/cmp/jbe                ; loop control + bounds checks
```

And here is the SPMD version (about 27 instructions per 8 elements):

```asm
; ...tail-mask setup (4-6 instrs, runs every iter even on full-width chunks)...
vpand    ymm3, ymm3, [rbx]       ; masked load via memory-operand AND
vpaddd   ymm0, ymm3, ymm0        ; accumulator stays in ymm0 across all iterations
add r11d, -8 / add r10, r8 / add r9, 8 / jmp
```

Same vector op count. The difference is the **store-call-reload chain on `ymm0`**. The `simd` intrinsic `LoadInt32x8Slice` returns its result through `ymm0` -- the standard vector return register. That is also the natural home for the running accumulator. So before every call, the accumulator gets spilled to the stack; after every call, it gets reloaded; only then can the `vpaddd` proceed. On top of that, the compiler reloads three loop-control registers after each call because the callee is opaque to it.

The store→call→reload pattern adds roughly six cycles of latency per iteration (store-to-load forwarding plus the call boundary). Over 128 iterations of a 1024-element reduction, that is most of the gap.

SPMD avoids it entirely. Because the loop body is a single LLVM function with no opaque calls inside it, the accumulator simply stays live in `ymm0` for the duration of the loop.

The horizontal reduce at the end shows the same pattern from a different angle. `lo/simd` stores the vector to the stack and then runs a scalar 8-element loop summing it back -- 9 instructions, all scalar. SPMD does `vextracti128 + 3×vphaddd + vmovd` -- 5 instructions, all vector. Both are correct. The latter happens because LLVM owns the reduction.

This could be improved in the current `go` compiler by fully inlining the intrinsics, avoiding the register spill. It's not a fundamental limitation of the simd intrinsics proposal -- it's just how young this code path is in the `go` compiler.

## Contains: three structural wins compound

The Sum case shows the cost of the call boundary on the loop-carried accumulator. Contains shows what happens when the compiler also owns the loop *shape*.

`lo/simd.ContainsInt32x8` hot loop, about 27 instructions per 8 elements:

```asm
...bounds checks...
CALL     LoadInt32x8Slice
vmovdqu  ymm1, [rsp+0x18]        ; reload broadcast needle
vpcmpeqd ymm0, ymm0, ymm1
vmovmskps edx, ymm0              ; vector -> GP bitmask
test     dl, dl
je       <continue>
```

SPMD's tight main loop, 9 instructions per 8 elements:

```asm
add r10, 0x8 / cmp r10, r8 / jge
lea r11, [r9+rcx] / sar r9, 0x1e
vpcmpeqd ymm2, ymm1, [rdi+r9]    ; load + compare fused into one instruction
vtestps  ymm2, ymm2               ; sets ZF directly from a vector
mov r9, r11
je       <found>
```

Three things compound:

1. **Loop peeling.** When the remaining length is at least 8, no mask is needed. SPMD emits a stripped main body for the all-active case and reserves the masked path for the tail. The intrinsic version cannot peel because it does not own the loop -- the user wrote it. The user has to implement the peeling.
2. **Memory-operand `vpcmpeqd`.** With the load fused into the compare, what was two instructions becomes one. The `simd` API exposes the load as a separate function, so the compiler never has the chance to fuse it.
3. **`vtestps` instead of `vmovmskps + test`.** The natural Go expression for "is any lane true?" is `cmp.ToBits() != 0`, which goes through a vector→GP-register move. `vtestps` sets ZF directly from a vector register. Today there is no intrinsic that exposes it.

These are hard to optimize in a library like google highway. With SPMD, they live in the compiler, and it sees the loop as a unit it can optimize as a whole.

## Min has its own small story

For brevity I am skipping the Min disassembly, but it carries a `firstInitialized bool` so the first iteration seeds `minVec` instead of comparing. That bool turns into a `movzx + test + je` inside the hot loop -- well-predicted, but still decoded and dispatched every iteration. SPMD does not need it: the mask handles the first iteration the same way it handles every other iteration.

(The horizontal reduce shows the same vector-vs-scalar split as Sum: 7 vector instructions for SPMD versus 17 scalar instructions for `lo/simd`.)

## What the gap is, structurally

Three root causes, in order of how much they cost:

**1. The intrinsic return-value ABI forces an accumulator spill.** A vector intrinsic that returns a vector lands in `ymm0`. Any caller-side vector that has to stay live across the call -- which is exactly what a reduction's accumulator is -- gets spilled. Per iteration. For the entire loop. There is no way around it inside the current API shape.

**2. Per-op intrinsics are bodyless to the compiler.** The library exposes `LoadInt32x8Slice` and friends as assembly-backed stubs that the compiler cannot inline. If `go` could see through them, the load would fold into the next op exactly the way LLVM does for SPMD, the spill would vanish, and Sum's loop would collapse to roughly 15 instructions per 8 elements -- faster than what SPMD currently emits. Likely an opportunity for both to get faster!

**3. Loop-level rewrites are not available to the intrinsic user.** Loop peeling, choosing between `vmovmskps` and `vtestps`, ... -- these are not library decisions. They are compiler-level transformations that need to see the loop as a whole or the developer need to shape his code with this optimization in mind.

**4. Portability.** Intrinsics expose hardware rather than abstracting it. With SPMD you get portable and competitive code, while with intrinsics you need to know each architecture and duplicate your files, including a fallback to scalar Go. That burden gets lifted with `go for`.

## Could the experimental `simd` package narrow the gap?

In two of the three kernels, yes -- partially -- with API or implementation changes:

- **Sum and Min** could close most of the gap if the load intrinsic wrote into a caller-owned accumulator instead of returning through `ymm0`. Something shaped like `acc.AddFrom(slice)` rather than `acc = acc.Add(LoadInt32x8Slice(slice))` would let the compiler keep the accumulator live across the call. An alternative path is to make the intrinsic wrappers genuinely inlinable so `go` can see through them; that has its own tradeoffs.
- **Contains** is harder. Closing the gap there would need a new intrinsic exposing `vtestps`-style direct ZF set, plus loop peeling at the `go` level. The first is a library extension; the second is a compiler change or a burden on the user of the API.

There is also a backend angle worth noting. If the `simd` package were ported to TinyGo, each intrinsic would naturally lower to LLVM vector IR rather than an opaque assembly stub, so two of the three causes above -- the call-boundary accumulator spill and the missed load/compare fusion -- would mostly disappear on their own: LLVM's register allocator would keep the accumulator live across iterations, and its instruction selector would fuse memory operands and pattern-match idioms like "any-lane true" to `vtestps` without the library having to ask. The third cause -- loop peeling and the rest of the loop-shape rewrites -- would not be addressed by a backend change alone, because the compiler still does not own the loop. That residual is exactly the seam where a loop-level construct earns its keep, and it lines up with the conclusion above: backends can close the codegen gap, but loop-level transformations need a loop-level anchor.

This isn't a criticism of the `simd` package. It's doing exactly what an instruction-level intrinsic library is supposed to do. **The API shape (per-op, vector return values) carries a structural cost on loop-carried state that no amount of library polish can remove**, and a complementary loop-level approach can address that cost without competing with the intrinsic surface for control.

One of the example, I am not covering much here, is the IPv4 parser which I couldn't get to its theoretical speed with a SPMD approach and I suspect it will do much better with just pure intrinsics. IPv4 just fit in a 128 bits register. There is no loop needed and you are actually fighting to get them to disappear when using a SPMD approach. This class of algorithm are likely going to fair a lot better with pure manual selection of instruction when you are already at the edge of what is doable.

## How this fits with `archsimd`

SPMD and `archsimd` are complementary, and this comparison is a concrete example of where the layers live:

- `archsimd` is the right tool when you want to pick exact instructions. It's honest about what it is: vector ops, one at a time, return values, explicit width.
- A loop-level construct like `go for` is the right tool when you want the compiler to choose width, generate the masked tail, hoist invariants, apply loop peeling and generating portable code -- without you writing any of it.

The two are not in competition. You could expect to use both in the same program: `archsimd` for the handful of kernels where intrinsic-level control is genuinely worth the source cost, and a loop-level construct for the long tail of reductions, scans, byte-classification loops, and per-pixel arithmetic where readability and portability matter more than picking instructions by hand.

The real performance available at the loop level is something a per-op intrinsic library structurally cannot reach. That is a useful argument for having both layers, not for replacing one with the other.

## Reproducing the comparison

- SPMD binaries: `PATH=$(pwd)/go/bin:$PATH GOEXPERIMENT=spmd ./tinygo/build/tinygo build -target=amd64-avx2 -o <out> test/integration/spmd/lo-<kernel>/main.go`.
- `lo/simd` binaries: `go test -c` in `test/bench/simd/`, then `go tool objdump` on the resulting test binary.
- Three-way driver: `test/e2e/spmd-benchmark-x86.sh` (scalar `lo` vs `lo/simd` vs SPMD).

The full report with the disassembly transcripts lives in [docs/spmd-vs-go-simd-intrinsics.md](https://github.com/Bluebugs/go-spmd/blob/main/docs/spmd-vs-go-simd-intrinsics.md) in the SPMD repository.

---

If you want the broader picture of where SPMD's speed comes from across all the workloads we have tested, the [results post](../spmd-results/) gathers the numbers. The [loop peeling post](../spmd-loop-peeling/) goes deeper on the single transformation that does most of the work, including in the Contains case above.
