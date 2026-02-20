---
title: "Mandelbrot Set - SPMD Version"
description: "SIMD-accelerated mandelbrot computation using go for loops"
date: 2025-07-13T15:00:00-07:00
tags: ["golang", "performance", "mandelbrot", "SIMD", "SPMD", "WASM"]
---

This is the SPMD (Single Program Multiple Data) implementation of the Mandelbrot set computation, using `go for` loops to process 4 pixels simultaneously via WASM SIMD128.

<!--more-->

## Complete Source Code

{{< readfile file="examples/mandelbrot_spmd.go" >}}

## How It Works

The SPMD version processes each row with a `go for i := range width` loop, which the compiler vectorizes into 4-wide SIMD operations. Each iteration computes 4 adjacent pixels in parallel:

- `lanes.Varying[float32](i)` creates a vector of x-coordinates `[i, i+1, i+2, i+3]`
- `mandelSPMD` receives varying complex coordinates and returns varying iteration counts
- Per-lane `break` inside the iteration loop allows lanes that diverge early to exit independently
- The compiler generates 53 WASM SIMD instructions (`v128.*`) for the inner loop

The result is approximately 3x faster than the serial version, with identical output.

## Key SPMD Patterns

- **Varying break**: `if diverged { break }` exits individual lanes, not the entire loop
- **Scatter store**: `output[index] = iterations` with varying `index` writes 4 results simultaneously
- **Uniform broadcast**: scalar `y` and `maxIter` are automatically broadcast to all lanes

## Related

- [Mandelbrot Set - Serial Version](../mandelbrot-serial/) - Scalar baseline for comparison
