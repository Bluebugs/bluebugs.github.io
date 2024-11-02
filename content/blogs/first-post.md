+++
date = '2024-10-29T20:49:45-06:00'
title = 'The SuperH family'
description = 'In depth SuperH instructions set'
+++

In this post, I’ll explore the SuperH instruction set and its relevance today, along with a tool I created to navigate it.

A few years ago, I met some of the members of the team behind the development of the _J-Core_, a _SuperH_ clone, and had some really interesting discussions with them. Using this [resource](http://www.shared-ptr.com/sh_insns.html), I created, for my own understanding, a JSON file and the dynamic table below to help me navigate and understand the _SuperH_ instruction set. I also added J2-related instructions that were not present in the initial material. As I turn off my _AWS_ account, I realized I could just share it via a _GitHub_ page. Here is a bit more context about _SuperH_.

# The Evolution of _SuperH_

The _SuperH_ family was a fascinating family of CPUs. It emerged from the understanding of how CISC CPUs allowed for denser code with lower memory bandwidth, giving them an edge over RISC CPUs, despite RISC having the advantage of simpler instruction decoders. Engineers studied what instructions compilers were able to generate and selected those that led to more compact binaries. For that reason, _SuperH_ has **16-bit instructions**, despite manipulating **32-bit data**, and these instructions can perform multiple operations. This allowed for array or stack manipulation, for example, with fewer instructions than a classic RISC CPU would require. It only has **16 registers**, but compilers are quite good at register allocation. _SuperH_ ended up with a simpler decode stage than x86, for example, even though it uses microcode, while having code density comparable to the x86 family at the time.

The history of _SuperH_ was cut short by the Asian economic crisis and never really recovered. It was present in many of the gaming consoles of the 90s, like the [Saturn](https://en.wikipedia.org/wiki/Sega_Saturn) or [Dreamcast](https://en.wikipedia.org/wiki/Dreamcast). It was the predecessor to the Arm [Thumb](https://en.wikipedia.org/wiki/ARM_architecture#Thumb) instruction set and the [MIPS16](https://en.wikipedia.org/wiki/MIPS_architecture). Today, all the patents related to the _SuperH_ family have expired, enabling the start of its revival with the _J-Core_ family for IoT devices.

# Take away

As _SuperH_ was designed to be more efficient at decoding instructions and using memory, I believe it would still have an edge and be relevant in any environment where energy and performance matter. Our main constraint for most computing use cases today is the ratio of energy to performance; this marks an important change over the last few decades. This ratio won’t improve in the future. The past advantage that x86 had with performance, regardless of other considerations, is now a hindrance, and the technological debt of this family makes it very difficult to compete with ARM, which has been much more focused on energy efficiency.

However, as a 90s CPU, bringing _SuperH_ outside of the IoT scope would require figuring out how to:

- **Add a 64-bit memory mode**: This would have a direct impact on memory bandwidth, as all pointers would instantly double in size, but it is required to run a lot of modern workload. Mixing 32-bit and 64-bit applications or perhaps developing ABI for libraries to support both modes would likely help. Most applications don't need more than 3GB of memory.

- **Add SIMD instructions**: SIMD allows for manipulating multiple data in parallel. They inherently have a high data-to-instruction ratio, which improves memory bandwidth usage for useful data. To compete with ARM, x86, or even RISC-V, you need SIMD instructions. Unfortunately, due to how poorly the software industry has made it to write data-parallel code, any new SIMD instruction set would require a large amount of hand-written code, representing a massive cost for any new architecture.

- **Review the instructions generated for JIT**: Today, we have a lot of software that performs _Just-In-Time_ compilation (_JS_ being one example, but also _QEMU_, which is useful for running non-native software). Such technologies didn’t exist in the 90s, so it would be beneficial to ensure that JIT workloads are well supported.

- **Review the need for VMs**: Again, in the 90s, VMs were not common. Today, VM workloads are the norm in data centers and on our laptops.

- **Design a modern MMU/TLB that enables a large L1 cache**: This requires a balance between memory waste and efficiency in accessing and using that memory while also ensuring that the software ecosystem can utilize it. Large L1 caches are possible with larger memory pages. Once you select a memory page size, the entire ecosystem becomes locked to that choice. x86 is significantly impacted by the 4KB page limit today, which Apple’s ARM hardware has managed to bypass.

Even with a full _Debian_ running today on the _SuperH_ family, it would take a lot of work to bring the _SuperH_ family to handle modern workloads outside of IoT. Nevertheless, I believe that as computing workloads become increasingly constrained by energy, the _SuperH_ family—or at least its concepts—will be more relevant than ever.

Have fun exploring the instruction set below. You can filter, search, and tag (locally stored) through the instruction set.

{{< insns >}}
