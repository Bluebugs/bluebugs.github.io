---
title: "Base64 Decoder - Complete Example"
description: "Full SPMD base64 decoder with cross-lane communication"
date: 2025-07-13T15:00:00-07:00
tags: ["golang", "performance", "base64", "SIMD", "SPMD"]
---

This is the complete implementation of the base64 decoder discussed in [Cross-Lane Communication: When Lanes Need to Talk](../../blogs/cross-lane-communication/).

<!--more-->

## Complete Source Code

{{< readfile file="examples/decode_base64.go" >}}

## Usage Example

```go
func main() {
    testCases := []string{
        "SGVsbG8gV29ybGQ=", // "Hello World"
        "Zm9vYmFy",         // "foobar"
        "YWJjZA==",         // "abcd"
    }

    for _, tc := range testCases {
        decoded, valid := Decode([]byte(tc))
        if !valid {
            fmt.Printf("%-20s -> ERROR: Invalid base64\n", tc)
        } else {
            fmt.Printf("%-20s -> %s\n", tc, string(decoded))
        }
    }
}
```

## Source material

This implementation is inspired by [Miguel Young de la Sota's excellent analysis](https://mcyoung.xyz/2023/11/27/simd-base64/) in "Designing a SIMD Algorithm from Scratch" and adapted for the hypothetical SPMD Go extension.

## Related Blog Posts

- [Cross-Lane Communication: When Lanes Need to Talk](../../blogs/cross-lane-communication/) - Detailed explanation of this implementation and the complexity trade-offs
- [Practical Vector Processing in Go](../../blogs/practical-vector/) - Introduction to SPMD concepts and `reduce` operations
