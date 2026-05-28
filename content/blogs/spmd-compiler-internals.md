+++
date = '2026-04-15T10:02:00-07:00'
draft = false
title = 'How SPMD Lives in the Compiler: Lessons from Building It'
description = 'The mask-stack detour, predicated SSA, and why SPMD has to live at the heart of the compiler'
featured_image = 'images/mountain-6.jpg'
featured_image_class = 'cover bg-center'
tags = ['SPMD', 'compiler', 'SSA', 'LLVM']
+++

We added a way to express data parallelism in idiomatic Go. Earlier discussions around this space often stalled on how it would actually work in the compiler. A working proof of concept that compiles `go for` loops to WASM SIMD128, x86 SSE, and x86 AVX2, with end-to-end tests passing and a base64 decoder reaching ~77% of simdutf C++ throughput, is a better answer than another round of speculation. The goal here is to make the implementation strategy concrete. Along the way we learned one lesson the hard way: **SPMD is a compiler feature that has to live at the heart of the SSA form.** Everything else follows from that.

This article is for compiler engineers. If you want to see the benchmarks and the short version, read [the overview](../spmd-results/). If you want to write SPMD Go code, the [practical guide](../writing-spmd-go/) is for you. Here, we talk about what we built inside the compiler, what we got wrong, and what we would do differently.

<!--more-->

## The mask-stack detour

The proof of concept already maintained two forked repositories: a Go fork for the frontend (lexer, parser, type checker) and a TinyGo fork for the LLVM backend. Adding a third fork -- a patched copy of `golang.org/x/tools/go/ssa` -- felt like one fork too many. We had already spent time on changes to the main Go compiler SSA that turned out not to matter for this path, and there is always the temptation to believe SPMD can be bolted on outside the compiler. If that were true, we should have been able to stay out of `go/ssa` entirely.

So we spent real effort trying to do everything in TinyGo's backend without modifying the SSA layer. The reasoning was straightforward: TinyGo already consumes `go/ssa` as a read-only input. If we could reconstruct varying-ness, control-flow masks, and predication from the SSA structure alone -- by analyzing the blocks during LLVM codegen -- we would not need to fork `go/ssa`.

That is where the mask stack came from. The TinyGo backend would walk the SSA blocks, detect varying conditions by inspecting operand types, push a mask when entering a varying scope, pop it when leaving, and consult the top of the stack at every memory operation to decide how to mask the load or store. For a `go for` with a single varying `if`/`else` inside, it worked.

Then we added varying `switch`. Then `&&`/`||` chains. Then `break` under varying conditions. Then inner scalar loops that should *not* be predicated. Each new control-flow pattern required new push/pop sites sprinkled through the block walker. The walker was doing double duty: traversing LLVM blocks for codegen (which happens in a specific order determined by LLVM's layout) while simultaneously tracking Go-level control-flow semantics (which depends on the Go source structure, not the LLVM block order). Those two concerns are fundamentally different, and entangling them was a steady source of bugs.

Every bug report came down to "the mask was wrong on this specific code path." The mask was too wide, or too narrow, or popped at the wrong time, or never pushed because the varying condition was detected too late. The walker could not reliably reconstruct information that the SSA should have carried in the first place.

We eventually accepted that the third fork was necessary. We created `x-tools-spmd/` -- a patched copy of `golang.org/x/tools@v0.30.0` -- and added SPMD metadata and transforms directly into `go/ssa`. About 2,000 lines of additions across [`spmd_loop.go`](https://github.com/Bluebugs/tools/blob/spmd/go/ssa/spmd_loop.go), [`spmd_varying.go`](https://github.com/Bluebugs/tools/blob/spmd/go/ssa/spmd_varying.go), [`spmd_peel.go`](https://github.com/Bluebugs/tools/blob/spmd/go/ssa/spmd_peel.go), and [`spmd_predicate.go`](https://github.com/Bluebugs/tools/blob/spmd/go/ssa/spmd_predicate.go). On 2026-03-05 we deleted approximately 330 lines of mask-stack code from TinyGo. All memory-operation masking moved to explicit SSA-level masks on `SPMDLoad`/`SPMDStore` instructions, populated by a predication pass that runs before the backend ever sees the code.

The result was immediate: that entire class of mask-stack bugs largely disappeared. New control-flow cases -- divergent inner loops, boolean chains, varying switch -- landed without the old style of regressions because the SSA already encoded the correct mask at every point of use. The block walker became trivial: each memory operation carries its mask; the walker emits it.

**SPMD is a compiler feature that has to live at the heart of the SSA form. You cannot bolt it on as a backend analysis.** The mask semantics are a property of the program's control flow, and they must be resolved where control flow is represented -- in the SSA -- not reconstructed during a traversal of a different IR. The bugs are proportional to the gap between what the SSA knows and what the backend needs. Close the gap at the source.

## Predicated SSA

Once we accepted the fork, the design fell into place quickly. We do not need a zoo of vector opcodes. We need **three** SPMD-aware SSA instructions and **four** metadata structures.

The instructions are `SPMDLoad`, `SPMDStore`, and `SPMDSelect`. Each carries an explicit mask operand that says which lanes are active. `SPMDSelect` is the workhorse: it replaces every phi node at a varying-control-flow merge point, choosing per-lane between the "then" value and the "else" value based on the varying condition.

The metadata structures tell the predication pass where varying control flow begins and ends:

- **`SPMDLoopInfo`** on `Function` (defined in [`go/ssa/ssa.go`](https://github.com/Bluebugs/tools/blob/spmd/go/ssa/ssa.go)). Describes each `go for` loop: entry, body, loop, and done blocks, the iteration phi, the bound value, the lane count, and whether this is a range-over-slice. After loop peeling, it also carries the main body, tail check, tail body, and trampoline blocks.
- **`If.IsVarying`** flag. Marks a conditional branch as dependent on a varying value. Set during SSA construction using [`exprHasSPMDType()`](https://github.com/Bluebugs/tools/blob/spmd/go/ssa/spmd_varying.go), an AST-level helper that walks the condition expression and returns true if any subexpression has a varying type.
- **`SPMDSwitchChain`**. Groups the chain of `If` instructions that Go's switch lowering produces from a single varying `switch` statement. Stores the tag value, the list of case `If`s, the default block, and the join block.
- **`SPMDBooleanChain`**. Captures the block structure of a varying `&&` or `||` expression. Records the operator, the list of short-circuit blocks, and the final then/else blocks.

With those four pieces, the predication pass can walk any Go function, linearize its varying control flow into masked selects, and hand the backend a CFG where every vector-relevant decision is explicit. The transform for a varying `if v { A } else { B }` with starting mask `m` is: execute `A` under `m & v`, execute `B` under `m & ~v`, replace the merge-point phi with `SPMDSelect(v, a_val, b_val)`. Varying switch fans out into per-case masks. Varying `&&`/`||` follows short-circuit semantics with mask narrowing.

After predication, the call sequence in [`go/ssa`](https://github.com/Bluebugs/tools/tree/spmd/go/ssa) is:

```
optimizeBlocks
  -> resolveSPMDSwitchChains
  -> resolveSPMDBooleanChains
  -> peelSPMDLoops
  -> spmdMergeRedundantStores
  -> predicateSPMDScope / predicateSPMDFuncBody
```

TinyGo's job shrinks to "consume the SSA mechanically." That is the whole point of doing predication at the SSA level.

All this metadata holds pointers into the CFG. Then `optimizeBlocks()` runs -- it deletes empty blocks, merges straight-line chains, renumbers everything. The metadata pointers go stale. The fix is two resolution passes (`resolveSPMDSwitchChains`, `resolveSPMDBooleanChains`) that run immediately after `optimizeBlocks` and re-discover which blocks correspond to which logical roles. If you add metadata that holds block pointers to any SSA, add a resolver hook that runs after every optimization pass. Design it up front or you will rediscover this bug the hard way.

## Where it goes for upstream Go

We prototyped in `golang.org/x/tools/go/ssa` because that is what TinyGo consumes. For upstream Go, the same patterns go into `cmd/compile/internal/ssa` -- the SSA that the Go compiler already uses for optimization and codegen.

In Phase 1 of the PoC, we added 42 vector opcodes to [`cmd/compile/internal/ssa/_gen/genericOps.go`](https://github.com/Bluebugs/go/blob/spmd/src/cmd/compile/internal/ssa/_gen/genericOps.go). They were a flat list: `VecAdd`, `VecSub`, `VecMul`, one per arithmetic operation per type width. In the PoC, those opcodes were never exercised -- TinyGo uses `go/ssa`, not `cmd/compile` internals, so all the vectorization work that actually produced results was developed in `x-tools-spmd/`.

The 42 opcodes were the wrong shape but the right location. A flat list of vector ops is how you would design a SIMD intrinsics library, not how you would design a compiler. The structured approach that worked -- `SPMDLoopInfo`, explicit-mask `SPMDLoad`/`SPMDStore`/`SPMDSelect`, `If.IsVarying` metadata, predication transforms, SSA-level loop peeling -- is what should replace them. An upstream implementation should rework `cmd/compile/internal/ssa` to carry these patterns natively.

## The `lanes.Varying[T]` type

`lanes.Varying[T]` is a compiler-magic generic. It parses as a plain generic index expression -- `PkgName.TypeName[TypeArg]` -- but the type checker special-cases it: when resolving the index expression, if the callee is the magic type `lanes.Varying`, it dispatches to the SPMD code path and synthesizes an `SPMDType`. Tooling works unchanged; the grammar is unchanged; `gopls` sees a generic invocation and does the right thing.

We considered a `varying` keyword. We rejected it because any new reserved word breaks old Go code that uses `varying` as an identifier. It also requires lexer changes that ripple into every downstream tool -- `gopls`, `goimports`, `vet`, tree-sitter grammars, syntax highlighters, generator authors. The cost falls on people who do not care about SPMD.

Every SPMD-specific frontend rule lives in a file whose name ends with `_ext_spmd.go`. This makes the SPMD work trivially separable from the rest of the type checker and makes understanding it much easier. Examples in `cmd/compile/internal/types2/`: [`typexpr_ext_spmd.go`](https://github.com/Bluebugs/go/blob/spmd/src/cmd/compile/internal/types2/typexpr_ext_spmd.go) for the entry point that catches `lanes.Varying[T]` index expressions, [`stmt_ext_spmd.go`](https://github.com/Bluebugs/go/blob/spmd/src/cmd/compile/internal/types2/stmt_ext_spmd.go) for return/break rules, [`call_ext_spmd.go`](https://github.com/Bluebugs/go/blob/spmd/src/cmd/compile/internal/types2/call_ext_spmd.go) for the public API restriction.

Every one of these files is mirrored in [`go/types/`](https://github.com/Bluebugs/go/tree/spmd/src/go/types) with identical logic. This is the single most painful thing about working in Go's type checker today: `cmd/compile/internal/types2` and `go/types` are two near-duplicate trees. Every SPMD rule had to be written twice, reviewed twice, tested twice. If you are planning to upstream this work, unify `types2` and `go/types` first, or accept that every SPMD contribution is a double-write.

The type lattice produced surprises we had to retrofit painfully:

- **`&Varying[T]` produces `Varying[*T]`.** Taking the address of a varying value gives a varying pointer. Dereferencing gives it back: `*Varying[*T]` produces `Varying[T]`. This required a four-layer fix across [`types2`](https://github.com/Bluebugs/go/tree/spmd/src/cmd/compile/internal/types2), [`go/types`](https://github.com/Bluebugs/go/tree/spmd/src/go/types), [`go/ssa`](https://github.com/Bluebugs/tools/tree/spmd/go/ssa), and [`tinygo/compiler/spmd.go`](https://github.com/Bluebugs/tinygo/blob/spmd/compiler/spmd.go) for per-lane GEP expansion.
- **`Varying[[N]T][i]` produces `Varying[T]`.** Indexing a varying fixed-size array returns a varying element.
- **`*Varying[Struct].Field` produces `Varying[FieldType]`.** Field access through a pointer to a varying struct.

Varying is a functor over types. The full lattice -- pointers, arrays, structs, slices-of -- should be built up front. Each one we added late cost time propagating through four codebases.

## Scalar fallback as correctness oracle

The `-simd=false` flag makes `SIMDRegisterSize()` report 1 byte. Every lane count becomes 1. Every "vector" type becomes a scalar. Every `SPMDSelect` becomes a plain `select`. In scalar fallback, an SPMD program and its non-SPMD equivalent should produce byte-identical output.

Dual-mode E2E testing in [`test/e2e/spmd-e2e-test.sh`](https://github.com/Bluebugs/go-spmd/blob/main/test/e2e/spmd-e2e-test.sh) builds every example twice -- once with `-simd=true`, once with `-simd=false` -- and diffs the output. Any divergence is a bug.

Building scalar mode exposed five categories of assumption bugs:

1. `vectorToArray` and `arrayToVector` had paths that assumed `laneCount > 1` and produced malformed LLVM types at lane count 1.
2. `MakeInterface` on a varying value assumed a vector layout; at lane count 1, the "vector" is a scalar and the layout differs.
3. `splatScalar` optimized for lane count 1 by returning the scalar directly, which broke type matching downstream.
4. `reduce.Add` and friends matched by a naming convention that broke when scalar mode inlined differently.

Each of these was a five-minute fix once found. All were hidden until scalar mode ran. Ship an SPMD compiler with a scalar fallback mode. It's the only automated correctness check that catches entire classes of bugs that pass under SIMD and fail under 1-lane execution.

## What we would do differently

**Unify `types2` and `go/types`.** The double-write cost us more time than any single technical decision. Every type-system extension was implemented twice, debugged twice, tested twice. We eventually gave up on making every pass elegant and settled for getting the behavior correct, even when that meant leaving some logic in later phases than it should have lived. There is clearly cleanup work left there. It was not an ideal decision, but it was a real tradeoff in a time-boxed PoC.

**First-class mask width in the type system.** Two `go for` loops in the same function can have different lane counts -- one iterating `int32` (4-wide on SSE), another iterating `byte` (16-wide on SSE). Their masks have different LLVM types. We handled this with deferred mask type resolution at materialization points. It works but it's ugly. A cleaner design would make mask width part of the SPMD type system so that `Varying[mask[N]]` carries its N. That has to be done with the implied decision that the `go for` loops drive the final lane count inside a loop.

**A real calling convention for the mask parameter.** In the PoC, the current mask is a synthetic parameter shoved into the front of every SPMD function's argument list. In a real implementation, debuggers, profilers, FFI bridges, and reflection should see it explicitly. It deserves a calling convention, not a hack.

**`iota` for `lanes.Index()`.** The SPMD loop variable is morally `iota` promoted to varying form. If `iota` were generalized to work inside `go for` -- producing a compile-time constant vector `[0, 1, 2, ..., N-1]` -- the API would be cleaner and the mental model more Go-native.

**A smaller [`compiler/spmd.go`](https://github.com/Bluebugs/tinygo/blob/spmd/compiler/spmd.go).** The file grew to approximately 9,000 lines. It should have been seven files of 1,200 lines each: `spmd_loop.go`, `spmd_memory.go`, `spmd_masks.go`, `spmd_patterns_x86.go`, `spmd_patterns_wasm.go`, `spmd_builtins.go`, `spmd_lowering.go`. Commit to a file-per-concern structure from day one.

**Work on `go/ir`, `go/ssa` and `x-tools/ssa` from day one.** The mask-stack detour cost us months. If we had accepted the three-fork maintenance burden early and built predication at the SSA level from the start, the total project time would have been shorter. The bugs were proportional to the gap between what the SSA knew and what the backend needed. Close the gap at the source.

Also we need a better name. I am bad at naming things. `goroutine` is nice and understandable way to describe light thread. So what should be the name of a SPMD for loop, this little `go for`... I am so close to call it a gopher loop :-D Yes, my jokes are not much better than my sense of naming. Compiler work might be easier.

---

For deeper dives into specific compiler mechanisms touched on above:

- [Loop Peeling: Where Most of the Speed Comes From](../spmd-loop-peeling/) -- the single highest-leverage optimization, and how the SSA-level split works.
- [How the Compiler Knows Your Load Is Contiguous](../spmd-contiguous-analysis/) -- the analysis that turns per-lane gather into a single vector load.
- [Byte Iteration at 32 Lanes: The Decomposed Index Path](../spmd-decomposed-index/) -- the representation choice that makes byte-granular iteration tractable at wide SIMD widths.
- [16 Bytes That Saved a Thousand Branches](../spmd-wasm-guard-zone/) -- the WASM linear-memory guard zone that eliminates bounce-buffer overhead on tail loads.

---

*This is part of a series on the SPMD-for-Go proof of concept. For the benchmark numbers and live demos, see [SPMD for Go: What If Your Loops Were 9x Faster?](../spmd-results/). For the story of how pattern detection outperformed explicit SIMD primitives, read [Pattern Matching Outperformed Hand-Written SIMD](../spmd-pattern-matching/).*
