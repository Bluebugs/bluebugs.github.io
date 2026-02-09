+++
date = '2025-07-12T14:30:00-07:00'
title = 'Cross-Lane Communication: When Lanes Need to Talk'
description = 'Understanding why and how SPMD programs coordinate data between execution lanes through base64 decoding'
featured_image = 'images/lakelouise.jpg'
featured_image_class = 'cover bg-center'
+++

## When Independent Lanes Aren't Enough

Most SPMD examples show lanes working independently—each processing its own data without communicating with neighbors. But real-world algorithms often require **cross-lane communication**: lanes must exchange data to solve the problem correctly. Base64 decoding demonstrates this problem, transforming groups of 4 ASCII characters into groups of 3 bytes through coordinated lane operations.

<!--more-->

This implementation draws inspiration from Miguel Young de la Sota's ["Designing a SIMD Algorithm from Scratch"](https://mcyoung.xyz/2023/11/27/simd-base64/), which explores the intricate challenges of building high-performance base64 decoders. Please go read his article if you want to really understand how and why this code work. I am just going to try in this article to show how it could be possible to do that in Go if we had a SPMD extension.

## The Problem: 4-to-3 Data Transformation

Base64 encoding converts every 3 bytes into 4 ASCII characters. Decoding reverses this: 4 ASCII characters become 3 bytes. This mismatch creates the core challenge—we can't simply process each character independently because the output structure differs from the input structure.

Let's look first at how we would iterate over all the data.

```go
// Package main demonstrates cross-lane communication in SPMD Go
// Based on: https://github.com/mcy/vb64/blob/main/src/simd.rs#L16-L144
package main

import (
 "lanes"
 "reduce"
)

func Decode(ascii []byte) ([]byte, bool) {
 if len(ascii) == 0 {
  return nil, true
 }
 if len(ascii)%4 != 0 {
  return nil, false // Base64 requires input length multiple of 4
 }

 decoded := make([]byte, 0, len(ascii)*3/4)
 pattern := outputPattern()

 go for _, v := range[4] ascii {
  decodedChunk, valid := decodeChunk(v, pattern)
  if !valid {
   return nil, false
  }
  decoded = append(decoded, decodedChunk...)
 }

 return decoded, true
}
```

The `range[4] ascii` syntax indicates we're processing 4 bytes at a time in groups. This explicit sizing is crucial—the algorithm's cross-lane operations depend on having lane counts that are multiples of 4 to maintain the correct 4-to-3 byte transformation pattern.

## The Three Core Cross-Lane Operations

Base64 decoding requires three fundamental cross-lane communication patterns. Let's understand each before seeing how they work together:

### 1. Swizzle: Parallel Table Lookups

```go
// Each lane indexes into a shared lookup table
offsetTable := []byte{255, 16, 19, 4, 191, 191, 185, 185}
offsets := lanes.Swizzle(lanes.From(offsetTable), hashes)
```

**What it does**: `lanes.Swizzle` allows each lane to access any position in a shared array based on its computed index. Lane 0 might read position 3, lane 1 might read position 6, etc.

**Why it's powerful**: This enables parallel table lookups where each lane can access different data simultaneously—like multiple hands reaching into different positions of the same toolbox at once.

### 2. Rotation: Neighboring Data Exchange

```go
// Lane N receives data from lane N-1
decodedChunks := shiftedLo | lanes.Rotate(shiftedHi, 1)
```

**What it does**: `lanes.Rotate` shifts data between adjacent lanes. With a rotation of 1, lane 0 gets data from lane 3, lane 1 gets data from lane 0, and so on.

**Why it's essential**: Base64's 6-to-8 bit conversion creates bit patterns that span lane boundaries. Each lane needs some bits from its neighbor to form complete bytes.

### 3. Output Pattern: Selective Extraction

```go
func outputPattern() lanes.Varying[uint8, 4] {
 var r lanes.Varying[uint8, 4]
 go for i := range[4] {
  r[i] = uint8(i + i/3) // Creates: [0,1,2,4]
 }
 return r
}

// Use pattern to select final bytes
output := lanes.Swizzle(decodedChunks, pattern)
```

**What it does**: The pattern `[0,1,2,4]` selects specific positions from the decoded data, skipping every fourth element. For larger lane counts, this extends to `[0,1,2,4,5,6,8,9,10,12,...]`.

**Why it's clever**: This elegant pattern automatically handles "contamination" from rotation operations that cross group boundaries—contaminated data lands in positions that get discarded anyway.

## The Complete Algorithm

Now let's see how these operations work together in the complete `decodeChunk` function:

```go
func decodeChunk(ascii lanes.Varying[byte, 4], pattern lanes.Varying[uint8, 4]) ([]byte, bool) {
 // Step 1: Perfect hash function for table indexing
 hashes := lanes.ShiftRight(ascii, 4) 
 if ascii == '/' {
  hashes += 1
 }

 // Step 2: Convert ASCII to 6-bit values via table lookup (Swizzle)
 offsetTable := []byte{255, 16, 19, 4, 191, 191, 185, 185}
 offsets := lanes.Swizzle(lanes.From(offsetTable), hashes)
 sextets := ascii + offsets

 // Step 3: Validate characters using parallel lookups (Swizzle + Reduction)
 loLUT := lanes.From([]byte{
  0b10101, 0b10001, 0b10001, 0b10001, 0b10001, 0b10001, 0b10001, 0b10001,
  0b10001, 0b10001, 0b10011, 0b11010, 0b11011, 0b11011, 0b11011, 0b11010,
 })
 hiLUT := lanes.From([]byte{
  0b10000, 0b10000, 0b00001, 0b00010, 0b00100, 0b01000, 0b00100, 0b01000,
  0b10000, 0b10000, 0b10000, 0b10000, 0b10000, 0b10000, 0b10000, 0b10000,
 })
 
 lo := lanes.Swizzle(loLUT, ascii&0x0f)
 hi := lanes.Swizzle(hiLUT, lanes.ShiftRight(ascii, 4))
 valid := reduce.Or(lo&hi) == 0

 // Step 4: Pack 6-bit values into bytes with cross-lane coordination (Rotation)
 shiftPattern := lanes.From([]uint16{2, 4, 6, 8})
 shifted := lanes.ShiftLeft(sextets, shiftPattern)
 
 shiftedLo := lanes.Varying[byte, 4](shifted)
 shiftedHi := lanes.Varying[byte, 4](lanes.ShiftRight(shifted, 8))
 decodedChunks := shiftedLo | lanes.Rotate(shiftedHi, 1)

 // Step 5: Extract final 3 bytes using output pattern (Swizzle)
 output := lanes.Swizzle(decodedChunks, pattern)
 return []byte(output), valid
}
```

## Why Multiple-of-4 Lane Counts Matter

Notice the explicit `lanes.Varying[T, 4]` types throughout the code. This isn't arbitrary—base64's 4-to-3 conversion requires lane counts that are multiples of 4. The cross-lane operations work correctly with any multiple of 4 because:

The **Output patterns** maintain the 4:3 ratio across lane groups, discarding the fourth byte that could be "contaminated" by rotation or shift operation. It is this pattern that make this algorithm work for any multiple of 4 lanes.

By specifying `lanes.Varying[T, 4]`, we tell the compiler: "Process data in groups of 4. If the hardware has 8 lanes, run two groups in parallel. If it has 16 lanes, run four groups simultaneously. If it has fewer than 4 lanes, unroll the loop and interleave operations to ensure each step has the required 4 elements ready for processing.

This allow supporting all hardware with the same code. Relying on compiler to provide portability, but also readability and maintainability while not sacrificing performance.

## The Cost of Communication

Cross-lane operations are expensive compared to independent lane operations:

- **Simple arithmetic**: Each lane operates independently—very fast
- **Shuffle/Swizzle**: Lanes access arbitrary positions—moderately expensive when staying in register, get more expensive when accessing random memory
- **Rotation/Shift**: Each lane still operates independently as it is just directed to land in another lane-very fast
- **Reduction**: All-to-one communication—can be expensive depending on operation

However, these costs enable algorithms impossible with purely independent processing. Base64 decoding with cross-lane communication can be significantly faster than scalar alternatives. The edge case of generating code for computer with no SIMD will be interesting to see if it impact the performance compared to current code.

## The Complexity Question

Base64 decoding demonstrates how cross-lane operations can replace hand-written assembly with portable Go code. But this raises a fundamental question: **Is the added complexity worth it?**

### What We Gain

- **Portability**: Same algorithm works across different SIMD widths and architectures
- **Performance**: Potentially significant speedups for data transformation algorithms
- **Maintainability**: No platform-specific assembly to maintain

### What We Lose

- **Simplicity**: Code becomes inherently harder to review and understand
- **Cognitive load**: Developers must understand lane interactions, not just individual operations
- **Debugging complexity**: Cross-lane bugs are more subtle than simple arithmetic errors

### The Minimal Alternative

Perhaps the real question is whether we need the full suite of cross-lane operations, or if **reduction alone** would cover the majority of practical use cases:

```go
// Simple parallel processing with reduction
sum := reduce.Add(data * coefficients)
anyInvalid := reduce.Or(validation_results)
maximum := reduce.Max(lane_values)
```

Reduction operations are:

- **Easier to understand**: All-to-one communication is conceptually simpler
- **Broadly applicable**: Many algorithms only need to combine results, not exchange data
- **Less error-prone**: No risk of rotation contamination or swizzle index errors

### The Design Decision

The base64 example shows what's possible with full cross-lane communication, but it also reveals the algorithmic complexity cost. For a Go SPMD extension, the choice might be:

1. **Full suite** (swizzle, rotation, reduction): Maximum capability, maximum complexity
2. **Reduction only**: Simpler mental model, covers many common patterns
3. **Gradual introduction**: Start with reduction, add others based on demonstrated need

The question isn't just technical—it's about whether Go developers would adopt and correctly use these more complex operations, or if the cognitive overhead outweighs the performance benefits.

**[View Complete Source Code](../../examples/base64-decoder/)** - Full implementation with usage examples and detailed algorithm explanations.

*Implementation inspired by Miguel Young de la Sota's excellent analysis in ["Designing a SIMD Algorithm from Scratch"](https://mcyoung.xyz/2023/11/27/simd-base64/), adapted for hypothetical SPMD Go extension.*

---

**Previous in series:** [What if? Practical parallel data.](../practical-vector/) - Learn basic SPMD patterns with simple string operations.

**Next in series:** [Putting It All Together](../go-spmd-ipv4-parser/) - See SPMD concepts applied to real-world IPv4 parsing performance.
