---
title: "Putting It All Together"
date: 2025-07-13T14:00:00-07:00
description: "Fast IPv4 Parsing with SPMD Go"
featured_image: 'images/lakelouise.jpg'
featured_image_class: 'cover bg-center'
tags: ["golang", "performance", "networking", "SIMD", "SPMD"]
---

Network address parsing is ubiquitous in Go applications, yet the standard library implementations process strings character by character, leaving significant performance on the table. In this comprehensive exploration, we'll combine the SPMD concepts from our [previous](../practical-vector/) [blogs](../cross-lane-communication/) to build a high-performance IPv4 parser inspired by [Wojciech Muła's SIMD research](http://0x80.pl/notesen/2023-04-09-faster-parse-ipv4.html).

This post demonstrates how SPMD Go could be used to keep code readable, but significantly improve performance by applying the techniques we've explored: parallel processing, reduction operations, and cross-lane communication. This example is a lot less complex than trying things like base64 and shows the benefit of language-level support for parallel data manipulation in my opinion.

<!--more-->

## The Research Foundation

Wojciech Muła's work on [SIMD-ized IPv4 parsing](https://github.com/WojciechMula/toys/tree/master/parseip4) demonstrates that clever parallel algorithms can achieve 2-3x performance improvements over traditional parsing. His approach uses several key insights:

1. **16-byte parallel processing**: Loading entire IPv4 strings into SIMD registers
2. **Dot mask generation**: Using parallel comparisons to create bitmasks of dot positions
3. **Pattern-based field extraction**: Leveraging precomputed lookup tables for field boundaries
4. **Parallel digit conversion**: Processing all four octets simultaneously

Our SPMD Go implementation adapts these techniques while maintaining readability and trying to keep it Go idiomatic. It should be readable without knowing assembly SIMD instructions.

## The Traditional Sequential Approach

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

This sequential approach, while correct and readable, processes one character at a time and can't leverage modern CPU parallelism.

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

    // Process all 16 lanes in parallel
    var dotMaskTotal varying[16] uint8
    var dotMask varying[16] bool
    var digitMask varying[16] bool
    var validChars varying[16] bool

    go for i, c := range[16] input {
        dotMask[i] = c == '.'
        if dotMask[i] {
            dotMaskTotal[i] = 1
        }
        digitMask[i] = (c >= '0' && c <= '9')
        
        // Valid if dot, digit, or null (padding)
        validChars[i] = dotMask[i] || digitMask[i] || c == 0
    }
```

This parallel analysis validates all characters simultaneously and creates boolean masks for dots and digits—a direct adaptation of Muła's SIMD character classification.

The key insight here is the **padding strategy**: since we process exactly 16 lanes using `range[16] input`, we need a consistent 16-byte input. The `input := [16]byte{}` creates a zero-initialized array, and `copy(input[:], s)` fills it with the IPv4 string, leaving trailing zeros as padding. The validation logic `validChars[i] = dotMask[i] || digitMask[i] || c == 0` explicitly accepts null padding, making shorter IPv4 addresses work seamlessly with 16-lane processing.

After this initial parallel validation phase, the algorithm continues with the original string `s` for precise boundary calculations and error reporting.

### Phase 2: Reduction-Based Validation

We use reduction operations to aggregate validation results across all lanes:

```go
    // Check character validity with precise error location
    if !reduce.All(validChars) {
        return [4]byte{}, parseAddrError{in: s, at: reduce.FindFirstSet(validChars), msg: "unexpected character"}
    }

    // Count dots using reduction
    dotCount := reduce.Sum(dotMaskTotal)
    if dotCount != 3 {
        return [4]byte{}, parseAddrError{in: s, msg: "invalid dot count"}
    }

    // Create dot position bitmask (mimics _mm_movemask_epi8)
    dotPositionMask := reduce.Mask(dotMask)
```

The `reduce.Mask()` operation is particularly elegant—it converts the boolean array into a bitmask, directly paralleling SSE's `_mm_movemask_epi8` instruction.

Note the improved error reporting: `reduce.FindFirstSet(validChars)` locates the exact position of the first invalid character, providing precise error messages instead of generic failures. This demonstrates how reduction operations can enhance not just performance, but also debugging and user experience.

### Phase 3: Race-Free Dot Position Extraction

Here we solve the potential race condition by using a normal `for` loop and bit manipulation on the mask instead of having lanes compete to write positions, sometimes you can't do things in parallel:

```go
    // Extract dot positions using bit manipulation
    var dotPositions [3]int
    mask := dotPositionMask
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
    var errors [4]parseAddrError
    var hasError varying[4] bool

    go for field, start := range starts {
        end := ends[field]

        if field > 0 {
            start++ // Skip the dot
        }
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

        // Validation and error handling
        if hasLeadingZero {
            errors[field] = parseAddrError{in: s, msg: "IPv4 field has octet with leading zero"}
            hasError[field] = true
        } else if value > 255 {
            errors[field] = parseAddrError{in: s, msg: "IPv4 field has value >255"}
            hasError[field] = true
        } else {
            ip[field] = uint8(value)
        }
    }

    // Check for errors using reduction
    if reduce.Any(hasError) {
        return [4]byte{}, errors[reduce.FindFirstSet(hasError)]
    }

    return ip, nil
}
```

This parallel field processing mirrors Muła's `SSE_CONVERT_MAX1/2/3` macros, handling different field lengths efficiently while maintaining full validation.

### Compiler Optimization: Array Range vs Range[N]

The use of `go for field, start := range starts` is a subtle but important optimization. When ranging over an array, the compiler knows the exact iteration count at compile time, enabling:

1. **Loop unrolling**: The compiler can unroll the loop entirely, generating direct code for each iteration
2. **Better instruction scheduling**: With known bounds, the compiler can optimize instruction ordering
3. **Eliminated bounds checks**: No runtime checks needed when array size is compile-time constant
4. **Eliminate iteration**: In this case especially, the number of iteration, 4, will fit on most architecture in just one SIMD register. So the iteration itself won't be necessary and can be removed.

This represents a key principle for SPMD Go: give the compiler as much compile-time information as possible to enable maximum optimization.

### Enhanced Error Reporting Through Reduction Operations

One significant advantage of the SPMD approach in Go itself is improved error reporting. If using SIMD intrinsics and assembly, it is harder to keep track of proper error handling, but with this proposal, it feels a lot more logical and simpler to do proper error reporting, like the locations when validating the initial content of the string:

```go
// Instead of generic "unexpected character" 
if !reduce.All(validChars) {
    return [4]byte{}, parseAddrError{in: s, at: reduce.FindFirstSet(validChars), msg: "unexpected character"}
}

// And precise field error reporting
if reduce.Any(hasError) {
    return [4]byte{}, errors[reduce.FindFirstSet(hasError)]
}
```

The `reduce.FindFirstSet()` operation efficiently locates the first lane with an error condition, providing users with exact character positions rather than generic failure messages. This demonstrates how parallel processing in Go enables simpler and idiomatic Go error handling in a simple form.

## Performance Implications

This SPMD approach offers several advantages over traditional parsing:

1. **Character-level parallelism**: All characters validated simultaneously
2. **Field-level parallelism**: All four octets processed in parallel
3. **Reduced branching**: Structured validation reduces conditional branches
4. **Cache efficiency**: Better memory access patterns
5. **Instruction-level parallelism**: Multiple operations execute simultaneously

Based on Muła's research, we can expect 2-3x performance improvements on IPv4 parsing. The added code complexity is not the same as if we were writing this with intrinsics, but we rely on the compiler to be able to do all those optimizations.

ISPC and Mojo have shown it is doable, but there is a lot of work to get there. A potential Proof of Concept could likely be built more easily with TinyGo that uses LLVM like ISPC and would give a validation of the concept.

## The Complexity Trade-off

While this SPMD approach offers significant performance benefits, it also raises important questions we explored in our [cross-lane communication analysis](../cross-lane-communication/):

- **Increased complexity**: The code is harder to understand than sequential parsing
- **Debugging challenges**: Parallel bugs are more subtle than sequential ones
- **Maintenance overhead**: Requires understanding of SIMD concepts

However, in this example, the resulting code is readable and maintainable. If it did come with a proper benchmark and the go profiler was able to track the result properly, it should be quite manageable for a lot of developers to write this code, I would think. This is the main justification potential for such an addition. If most developers can write data parallel code and we democratize writing high-performance code, it is worth it. If not, leaving intrinsic and assembly to engineer that can do it might be actually the better way forward. What do you think?

**[View Complete Source Code](../../examples/ipv4-parser/)** - Full implementation with usage examples and detailed comments.

## References

- [Wojciech Muła's SIMD IPv4 parsing research](http://0x80.pl/notesen/2023-04-09-faster-parse-ipv4.html) - The foundational research this implementation is based on
- [Muła's IPv4 parsing implementation](https://github.com/WojciechMula/toys/tree/master/parseip4) - Complete SSE implementation and benchmarks
- [Practical Vector Processing in Go](../practical-vector/) - Introduction to SPMD concepts and `reduce` operations
- [Cross-Lane Communication](../cross-lane-communication/) - Deep dive into advanced SPMD patterns and race condition solutions
- [Go's net/netip package](https://github.com/golang/go/blob/master/src/net/netip/netip.go) - The traditional IPv4 parsing implementation

---

**Previous in series:** [Cross-Lane Communication: When Lanes Need to Talk](../cross-lane-communication/) - Understanding complex cross-lane operations and their trade-offs.

This concludes our SPMD Go blog series. We've explored the theoretical foundations, practical applications, advanced communication patterns, and real-world performance implementations.
