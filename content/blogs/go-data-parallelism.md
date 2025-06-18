+++
date = '2024-11-13T18:48:59-07:00'
draft = true
title = 'Data Parallelism: simple solution for Golang to provide Single Program Multiple Data'
+++

# Why?

Go is fast, but it can't easily express data parallelism nor does it provides an access to low level _SIMD_, _Single Instruction Multiple Data_. This is why [base64](https://github.com/golang/go/issues/19636), [hex](https://github.com/golang/go/issues/68188), [utf8](https://github.com/golang/go/issues/63347), [json](https://github.com/golang/go/issues/53178), [jpeg](https://github.com/golang/go/issues/24499), [map](https://github.com/golang/go/issues/71255) are slow. Other language language ecosystem are more prone to adopt specialty fast library. This is why in some use case Node will outperform Go.

The solution to this bottleneck is for the Go compiler to generate _SIMD_ instructions. There is three schools for that.

1. The historic premise that one day somehow compiler will be able to have some magic heuristic that can auto vectorize algorithm and generate _SIMD_ instruction by themselves. I heard that during my compiler course in school in the very early 2000. We are in 2025 and nobody rely on auto vectorization. We still write assembly and link it to C or C++ code. Somehow, we never expected auto threading, but expected auto vectorization which is a very similar problem. And well, it didn't deliver. We sunk a lot of effort in it and the gain are marginal.

2. Approach is to add a layer on top of your language ala [Google Highway](https://github.com/google/highway). This provide a small abstraction on top of direct assembly instruction, but leverage a lot of the complexity that C++ has, to make this work. Taking this approach in Go would be doable and is the current idea followed by the current [SIMD proposal](https://github.com/golang/go/issues/73787) with code that could look like [this](https://github.com/AndrewHarrisSPU/simd-demo-0/blob/main/sigmoid_simd.go).

3. Approach is what **GPU** oriented language have done with shaders and compute kernel. Think about _CUDA_, _OpenGL_ and friends. Porting that logic to a high level language that run on **CPU** first and can also run on **GPU**, was done first with [ISPC](https://ispc.github.io/ispc.html) which basically took the C syntax and added data parallelism to it and a later by [Mojo](https://docs.modular.com/mojo/) which did the same but for the Python ecosystem.

# What if Go trying to make it simpler

I do believe that the third approach, making it a core part of the language, would lead to a more accessible, simpler, readable and portable code. That is why I will dedicate this blog to what it would look like to add data parallelism. Hopefully if I succeed, you will even be able to write your own compute kernel or Mojo code, if Go never goes take this road.

The core feature missing in Golang is the ability to express data parallelism. We can express code flow parallelism by using the `go` keyword, but we can't tell the compiler where there is data parallelism. Language like ISPC, the grandfather in this domain, introduced `foreach` on top of a C syntax, while language like Mojo use `vectorize`. They both work in the same way and can enable the same code to run also on the GPU.

This language express what has been called _SPMD_, _Single Program Multiple Data_. These are different from C#, Zig or Rust, which only expose either too simple high level type or just lower level primitive, but doesn't enable the developer to tell the compiler when the code is actually able to manipulate data in parallel.

# Let's `go for it`

So how would we express in Go that there is data parallelism. In Go, we do not color function or indicate they are safe to call from a thread, we just use go and we get parallel code execution. To indicate we want to want to have data parallelism, we could just reuse the `go` keyword and follow it by `for`. This won't break any existing code as `go` can only be followed by a function today.

Before using live example to better explain how this would work, let's introduce a bit of vocabulary. I will use `varying` are a representation of the _SIMD_ register that contain multiple value in it. Each of those value are in a `lane`. The number of lane will vary between process, instruction set and size of the data manipulated. Each lane will get applied the same instruction as all the other lane. One instruction, multiple lanes. To make it possible to implement complex algorithm, we will use a mask that can turn on and off any lane.

On the other side, existing variable can only hold one value at a time and you need to assemble them in an array to get more than one. In shader vocabulary, this variable would be called `uniform` as there value would be uniform for all `lane`.

We will need to use `varying` to indicate that a type is going to be used to fill value from multiple `lane`. This is going to be useful especially when outside of a `go for`. On the opposite, when inside a `go for` loop, we might need to declare a variable `uniform` to match the type of variable created outside of the `go for` or for optimization purpose.

# Simple example

The example below is one of the most common case. A lot of language have started by just implementing it as a special type with operator overload and no need for anything else. That ended up limiting what was doable and most algorithm require more complex code structure. Still, let's start with the simple case.

{{< spmd-sum >}}

The example show why we need to be able to declare variable as `varying` and how the code flow. I hope it also clarify that some time at the end of a loop, you have your result in a `varying` when you want it as just one value. That's why this example introduce a `reduce` module that enable going from a `varying` to an `uniform`. `Add` is just one example. [ISPC](https://ispc.github.io/ispc.html#reductions) and [Mojo](https://docs.modular.com/mojo/stdlib/algorithm/reduction/) have a good amount of functions they provide as part of their equivalent `reduce` module. They are a great inspiration of what could be the content of a `reduce` package.

With this example, we also show how the mask can be used. If there is no data to be manipulated, the compiler can use a `mask` to ignore some lane and just move on. There is no requirement on the compiler on how to implement this. ISPC and Mojo have shown that this model can be match with a very large set of hardware. It also leave a lot of freedom to the compiler on how to implement this. This is just a mental model of what a pseudo compiler would do.

# How would _if_ work

We can use the masking concept used for the end of the loop and extend it further to implement `if`. Let's go with a small example.

{{< spmd-oddeven >}}

As you see with this example, we can now implement simple algorithm that process data in parallel, but with slightly different behavior depending on what those data are. The next code flow control, we are missing to be complete, is `for` in the context of data parallelism.

# Extending to _for_

Let's look at how `for` inside `go for` _SPMD_ context would work. Adding a bit of `if` inside to show that they can be stacked however we want.

{{< spmd-countbits >}}

For simplicity of the example and because I do not want everyone to have to click 32 times in the inner loop, I went with byte and uint8 type here. In a more practical implementation of this function, I would be manipulating int32 directly and write the inner loop test just inside the if  like so `if v & (1 << it) != 0 {`. The compiler could match this loop with a popcount instruction if the instruction set support it. Basically there is no reason that this would be any slower than a more direct to assembly approach, but it keep its readability in my opinion.

# Conclusion

Now we have a mental model of how data parallelism could be working in Go and ISPC along with Mojo have shown that we can get the performance we want from that information close to what assembly provide. I would actually argue that because in my opinion this is more accessible, readable and maintainable, it could be used by more people in more place leading to actually more speed improvement for the entire ecosystem.

And even if Go does go a different way, you can likely understand Mojo, ISPC or even compute kernel now. Have fun!
