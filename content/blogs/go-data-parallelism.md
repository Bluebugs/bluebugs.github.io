+++
date = '2024-11-13T18:48:59-07:00'
draft = true
title = 'Data Parallelism: simpler solution for Golang?'
+++

## Why Data Parallelism Matters in Go

Go is a fast language, but it lacks easy ways to express data parallelism and does not provide direct access to low-level SIMD (Single Instruction Multiple Data) instructions. As a result, standard libraries like [base64](https://github.com/golang/go/issues/19636), [hex](https://github.com/golang/go/issues/68188), [utf8](https://github.com/golang/go/issues/63347), [json](https://github.com/golang/go/issues/53178), [jpeg](https://github.com/golang/go/issues/24499), and [map](https://github.com/golang/go/issues/71255) can be slower compared to other languages. Other ecosystems are more likely to adopt specialized, high-performance libraries, which is why, in some cases, Node.js can outperform Go.

The solution to this bottleneck is for the Go compiler to generate SIMD instructions. There are three main approaches to enabling SIMD in programming languages:

1. **Automatic Vectorization:** Relying on the compiler to automatically generate SIMD instructions. Despite decades of research, this approach rarely delivers significant performance gains, and developers still often write assembly code for critical sections.

2. **Abstraction Libraries:** Using libraries like [Google Highway](https://github.com/google/highway) that provide a higher-level abstraction over SIMD instructions. This approach works well in languages like C++, but is less common in Go. The current [SIMD proposal](https://github.com/golang/go/issues/73787) for Go follows this idea, with code examples like [this](https://github.com/AndrewHarrisSPU/simd-demo-0/blob/main/sigmoid_simd.go).

3. **Language-Level Support:** Integrating data parallelism directly into the language, as seen in GPU-oriented languages (e.g., CUDA, OpenGL shaders) and newer languages like [ISPC](https://ispc.github.io/ispc.html) and [Mojo](https://docs.modular.com/mojo/). This approach makes parallelism more accessible and portable.

## What if Go Made Data Parallelism Simpler?

I believe that integrating data parallelism as a core language feature would make Go code more accessible, readable, and portable. In this blog, I explore what it might look like to add data parallelism to Go, inspired by languages like ISPC and Mojo. Even if Go never adopts this approach, understanding these concepts can help you write better compute kernels or Mojo code.

The key feature missing in Go is the ability to express data parallelism. While Go supports concurrent execution with the `go` keyword, it does not let developers indicate where data can be processed in parallel. Languages like ISPC use `foreach`, and Mojo uses `vectorize` to express this. Both enable the same code to run on CPUs and GPUs.

This model is called SPMD (Single Program Multiple Data). Unlike languages like C#, Zig, or Rust, which offer only high-level abstractions or low-level primitives, SPMD lets developers explicitly mark code that can be parallelized.

## Let's `go for it`

How could we express data parallelism in Go? Currently, Go does not annotate functions as thread-safe; we simply use `go` to run them concurrently. Similarly, we could extend the `go` keyword with `for` to indicate data parallelism, e.g., `go for`. This would not break existing code, as `go` is currently only followed by a function call.

Before diving into examples, let's define some vocabulary:

- `varying`: Represents a SIMD register containing multiple values, one per "lane".
- `lane`: Each value in a SIMD register.
- `mask`: Used to enable or disable lanes during computation.
- `uniform`: A variable with the same value across all lanes.

We use `varying` to indicate types that hold multiple values (across lanes), and `uniform` for single values. Inside a `go for` loop, you might need to declare variables as `uniform` for optimization or compatibility.

## Simple Example

The following example demonstrates a simple sum operation using data parallelism:

{{< spmd-sum >}}

Here, we declare variables as `varying` to operate on multiple data points in parallel. At the end of the loop, we use a `reduce` function to combine the results from all lanes into a single value. Libraries like [ISPC](https://ispc.github.io/ispc.html#reductions) and [Mojo](https://docs.modular.com/mojo/stdlib/algorithm/reduction/) provide a variety of reduction functions, which could inspire a similar package in Go.

With this example, we also show how the mask can be used. If there is no data to be manipulated, the compiler can use a `mask` to ignore some lanes and just move on. There is no requirement on the compiler for how to implement this. ISPC and Mojo have shown that this model can match a wide range of hardware. It also leaves a lot of freedom to the compiler on how to implement this. This is just a mental model of what a pseudo compiler would do.

## How Would _if_ Work?

We can extend the masking concept to implement conditional logic (`if` statements) in data-parallel code:

{{< spmd-oddeven >}}

This allows us to process data in parallel, with different behavior depending on the data in each lane. The next control flow construct we need is `for` in the context of data parallelism.

## Extending to _for_

Let's look at how `for` inside a `go for` SPMD context would work. We'll add a bit of `if` inside to show that they can be stacked however we want.

{{< spmd-countbits >}}

## Summary

Adding data parallelism as a first-class feature in Go could make high-performance computing more accessible and portable. By learning from languages like ISPC and Mojo, we can imagine a future where Go code is both simple and fast, leveraging the full power of modern hardware. Even if Go never adopts these features, understanding them can help you write better, more efficient code in any language.
