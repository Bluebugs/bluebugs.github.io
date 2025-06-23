+++
date = '2025-06-21T14:38:38-07:00'
draft = false
title = 'What if? Practical parallel data.'
description = 'Using a hypothetical `go for` construct to implement a variety of string operation'
featured_image = 'images/lakelouise.jpg'
featured_image_class = 'cover bg-center'
+++

## Printf helper

One of the first and most repetitive tasks for [doPrintf](https://github.com/golang/go/blob/master/src/fmt/print.go#L1028) is to find `%` in a string. Right now, this is just iterating one character after another. This is very simple to parallelize using data parallelism and would look like the code below.

<!--more-->

{{< spmd-printf-verbs >}}

One of the important bit of this example is the use `reduce.Any` inside a `go for` loop. This make the `if` act like a normal `if` with a jump and enable a quick exit as soon as at least one `%` is found. This is still readable and maintainable. I would think this could be acceptable in the Go standard library codebase once this feature is properly ready for prime time.

## Encode hexadecimal

Hexadecimal encoding and decoding are relatively [slow](https://github.com/golang/go/issues/68188) and would benefit from using SIMD instructions. If we look at the current implementation for hex.Encode in Go's standard library:

```go
func Encode(dst, src []byte) int {
 j := 0
 for _, v := range src {
  dst[j] = hextable[v>>4]
  dst[j+1] = hextable[v&0x0f]
  j += 2
 }
 return len(src) * 2
}
```

If we adapt it to use `go for` and parallelize the data manipulation, we get the following example:

{{< spmd-hex >}}

There are different ways to do the index access. We could have also used an if statement for both index cases, but the most important change is that we are expressing the operation on one lane at a time. This enables the compiler to automatically adapt the code to whatever number of lanes are available and use proper linear memory access, which is the most efficient way to manipulate data. It is critical when doing SIMD to be very efficient with memory access, as that often ends up being the limiting factor.

## bytes.ToUpper

Last practical example. For bytes.ToUpper, the Go standard library has a fast path when the string consists only of ASCII characters. We can make that fast path even faster using SIMD and data parallelism. Here is what the code would look like:

{{< spmd-toupper >}}

This example demonstrates two key concepts. First, we use data parallelism to quickly scan the entire string for non-ASCII characters and lowercase letters in the first `go for` loop. The `reduce.Any()` function allows us to efficiently detect if any lane found a non-ASCII character, enabling an early break from the loop.

Second, if we determine the string contains only ASCII characters and has lowercase letters, we use another `go for` loop to perform the actual uppercase conversion in parallel. Each lane processes one character, checking if it's lowercase and applying the conversion (`c -= 'a' - 'A'`) only when needed.

This approach leverages the fact that ASCII uppercase conversion is a simple arithmetic operation that can be efficiently vectorized, while maintaining the readability and structure of the original algorithm.

## Summary

These three practical examples demonstrate how a hypothetical `go for` construct could bring data parallelism to Go while maintaining the language's core principles of simplicity and readability. Each example showcases different aspects of SIMD programming:

**Printf helper** shows the simplest case: parallel scanning with early termination using `reduce.Any()`. This pattern is common in string processing where you need to find the first occurrence of a character or pattern.

**Hexadecimal encoding** illustrates how parallel computation can be combined with efficient memory access patterns. The key insight is expressing operations per-lane rather than per-iteration, allowing the compiler to automatically vectorize and adapt to different SIMD widths.

**bytes.ToUpper** demonstrates the most sophisticated pattern: conditional execution with lane masking. The two-phase approach (scan then convert) shows how reduction operations can inform algorithmic decisions, while masked execution ensures only relevant lanes perform computation.

### Real-World Impact

These optimizations could significantly improve Go's standard library performance and any Go application. String processing, encoding/decoding, parsing, and mathematical operations are fundamental building blocks that appear in virtually every Go application. Making them faster through data parallelism would benefit the entire ecosystem without requiring developers to learn complex SIMD programming.

The `go for` construct bridges the gap between Go's accessibility and the performance demands of modern applications, proving, in my opinion, that readable code and high performance don't have to be mutually exclusive.
