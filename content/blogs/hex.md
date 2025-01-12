+++
date = '2025-01-12T14:38:38-07:00'
draft = true
title = 'What if? Readable, SIMD hex convertion'
description = 'Using an hypothetical go for construct to implement hex.go'
+++

In my previous article, we looked at how we could introduce a more readable and maintainable way to express data parallelism in Go and used it for a quick `Sum` example. In this article, I will look at implementing `hex.Encode` and `hex.Decode` with it to start a discussion based on some more practical example.

# Encode

Current implementation for hex.Encode looks like:

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

We will have to change a bit the algorithm and iterate on the `EncodedLen` of `src` to have a continuous iterator. With that in mind, let's see how it would look like:

```go
func Encore(dst, src []byte) int {
    go for idx := range EncodedLen(len(src)) {
        var d byte

        s = src[idx>>1]
        if idx & 0x1 == 0 {
            v = hextable[s>>4]
        } else {
            v = hextable[s&0x0f]
        }
        dst[idx] = v
    }
    return EncodedLen(len(src))
}
```

The code work the other way around from the original version. The compiler will know that `idx` is a continuous stream of integer. This is important as it means all memory access (both write or read are going to be continuous) and it help the compiler to be able to use SIMD instruction effectivelly. You want to keep random memory access when using SIMD to small array of known size (like `hextable`) just due to the limit of space in the instruction (It is easier to encode an access to an array as a pointer + 8bits offset, than any random pointer size).

## Decode

