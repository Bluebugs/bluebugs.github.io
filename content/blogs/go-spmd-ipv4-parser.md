---
title: "Putting It All Together"
date: 2025-07-13T14:00:00-07:00
description: "Fast IPv4 Parsing with SPMD Go"
featured_image: 'images/mountain-12.jpg'
featured_image_class: 'cover bg-center'
tags: ["golang", "performance", "networking", "SIMD", "SPMD"]
---

> [!WARNING]
> **Historical note.** This post is a thought experiment that predates the actual TinyGo SPMD compiler. Most of the building blocks now exist, but several specific claims in this post no longer match the working compiler: writing into a uniform `[16]bool` array from inside a `go for` (`dotMask[i] = c == '.'`) is a per-lane scatter that the compiler treats as an anti-pattern (lane-count-dependent and not portable across SIMD widths); incrementing a uniform `dotMaskTotal` from inside a varying conditional is also lane-count-dependent; the portable form is `reduce.Add(boolToInt(varyingDot))`. `reduce.FindFirstSet` is currently a stub. `reduce.Mask` returns a bitmask but the shape (16-bit vs wider) depends on the active lane count, not the hardware register width. The IPv4 parser was implemented and benchmarked on x86-64; measured throughput plateaued ~0.58x of scalar because of an inherent SPMD overhead for non loop scenario. For the patterns that actually deliver, see [Writing SPMD Go](../writing-spmd-go/) and [SPMD Results](../spmd-results/).

Network address parsing is everywhere in Go applications, yet the standard library processes strings character by character. This post combines SPMD concepts from our [previous](../practical-vector/) [blogs](../cross-lane-communication/) to build a high-performance IPv4 parser inspired by [Wojciech Muła's SIMD research](http://0x80.pl/notesen/2023-04-09-faster-parse-ipv4.html).

This post shows how SPMD Go can keep code readable while improving performance through parallel processing, reduction operations, and cross-lane communication. This example is a lot less complex than base64 and demonstrates why language-level support for parallel data manipulation matters.

<!--more-->

## Background

Wojciech Muła's [SIMD-ized IPv4 parsing](https://github.com/WojciechMula/toys/tree/master/parseip4) shows parallel algorithms achieving 2-3x speedups over traditional parsing. His approach uses:

1. **16-byte parallel processing**: Loading entire IPv4 strings into SIMD registers
2. **Dot mask generation**: Using parallel comparisons to create bitmasks of dot positions
3. **Pattern-based field extraction**: Leveraging precomputed lookup tables for field boundaries
4. **Parallel digit conversion**: Processing all four octets simultaneously

Our SPMD Go implementation adapts these techniques while maintaining readability and trying to keep it Go idiomatic. It should be readable without knowing assembly SIMD instructions.

## Sequential Approach

Go's standard library processes IPv4 addresses character by character:

```go
func parseIPv4Fields(in string, off, end int, fields []uint8) error {
    var val, pos int
    var digLen int
    s := in[off:end]
    for i := 0; i < len(s); i++ {
        if s[i] >= '0' && s[i] <= '9' {
            if digLen == 1 && val == 0 {
                return parseAddrError{in: in, msg: "IPv4 field has octet with leading zero"}
            }
            val = val*10 + int(s[i]) - '0'
            digLen++
            if val > 255 {
                return parseAddrError{in: in, msg: "IPv4 field has value >255"}
            }
        } else if s[i] == '.' {
            // Handle dot logic...
            fields[pos] = uint8(val)
            pos++
            val = 0
            digLen = 0
        } else {
            return parseAddrError{in: in, msg: "unexpected character"}
        }
    }
    return nil
}
```

This sequential approach processes one character at a time and leaves CPU parallelism unused.

## The SPMD Transformation

### Phase 1: Parallel Character Analysis

Our SPMD approach begins by analyzing all characters simultaneously using 16-lane processing:

```go
func parseIPv4(s string) ([4]byte, error) {
    if len(s) < 7 || len(s) > 15 {
        return [4]byte{}, parseAddrError{in: s, msg: "IPv4 address string too short or too long"}
    }

    // Pad string to 16 bytes with null terminators (like SSE register)
    input := [16]byte{}
    copy(input[:], s)

    // Process all bytes in parallel
    var dotMask [16]bool
    var dotMaskTotal lanes.Varying[uint32]

    var loop int
    go for i, c := range input {
        dotMask[i] = c == '.'
        if dotMask[i] {
            dotMaskTotal++
        }
        digitMask := (c >= '0' && c <= '9')

        // Valid if dot, digit, or null (padding)
        validChars := dotMask[i] || digitMask || c == 0
    }
```

This parallel analysis validates all characters simultaneously and creates boolean masks for dots and digits -- a direct adaptation of Mula's SIMD character classification.

The key insight here is the **padding strategy**: since we process the fixed-size `[16]byte` array in SPMD fashion, we need a consistent 16-byte input. The `input := [16]byte{}` creates a zero-initialized array, and `copy(input[:], s)` fills it with the IPv4 string, leaving trailing zeros as padding. The validation logic `validChars := dotMask[i] || digitMask || c == 0` explicitly accepts null padding, making shorter IPv4 addresses work seamlessly with parallel processing. Note that `dotMask` is a regular `[16]bool` array -- the parallel `go for` loop writes to it via scatter operations, and we use it later in a second `go for` loop to build the dot position bitmask.

After this initial parallel validation phase, the algorithm continues with the original string `s` for precise boundary calculations and error reporting.

### Phase 2: Reduction-Based Validation

We use reduction operations to aggregate validation results across all lanes:

```go
        // Check character validity with precise error location
        if !reduce.All(validChars) {
            return [4]byte{}, parseAddrError{in: s, at: reduce.FindFirstSet(!validChars) + loop, msg: "unexpected character"}
        }
        loop += lanes.Count(c)
    }

    // Count dots using reduction
    dotCount := reduce.Add(dotMaskTotal)
    if dotCount != 3 {
        return [4]byte{}, parseAddrError{in: s, msg: "invalid dot count"}
    }

    // Create dot position bitmask (mimics _mm_movemask_epi8)
    var mask uint16
    loop = 0
    go for _, isDot := range dotMask {
        mask |= uint16(reduce.Mask(isDot)) << loop
        loop += lanes.Count(isDot)
    }
```

The `reduce.Mask()` operation is particularly elegant: it converts the varying boolean into a bitmask, directly paralleling SSE's `_mm_movemask_epi8` instruction. The `loop` variable tracks the bit offset across iterations, and `lanes.Count(isDot)` returns the number of lanes for the element type, ensuring the bitmask is built correctly regardless of SIMD width.

Note the improved error reporting: `reduce.FindFirstSet(!validChars) + loop` locates the exact position of the first invalid character by combining the lane index with the iteration offset, providing precise error messages instead of generic failures. This demonstrates how reduction operations can enhance not just performance, but also debugging and user experience.

### Phase 3: Race-Free Dot Position Extraction

Here we solve the potential race condition by using a normal `for` loop and bit manipulation on the mask instead of having lanes compete to write positions, sometimes you can't do things in parallel:

```go
    // Extract dot positions using bit manipulation
    var dotPositions [3]int
    for i := 0; i < 3; i++ {
        pos := bits.TrailingZeros16(mask)
        dotPositions[i] = pos
        mask &= mask - 1  // Clear lowest set bit
    }

    // Define field boundaries as separate arrays for efficient range processing
    starts := [4]int{0, dotPositions[0], dotPositions[1], dotPositions[2]}
    ends := [4]int{dotPositions[0], dotPositions[1], dotPositions[2], len(s)}
```

This approach eliminates race conditions while extracting dot positions in order, exactly as Wojciech Muła's implementation does with bit manipulation.

### Phase 4: Parallel Field Validation and Conversion

Now we process all four IPv4 octets in parallel, with each lane handling one field. Note the use of `range`, this gives the compiler precise information about the iteration count, enabling better optimization:

```go
    // Validate field lengths in parallel
    go for i, start := range starts {
        end := ends[i]
        if i > 0 {
            start++ // Skip the dot
        }
        fieldLen := end - start
        if reduce.Any(fieldLen < 1 || fieldLen > 3) {
            return [4]byte{}, parseAddrError{in: s, msg: "invalid field length"}
        }
    }

    // Process all four fields in parallel
    var ip [4]byte

    go for field, start := range starts {
        end := ends[field]

        if field > 0 {
            start++ // Skip the dot
        }

        fieldLen := end - start
        var value int
        var hasLeadingZero bool

        // Convert field using optimized digit processing
        switch fieldLen {
        case 1:
            value = int(s[start] - '0')
        case 2:
            d1 := int(s[start] - '0')
            d0 := int(s[start+1] - '0')
            value = d1*10 + d0
            hasLeadingZero = (d1 == 0)
        case 3:
            d2 := int(s[start] - '0')
            d1 := int(s[start+1] - '0')
            d0 := int(s[start+2] - '0')
            value = d2*100 + d1*10 + d0
            hasLeadingZero = (d2 == 0)
        }

        // Validation: check each error condition across all lanes
        if reduce.Any(hasLeadingZero) {
            return [4]byte{}, parseAddrError{in: s, msg: "IPv4 field has octet with leading zero"}
        }
        if reduce.Any(value > 255) {
            return [4]byte{}, parseAddrError{in: s, msg: "IPv4 field has value >255"}
        }
        ip[field] = uint8(value)
    }

    return ip, nil
}
```

This parallel field processing mirrors Muła's `SSE_CONVERT_MAX1/2/3` macros, handling different field lengths efficiently while maintaining full validation.

### Compiler Optimization: Array Range

The use of `go for field, start := range starts` is a subtle but important optimization. When ranging over a fixed-size array, the compiler knows the exact iteration count at compile time, enabling:

1. **Loop unrolling**: The compiler can unroll the loop entirely, generating direct code for each iteration
2. **Better instruction scheduling**: With known bounds, the compiler can optimize instruction ordering
3. **Eliminated bounds checks**: No runtime checks needed when array size is compile-time constant
4. **Eliminate iteration**: In this case especially, the number of iteration, 4, will fit on most architecture in just one SIMD register. So the iteration itself won't be necessary and can be removed.

Give the compiler as much compile-time information as possible to enable maximum optimization.

### Enhanced Error Reporting Through Reduction Operations

One advantage of the SPMD approach in Go is improved error reporting. With SIMD intrinsics and assembly it's harder to track error handling, but this proposal makes it straightforward:

```go
// Instead of generic "unexpected character"
if !reduce.All(validChars) {
    return [4]byte{}, parseAddrError{in: s, at: reduce.FindFirstSet(!validChars) + loop, msg: "unexpected character"}
}

// And precise field error reporting using reduce.Any
if reduce.Any(hasLeadingZero) {
    return [4]byte{}, parseAddrError{in: s, msg: "IPv4 field has octet with leading zero"}
}
if reduce.Any(value > 255) {
    return [4]byte{}, parseAddrError{in: s, msg: "IPv4 field has value >255"}
}
```

`reduce.Any()` and `reduce.FindFirstSet()` detect error conditions across all lanes simultaneously. Since `reduce.Any()` returns a uniform bool, the `return` statement is under a uniform condition -- all lanes agree on whether to return -- making it a valid early exit in the `go for` loop.

## Performance

The SPMD approach enables character-level parallelism (all characters validated simultaneously), field-level parallelism (all four octets processed in parallel), reduced branching, better cache efficiency, and instruction-level parallelism.

Muła's research suggests 2-3x speedups on IPv4 parsing. The code complexity is lower than writing with intrinsics, but it depends on the compiler's optimization ability. ISPC and Mojo have shown it's doable, though there's significant work to get there. A proof of concept could likely be built with TinyGo (it uses LLVM like ISPC) and would validate the concept.

## Complexity Trade-off

This SPMD approach has real costs, as discussed in the [cross-lane communication post](../cross-lane-communication/): harder to understand than sequential parsing, more subtle bugs, and requires understanding of SIMD concepts.

In this example the resulting code is readable and maintainable. If the go profiler could track the results properly, it should be manageable for many developers. That's the main justification for such an addition. If most developers can write data parallel code and we democratize high-performance code, it's worth it. If not, intrinsics and assembly might be the better path forward.

**[View Complete Source Code](../../examples/ipv4-parser/)** - Full implementation with usage examples and detailed comments.

## References

- [Wojciech Muła's SIMD IPv4 parsing research](http://0x80.pl/notesen/2023-04-09-faster-parse-ipv4.html) - The foundational research this implementation is based on
- [Muła's IPv4 parsing implementation](https://github.com/WojciechMula/toys/tree/master/parseip4) - Complete SSE implementation and benchmarks
- [Practical Vector Processing in Go](../practical-vector/) - Introduction to SPMD concepts and `reduce` operations
- [Cross-Lane Communication](../cross-lane-communication/) - Deep dive into advanced SPMD patterns and race condition solutions
- [Go's net/netip package](https://github.com/golang/go/blob/master/src/net/netip/netip.go) - The traditional IPv4 parsing implementation

---

**Previous in series:** [Cross-Lane Communication: When Lanes Need to Talk](../cross-lane-communication/) - Understanding complex cross-lane operations and their trade-offs.

That wraps up this SPMD Go blog series.
