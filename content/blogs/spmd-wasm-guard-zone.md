+++
date = '2026-04-15T10:06:00-07:00'
draft = true
title = '16 Bytes That Saved a Thousand Branches'
description = 'The cheapest optimization in our SPMD proof of concept: a WASM linear memory guard zone for safe vector overreads'
featured_image = 'images/lakelouise.jpg'
featured_image_class = 'cover bg-center'
+++

The cheapest optimization in our entire SPMD proof of concept cost 16 bytes of memory and eliminated an entire class of branch-heavy fallback code.

<!--more-->

This is part of a [series on SPMD for Go](../go-data-parallelism/). If you want the full picture -- what SPMD is, why it belongs in Go, and what performance it delivers -- start there. This article is about one small trick that made the whole thing practical on WebAssembly.

## The problem

WebAssembly bounds-checks every memory access. A `v128.load` that reads even one byte past the end of linear memory **traps** -- a hard fault, not undefined behavior. There is no "it's fine, those bytes are just garbage." The runtime kills your program.

This matters for SPMD because every `go for` loop over a `[]byte` has a tail. If your slice has 19 bytes and your SIMD register holds 16, the last iteration needs to load bytes 16 through 18 -- but `v128.load` will read bytes 16 through 31. Thirteen bytes past the end of valid data. Trap.

The conventional solutions are all expensive:

- **Bounce buffer.** Copy the tail bytes into a scratch buffer, load from there. Extra `memcpy` on every tail iteration.
- **Scalar fallback.** Load one byte at a time for the tail. Defeats the purpose of SIMD entirely.
- **`v128.load_lane`.** Load only the valid bytes one lane at a time. Not universally available, and slower than a full vector load where it is.

Every one of these adds branches, complexity, and latency to the hottest path in the program.

## The trick

Reserve 16 bytes at the top of WASM linear memory. Never allocate them. Never write to them. Just leave them there.

From `tinygo/src/runtime/arch_tinygowasm.go`:

```go
// heapEnd is the current memory length in bytes, minus the SIMD guard zone.
//
// Reserve 16 bytes at the top of linear memory as a SIMD guard zone.
// This guarantees that v128.load from any heap-allocated pointer will
// not trap, even if it reads up to 15 bytes beyond the allocation.
// Cost: 16 bytes out of minimum 64KB. Used by createSPMDVectorFromMemory
// to do overread+mask instead of memset+memcpy+v128.load bounce buffer.
heapEnd = uintptr(wasm_memory_size(wasmMemoryIndex)*wasmPageSize) - 16
```

One line. The heap allocator subtracts 16 from the memory size. Those 16 bytes sit at the top of linear memory, always valid for reads, never handed out by `malloc`. Cost: 16 bytes out of a minimum 64KB page. That is 0.02% of the smallest possible WASM memory.

Now every `v128.load` from any heap-allocated pointer is safe. The overread lands in the guard zone. The bytes are garbage, but we are about to deal with that.

## The overread + mask sequence

With the guard zone in place, the tail load becomes four instructions with no branches. From `createSPMDVectorFromMemoryMasked` in `tinygo/compiler/spmd.go`:

```go
func (b *builder) createSPMDVectorFromMemoryMasked(
    dataPtr, length llvm.Value, lanes int,
) llvm.Value {
    // Load all lanes. Guard zone guarantees no trap.
    rawLoad := b.CreateLoad(vecType, dataPtr, "vfm.raw")

    // Build lane index constant [0, 1, 2, ..., lanes-1].
    indicesVec := llvm.ConstVector(indices, false)

    // Splat length across all lanes.
    lenSplat := b.splatScalar(lenI8, vecType)

    // Active lanes get 0xFF, inactive lanes get 0x00.
    mask := b.CreateICmp(llvm.IntULT, indicesVec, lenSplat, "vfm.mask")
    maskExt := b.CreateSExt(mask, vecType, "vfm.mask.ext")

    return b.CreateAnd(rawLoad, maskExt, "vfm.masked")
}
```

The sequence in pseudocode:

```
raw     = v128.load(dataPtr)              // safe: guard zone prevents trap
indices = const [0, 1, 2, ..., 15]        // lane index vector
mask    = icmp ult indices, splat(length)  // 0xFF for valid, 0x00 for garbage
result  = and(raw, mask)                  // zero the overread bytes
```

Four instructions. No branches. No bounce buffer. No scalar fallback. The `sext` produces `0xFF` for every lane whose index is less than the valid length, and `0x00` for the rest. The `and` zeroes the garbage bytes cleanly.

For the IPv4 parser and the base64 decoder's remainder handling, this was the difference between a working fast path and a slow fallback on every input that is not a multiple of 16 bytes.

## The x86 variant

On x86-64, WASM's bounds-checking model does not apply -- native memory has OS-managed guard pages. But a `vmovdqu` that straddles a page boundary can still fault if the next page is unmapped.

The compiler checks whether the pointer is within 16 bytes of a 4096-byte page boundary:

```
pageOff = ptr & 0xFFF
nearEnd = pageOff > 0xFF0
```

If `nearEnd` is false -- roughly 99.6% of the time -- a single `vmovdqu` suffices. The garbage bytes are acceptable because callers trim via the execution mask, not via vector content. For the rare case where the pointer sits in the last 16 bytes of a page, the code falls through to the same overread + mask sequence.

Two paths, one branch, and the branch is almost never taken.

## Closing

The guard zone is the kind of optimization that looks obvious in hindsight: reserve a few bytes of padding so the hardware never faults on an overread. But "obvious in hindsight" and "obvious before you've spent a week debugging bounce-buffer codegen" are different things.

Any WASM runtime that supports SIMD should do this. If WASM ever gains 256-bit or 512-bit vector extensions, scale the guard zone to match the register width -- 32 or 64 bytes instead of 16. The cost is negligible. The codegen simplification is substantial.

---

**Further reading:** [Data Parallelism: simpler solution for Golang?](../go-data-parallelism/) for the full SPMD pitch. The compiler internals article (forthcoming) covers the predicated SSA architecture that makes this and every other optimization in the PoC possible.
