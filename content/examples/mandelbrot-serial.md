---
title: "Mandelbrot Set - Serial Version"
description: "Scalar mandelbrot computation compiled to browser WASM"
date: 2025-07-13T15:00:00-07:00
tags: ["golang", "performance", "mandelbrot", "WASM"]
---

This is the serial (scalar) implementation of the Mandelbrot set computation, compiled to WebAssembly for the browser-based benchmark demo.

<!--more-->

## Complete Source Code

{{< readfile file="examples/mandelbrot_serial.go" >}}

## How It Works

The serial version computes each pixel independently in a nested loop. For each pixel coordinate `(x, y)` in the complex plane `[-2.5, 1.5] x [-1.25, 1.25]`, it iterates `z = z^2 + c` until either `|z| > 2` (escaped) or `maxIter` is reached (in the set).

Results are stored in a global `int32` buffer exposed to JavaScript via `//go:export` functions, allowing the browser to read WASM linear memory directly and render the fractal on an HTML canvas.

## Related

- [Mandelbrot Set - SPMD Version](../mandelbrot-spmd/) - SIMD-accelerated version using `go for` loops
