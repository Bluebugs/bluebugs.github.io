---
title: "IPv4 Parser - Complete Example"
description: "Full SPMD IPv4 address parser implementation"
date: 2025-07-13T15:00:00-07:00
tags: ["golang", "performance", "networking", "SIMD", "SPMD"]
---

This is the complete implementation of the high-performance IPv4 address parser discussed in [Putting It All Together: Fast IPv4 Parsing with SPMD Go](../../blogs/go-spmd-ipv4-parser/).

<!--more-->

## Complete Source Code

{{< readfile file="examples/ipv4_parser.go" >}}

## Usage Example

```go
func main() {
    testCases := []string{
        "192.168.1.1",
        "10.0.0.1", 
        "255.255.255.255",
        "192.168.1.256", // Invalid: >255
        "192.168.01.1",  // Invalid: leading zero
        "192.168.a.1",   // Invalid: non-digit
    }

    for _, tc := range testCases {
        ip, err := parseIPv4(tc)
        if err != nil {
            fmt.Printf("%-15s -> ERROR: %s\n", tc, err)
        } else {
            fmt.Printf("%-15s -> %d.%d.%d.%d\n", tc, ip[0], ip[1], ip[2], ip[3])
        }
    }
}
```

## Source material

This implementation is inspired by [Wojciech Muła's SIMD-ized IPv4 parsing research](http://0x80.pl/notesen/2023-04-09-faster-parse-ipv4.html) and his [complete SSE implementation](https://github.com/WojciechMula/toys/tree/master/parseip4).

## Related Blog Posts

- [Putting It All Together: Fast IPv4 Parsing with SPMD Go](../../blogs/go-spmd-ipv4-parser/) - Detailed explanation of this implementation
- [Cross-Lane Communication: When Lanes Need to Talk](../../blogs/cross-lane-communication/) - Advanced SPMD patterns used here
- [Practical Vector Processing in Go](../../blogs/practical-vector/) - Introduction to SPMD concepts and `reduce` operations
