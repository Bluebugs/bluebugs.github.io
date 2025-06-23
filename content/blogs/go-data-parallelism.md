+++
date = '2025-06-19T18:48:59-07:00'
draft = false
featured_image = 'images/banff.jpg'
featured_image_class = 'cover bg-center'
title = 'Data Parallelism: simpler solution for Golang?'
+++

## Why Data Parallelism Matters in Go

Go is a fast language, but it lacks easy ways to express data parallelism and does not provide direct access to low-level **SIMD** (Single Instruction Multiple Data) instructions. As a result, standard libraries like [base64](https://github.com/golang/go/issues/19636), [hex](https://github.com/golang/go/issues/68188), [utf8](https://github.com/golang/go/issues/63347), [json](https://github.com/golang/go/issues/53178), [jpeg](https://github.com/golang/go/issues/24499), and [map](https://github.com/golang/go/issues/71255) are slower than expected. Other ecosystems are more likely to adopt specialized, high-performance libraries, which is why, in some cases, for example, Node.js can outperform Go.

The solution to this bottleneck is for the Go compiler to generate SIMD instructions. There are three main approaches to enabling SIMD in programming languages:

1. **Automatic Vectorization:** Relying on the compiler to automatically generate SIMD instructions. Despite decades of research, this approach rarely delivers significant performance gains, and developers still write assembly code for critical sections to have reliable outcome.

2. **Abstraction Libraries:** Using libraries like [Google Highway](https://github.com/google/highway) that provide a higher-level abstraction over SIMD instructions. This approach works well in languages like C++. The current [SIMD proposal](https://github.com/golang/go/issues/73787) for Go follows this idea, with code examples like [this](https://github.com/AndrewHarrisSPU/simd-demo-0/blob/main/sigmoid_simd.go).

3. **Language-Level Support:** Integrating data parallelism directly into the language, as seen in GPU-oriented languages (e.g., CUDA, OpenGL shaders) and more classical languages like [ISPC](https://ispc.github.io/ispc.html), close to C, and [Mojo](https://docs.modular.com/mojo/), close to Python. This approach makes data parallelism in the code base more readable and maintainable.

## What if Go Made Data Parallelism Simpler?

I believe that integrating data parallelism as a core language feature would make Go code faster, but keep its accessibility, readability, and portability. In this blog, I will explore what it might look like to add data parallelism to Go, inspired by languages like ISPC and Mojo. Even if Go never adopts this approach, understanding these concepts can help you write better compute kernels or Mojo code.

The key feature missing in Go is the ability to express that we can manipulate data in parallel in a certain block of code. While Go supports concurrent function execution with the **`go`** keyword, it focus only on code flow level parallelism. Languages like ISPC use **`foreach`**, and Mojo uses **`vectorize`** to express this. Both enable the same code to run on CPUs and GPUs.

This model is called **SPMD** (Single Program Multiple Data). Unlike languages like C#, Zig, or Rust, which offer only high-level abstractions or low-level primitives, SPMD lets developers explicitly write code that can be parallelized mechanically.

## Let's `go for it`

How could we express data parallelism in Go? Currently, Go does not annotate functions as thread-safe; we simply use **`go`** to run them concurrently. Similarly, we could extend the **`go`** keyword with **`for`** to indicate data parallelism, e.g., **`go for`**. This would not break existing code, as **`go`** is currently only followed by a function call.

Before diving into examples, let's define some vocabulary:

- **`varying`**: Represents a SIMD register containing multiple values, one per "lane".
- **`lane`**: Each value in a SIMD register.
- **`mask`**: Used to enable or disable lanes during computation.
- **`uniform`**: A variable with the same value across all lanes, aka a normal variable like all the variable you have in Go today.

We use **`varying`** to indicate types that hold multiple values (across lanes), and **`uniform`** for single values. Inside a **`go for`** loop, you might need to declare variables as **`uniform`** for optimization or compatibility.

## Simple Example

An example is always better than a long discourse. Let start with the following example to demonstrates a simple sum operation using data parallelism:

{{< spmd-sum >}}

Here, we declare a variables **`s`** as **`varying`** to operate on multiple data points in parallel. At the end of the loop, we use a **`reduce`** function to combine the results from all lanes into a single value. Libraries like [ISPC](https://ispc.github.io/ispc.html#reductions) and [Mojo](https://docs.modular.com/mojo/stdlib/algorithm/reduction/) provide a variety of reduction functions, which could inspire a similar package in Go.

With this example, we also show how the mask can be used. If there is no data to be manipulated, the compiler can use a mask to ignore some lanes and just move on. There is no requirement on the compiler for how to implement this. ISPC and Mojo have shown that this model can match a wide range of hardware, while CUDA, OpenCL and friends have shown it deliver well on GPU. It also leaves a lot of freedom to the compiler on how to implement it. This is just a mental model of what a pseudo compiler would do.

## How Would _if_ Work?

We can extend the masking concept to implement conditional logic (**`if`** statements) in data-parallel code:

{{< spmd-oddeven >}}

This allows us to process data in parallel, with different behavior depending on the data in each lane. The next control flow construct we need is **`for`** in the context of data parallelism.

## Extending to _for_

Let's look at how **`for`** inside a **`go for`** SPMD context would work. We'll add a bit of **`if`** inside to show that they can be stacked however we want.

{{< spmd-countbits >}}

> NOTE: For simplicity of the example and because I do not want everyone to have to click 32 times in the inner loop, I went with byte and uint8 type here. In a more practical implementation of this function, I should be manipulating int32 directly and write the inner loop test just inside the if  like so **`if v & (1 << it) != 0 {`**. The compiler should be able to match this loop with a popcount instruction if the hardware support it. Basically there is no reason that this would be any slower than a more direct to assembly approach, but it keep its readability in my opinion.

This was a fairly simple **`for`** loop, but it shows how manipulating the mask enable all the complexity in behavior we could want. We can nest loop, if. We can also implement **`break`** and **`continue`** using just mask.

## Summary

And we have shown that it is possible to extend Go with just a few keyword and make writing data parallel algorithm approachable, more readable and maintainable in my opinion. Let me know if there is anything that need clarification.

Adding data parallelism as a first-class feature in Go could make high-performance computing more accessible and portable. By learning from languages like ISPC and Mojo, we can imagine a future where Go code is simple, fast and leverage the full power of modern hardware. Even if Go never adopts these features, understanding them can help you write shader, compute kernel and code for Mojo or ISPC.
