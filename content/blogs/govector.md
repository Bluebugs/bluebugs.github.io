+++
date = '2025-01-12T10:40:28-07:00'
draft = true
title = 'Parallel data manipulation to Go'
description = 'How could we enable Go to manipulate data in parallel efficiently?'
+++

In Go, we have the ability to execute multiple concurrent code path (concurrency) in parallel using goroutine. There has been consideration to add concurrency without parallelism with [coroutine](https://research.swtch.com/coro), but we haven't really explored the concept of getting parallelism without concurrency.

# What is parallelism without concurrency?

The idea is that the same code is executed on an array of data in parallel. This is how language like Cuda, OpenCL or even shaders language work with GPU. For shaders, for example, you code the algorithm that is going to be applied to each pixels, for example, and it will execute that code on all the pixels in parallel. Same code, different data. No code concurrency, just data parallelism. And the code can adapt to the capacity of the hardware (number of unit doing computation in parallel) without change.

For CPU, we have [ISPC](https://ispc.github.io/), which tried to bring this concept to the C ecosystem. [Google Highway](https://github.com/google/highway) trying to bring this to the C+ ecosystem. And finally [Mojo](https://www.modular.com/mojo) trying to bring this concept with a very modern take to the Python ecosystem. And zig is supporting it natively ([here](https://www.openmymind.net/SIMD-With-Zig/) is an example on how to use it).

# Why would we want this in the Go ecosystem?

All Go core library and the vast majority are not taking any advantage of SIMD instructions set which would enable significant speedup when iterating on large-ish data. It is why Go is slower than Node at parsing JSON (Node use [simdjson](https://simdjson.org/software/). There is a very long list of slow functions in the Go standard library:
- encoding/base64: [decoding](https://github.com/golang/go/issues/19636), [encoding](https://github.com/golang/go/issues/20206)
- image/jpeg: [decode](https://github.com/golang/go/issues/24499)
- strings/bytes: [LastIndexBytes](https://github.com/golang/go/issues/36891)
- [encoding/json](https://github.com/golang/go/issues/53178) and [strings escaping](https://github.com/golang/go/issues/68203)
- [unicode/utf8](https://github.com/golang/go/issues/63347)
- crypto: [64634]](https://github.com/golang/go/issues/64634), [21269](https://github.com/golang/go/issues/21269), [22809](https://github.com/golang/go/issues/22809)
- [encoding/hex](https://github.com/golang/go/issues/68188)

Some of them, especially the crypto, are getting an exception, but most can't get any hand written assembly due to [Go policy](https://go.dev/wiki/AssemblyPolicy) which I find very meaningful. Arguably, we are in 2025, nobody should have to write assembly by hand anymore, but here we are.

So if we had a way to express parallelism without concurrency in Go natively, the majority of the open issue above could be solved easily and by anyone writing Go. And likely even 5% improvement would become acceptable.

> [!NOTE]
> _Auto vectorisation exist._ It is the limited ability of the compiler to figure out where it can add parallelism by looking at non explicitly parallel code. This is a nice to have, but nobody in any language seriously rely on it for performance critical task. Which is why all standard library have assembly in them to work around the language limitation. It is also why we do not run just C or C++ on GPU, but we had to invent new language. GPU would not have taken off if everyone had to develop for each of them in assembly.

# How would that work?

First a bit of naming convention. Go currently manipulate one data at a time. This single element are called _scalar_ or _uniform_ on GPU. When switching to manipulate multiple data at once, it is usually named _varying_ on GPU or _vector_. A _scalar_ operation (addition, multiplication, ...) with a _vector_ produce a _vector_, like so:

/* FIXME: SVG of a scalar by vector operation */

A _vector_ operation with a _vector_ result in a vector like so:

/* FIXME: SVG of a vector by vector operation */

## Types

We will need the ability to specify that a type is a _scalar_ or a _vector_. Maybe something like:
```go
var a scalar int
var b vector(4) int
var c vector(4) []int // A vector of array of int (each entry of the vector point to a different array)
var d []vector(4) int // An array of vector of int (each entry of the array is a vector of int)
```

By default, all types would remain _scalar_ in an implicit declaration to keep backward compability with past Go code.

## *IF*

To write algorithm, the first construct we need is the ability to do `if` in parallel. The idea here is very simple. We will use if, but when operating on _vector_ it will generate a *mask*. Then the operation inside the `if` will only update the portion of the _vector_ that is not masked out. In the `else` branch, we will do exactly the opposite. Once done, we can just restore the *mask* the wayt it was prior to the `if`. Let's look at an example:

```go
var r
test := vector(4) bool{true, false, true, false}
a := vector(4) int{1, 2, 3, 4}
b := vector(4) int{5, 6, 7, 8}

if test {
    r = a * 2
} else {
    r = b - 1
}

assert.Equal(t, r, vector int{2, 5, 6, 7})
```

/* FIXME: SVG of the above code */

Note that all of this code is executed sequentially. Both branch of the if is executed on all the data, just we do not care of the result whne it is masked out. This is manipulating data in parallel, but the code is exactly the same for the entire vector.

## *FOR*

Now, that we have `if` in parallel, we just need to be able to write `for` in parallel. The idea of a `for` in parallel, is that each entry of a _vector_ will have the entire `for` run for it. If another entry use `break` or `continue`, the `for` will still run for all the other entries of the _vector_. This is again something we can do by using *mask*. We will need to remember the *mask* prior to the `for` so that we can continue as `if` no modification on it was done. We will need a *mask* to use to restore for the next loop. This *mask* will be altered when `break` is called. We will use this mask as a starting point of a loop and apply to it the loop condition. If the resulting *mask* is false, we can exit the loop. This *mask* is the *current loop mask*. Finally when encountering a `continue` inside a `for`, we can modify permanently the *current loop mask* to reflect that a column of the _vector_ is not going to be processed anymore in this loop.

Let's look at an example:

```go
var r
for i := vector(4) int{1, 2, 3, 4}; i < 5; i++ {
    if i % 2 == 0 {
        continue
    }
    if i == 3 {
        break
    }
    r += i
}

assert.Equal(t, r, vector(4) int{1, 0, 0, 0})
```

/* FIXME: SVG of the above code */

## Initializing _vector_

So far, we have been manually initializing all our vector with random data. There is definitively benefit to have better and more readable construct. `zig` use `iota` to initialize a _vector_ with increment of one for each entry. I think this would work well in Go too. We would be able to do:

```go
var i vector(4) int = iota

assert.Equal(t, r, vector(4) int{0, 1, 2, 3})
```

The other bit missing is the ability to iterate over an existing array of _scalar_ using _vector_. This would be where we enter a block of code that is operating on _vector_. The starting point of parallelism. As `go` introduced the `go func` combination for concurrency with parallelism aka _goroutine_, we could use `go for` combination to introduce parallelism without concurrency. This would look like:

```go
a := []int{1, 2, 3, 4, 5, 6, 7, 8}

var r vector int
go for _, value := range a {
    r += value
}
```

This would make it possible to write vector length agnostic code and make specifying the vector length optional. We might likely want `len` on a _vector_ type to return the length of a _vector_.

## Going from _vector_ to _scalar_

Very often, we don't just want to manipulate vector, but instead get a _scalar_ as a result. This operation are called `reduce` in most of the CPU language listed above and it makes sense as we **reduce** our _vector_ to one _scalar_. All kind of `reduce` operation should exist, like Add, Mul, Or, ... 

Using this to finish the previous example we would get:

```go
func Sum(a []int) int {
    var r []vector int
    go for _, it := range a {
        r += a
    }
    return reduce.Add(r)
}
```
