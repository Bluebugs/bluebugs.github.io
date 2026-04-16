+++
date = '2026-04-15T10:07:00-07:00'
draft = true
title = 'How the Compiler Knows Your Load Is Contiguous'
description = 'The most important backend optimization in SPMD: recognizing contiguous memory access through ChangeType and BinOp chains'
featured_image = 'images/lakelouise.jpg'
featured_image_class = 'cover bg-center'
+++

The single most important question the SPMD backend asks is: **"is this memory access contiguous?"** The answer determines whether your loop runs at vector speed or crawls through gather/scatter. This article is about the compiler pass that answers that question, and why it was worth more than every other optimization we built combined.

<!--more-->

## Why Contiguous Access Is the Whole Game

When you write a `go for` loop that reads from a slice, the compiler has two choices for how to load the data into SIMD registers.

If the access is **contiguous** — elements at addresses `base`, `base+1`, `base+2`, `base+3` — it emits a single vector load instruction. On WASM that is `v128.load`. On x86 that is `vmovdqu`. One instruction, one memory operation, all lanes filled.

If the access is **not contiguous** — the lanes point to scattered locations in memory — the compiler must fall back to gather/scatter. On hardware that supports it, that is still one instruction but at 2-4x the latency. On WASM, which has no gather instruction, it means four separate scalar loads and an `insertelement` per lane. On AVX2 with byte-width data, it means 32 separate loads.

The performance difference is 4-8x. Not a rounding error — a categorical change. In our benchmarks, every example that achieves 5x or better has contiguous access in its hot loop. Every example that underperforms has a non-contiguous access somewhere the compiler could not resolve.

This is why the contiguous recognizer matters more than loop peeling, more than mask elimination, more than pattern detection. Those optimizations make a fast loop faster. The contiguous recognizer determines whether the loop is fast at all.

## Why Recognizing Contiguous Access Is Non-Trivial

If you have never looked at Go's SSA form, you might expect the index expression inside a range loop to be straightforward — just the iteration variable, maybe plus a constant. In practice, it is not.

Go's `range` over integers produces SSA that widens or narrows the iteration variable through `ChangeType` nodes. A `range 16` loop over `int` on a 64-bit target creates a phi of type `int`, but the slice indexing may need `int32` or `int64`, and Go's SSA inserts explicit type conversions. The index expression arrives at the backend looking like `ChangeType(i64 -> i32)(phi)` rather than just `phi`.

Then there is arithmetic. A Mandelbrot renderer indexes its output buffer as `output[y*width + x]`, where `y*width` is uniform (same across all lanes) and `x` is the SPMD loop iterator. The SSA for this is a `BinOp ADD` with the iter phi on one side and a scalar expression on the other. But ADD is commutative — the iter phi can be on either side. And the scalar side may itself contain further `ChangeType` wrappers.

Constant folding makes things worse. The compiler may fold `iter + 0` into just `iter`, but it may also fold a chain of additions so that the iter phi ends up buried two or three levels deep in the expression tree.

A naive pattern matcher that just checks "is this the iter phi?" catches perhaps 40% of contiguous accesses. The rest look non-contiguous and fall back to scatter. Our benchmark numbers reflected this — until we built the full recognizer.

## The Recognizer

The core function is `spmdAnalyzeContiguousIndex` in `tinygo/compiler/spmd.go`. It takes an SSA index value and returns either "yes, this is contiguous with respect to a known SPMD loop, and here is the scalar base offset" or "no."

```go
// spmd.go:4833
func (b *builder) spmdAnalyzeContiguousIndex(
    index ssa.Value,
) (*spmdActiveLoop, llvm.Value, bool) {
    // Fast path: direct iter phi match.
    if loop, ok := b.spmdLoopState.activeLoops[index]; ok {
        return loop, loop.scalarIterVal, true
    }

    // Check BinOp ADD: scalar + iter or iter + scalar.
    binop, ok := index.(*ssa.BinOp)
    if !ok || binop.Op != token.ADD {
        return nil, llvm.Value{}, false
    }

    // Try both sides (commutativity).
    if loop, ok := b.spmdLoopState.activeLoops[binop.X]; ok {
        if scalarVal, ok := b.spmdUnwrapScalar(binop.Y); ok {
            scalarBase := b.CreateAdd(scalarVal, loop.scalarIterVal,
                "spmd.contiguous.base")
            return loop, scalarBase, true
        }
    }
    if loop, ok := b.spmdLoopState.activeLoops[binop.Y]; ok {
        if scalarVal, ok := b.spmdUnwrapScalar(binop.X); ok {
            scalarBase := b.CreateAdd(scalarVal, loop.scalarIterVal,
                "spmd.contiguous.base")
            return loop, scalarBase, true
        }
    }

    return nil, llvm.Value{}, false
}
```

The logic is: first check if the index *is* the iter phi directly. If not, check if it is an ADD where one side is the iter phi and the other side is scalar. Try both sides of the ADD because the SSA does not guarantee operand order.

The companion function `spmdUnwrapScalar` handles the scalar side:

```go
// spmd.go:4870
func (b *builder) spmdUnwrapScalar(v ssa.Value) (llvm.Value, bool) {
    unwrapped := v
    for {
        if ct, ok := unwrapped.(*ssa.ChangeType); ok {
            unwrapped = ct.X
        } else {
            break
        }
    }

    if _, isVec := b.spmdValueOverride[unwrapped]; isVec {
        return llvm.Value{}, false
    }

    scalarVal := b.getValue(unwrapped, getPos(unwrapped))
    if scalarVal.Type().TypeKind() == llvm.VectorTypeKind {
        return llvm.Value{}, false
    }
    return scalarVal, true
}
```

It peels `ChangeType` chains — the nodes that Go's SSA inserts for type widening and narrowing — then checks whether the underlying value is truly scalar (not in the `spmdValueOverride` map, which tracks values that have been promoted to vectors). If it is scalar, it returns the LLVM value. If the unwrapped value turns out to be varying after all (for example, a `ChangeType` wrapping the iter phi itself), it returns false.

Note the careful distinction: `ChangeType` is peeled because it is a pure type annotation that does not change the value. `Convert` is *not* peeled because it may involve actual numeric conversion (truncation, sign extension) that changes the bit pattern. Getting this wrong means silently miscompiling — treating a non-contiguous access as contiguous and loading garbage into lanes.

## The 38% Improvement

Here is the part that surprised us. The original contiguous recognizer handled the iter-phi-directly case and the `BinOp ADD` case, but it did not peel `ChangeType` on the scalar side. When Go's range-over-int produced `ChangeType(i32 -> i64)(iter_phi + scalar_offset)`, the recognizer saw a `ChangeType` at the top, did not find the iter phi directly, and gave up. The access fell through to scatter.

Adding `spmdUnwrapScalar` — the function shown above, roughly 20 lines of code — gave a **38% speedup** on benchmarks that involved contiguous stores. Not 38% on one microbenchmark. 38% on real code like hex-encode and Mandelbrot where the stores were contiguous but the recognizer was not seeing them.

That means more than a third of the contiguous stores in those benchmarks were silently falling back to gather/scatter before we added one additional unwrap case. Each unrecognized contiguous access was costing 4-8x on that individual memory operation, and memory operations dominate the hot loop.

One recognizer extension. Twenty lines. 38%.

## Lessons for Other SIMD Compilers

If you are building a vectorizing compiler for any managed-memory language, here are the principles we extracted:

**Walk both sides of every ADD.** Commutativity means the iter phi can be on either side. Do not assume operand order.

**Peel all view-only type conversions.** `ChangeType`, pointer bitcasts, zero-cost widenings — anything that does not change the underlying value. But stop at genuine numeric conversions.

**Trace through phis within the loop body.** The phi may refer to a value computed in a previous iteration. Follow the chain, but only within the loop — do not chase phis across loop boundaries.

**Treat unrecognized as non-contiguous.** Do not try to handle "mostly contiguous" or "contiguous with gaps." Either the recognizer can prove full contiguity or the access falls back to scatter. Partial solutions create partial correctness bugs.

And the overarching lesson: **invest disproportionately here.** Every percentage point of recognizer coverage is worth more than any other compiler work in this domain. The contiguous/non-contiguous boundary is where most of the benchmark delta lives. A slightly better recognizer beats a much better register allocator, a much better instruction selector, or a much better loop unroller. Those optimizations make fast code faster. The contiguous recognizer decides whether the code is fast in the first place.

---

**Further reading:** [How SPMD Lives in the Compiler](../spmd-compiler-lessons/) covers the broader compiler architecture. [Pattern Matching Beats Hand-Written SIMD](../spmd-pattern-matching/) shows another case where a simple compiler recognizer outperformed explicit intrinsics.

*This article is part of a series on SPMD for Go. The proof of concept is open source at [github.com/nicedispatcher/SPMD](https://github.com/nicedispatcher/SPMD).*
