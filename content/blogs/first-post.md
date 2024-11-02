+++
date = '2024-10-29T20:49:45-06:00'
draft = true
title = 'The SuperH family'
description = 'In depth SuperH instructions set'
+++

A few years ago, I met some of the member of the team behind the development of the J-Core a SuperH clone and had some really interesting discussion with them. Using http://www.shared-ptr.com/sh_insns.html, I created, for my own understanding, a JSON file and the below dynamic table to help me navigate and understand the instruction set. I added to that JSON the J2 related instructions that were not present in the initial material. You can filter, search and tag (locally stored) through the instruction set to learn more about the _SuperH_ instruction set.

# History

The _SuperH_ family was a really interesting family of CPU. It was born of the learning of how CISC CPU allowed for denser code with lower memory bandwidth giving them a edge over RISC CPU despite them having the advantage of having a simpler instruction decoder. Engineer studied what instructions compilers were able to generate and selected instruction that lead to more compact binary. For that reason, _SuperH_ have 16 bits instructions wide, despite manipulating 32bits data and those instructions can do more than one things. This allowed to do array or stack manipulation for example with less instructions than a classic RISC CPU would. It only has 16 registers, but compiler are pretty good at register allocation. _SuperH_ end up with a simpler decode stage than x86 for example, even if it use microcode, while having code density comparable to the x86 family at the time.

The _SuperH_ history got cut short with the Asian economics crisis and never really recovered. It was present in a lot of the 90s gaming console like the [Saturn](https://en.wikipedia.org/wiki/Sega_Saturn) or [Dreamcast](https://en.wikipedia.org/wiki/Dreamcast). It was the father to the Arm [Thumb](https://en.wikipedia.org/wiki/ARM_architecture#Thumb) instructions set and the [MIPS16](https://en.wikipedia.org/wiki/MIPS_architecture). Today, all the patents related to the _SuperH_ family have expired which enable the start of its revival with the J-Core family for IoT devices.

# Take away

As _SuperH_ by design was more efficient at decoding instruction and at using memory, I would think it would still have an edge and be relevant in any environment were energy and performance matter... And this is an important change over the last decades, our main constraint for most of our computing use case is our ratio of energy to performance. This won't improve in the future. The past edge that x86 had with just performance, the rest be damn is now an hindrance and the technological debt of this family is making it really hard to compete with ARM which were much more focused on energy efficiency.

Still the _SuperH_ was a 90s CPU, bringing it outside of the IoT scope would requires to figure out how to:

- __Add a 64bits memory mode__: This has direct impact on the memory bandwidth as all pointers instantly double in size. So mixing 32bits and 64bits applications or maybe even figuring out how to have library that can support both mode would likely help. Most application don't need 3GB of memory...
- __Add SIMD instructions__: SIMD allow for manipulating multiple data in parallel. They inherently have a high data to instruction ratio which improve the consumption of memory bandwidth for useful data. To compete with ARM, x86 or even RISC-V, you need SIMD instructions. Sadly, due to how poor the software industry has been at making it easy to write data parallel code, any new SIMD instructions set would require a large amount of code to be hand written being a massive cost to any new architecture.
- __Review the instructions generated for JIT__: Today we do have a lot of software that do Just In Time compilation (JS being one, but also QEMU which would be useful to run non native software). Those didn't exist in the 90s and it would be good to ensure that JIT workload are looking good to.
- __Review the need for VM__: Again in the 90s, VM were not a thing. Today VM workload are the norm in the data center, but also on our laptop.
- __Design a modern MMU/TLB system that enable large L1 cache__. This require a balance between memory waste and efficiency in accessing and using that memory. Large L1 cache are possible by having large memory page. Once you select your memory page, the entire ecosystem will get locked. x86 is really impacted by the 4KB page limit today.

Even with a full _Debian_ running today on the _SuperH_ family, it would be a lot of work to bring the _SuperH_ family to more complex work load. Never the less, I would think as compute work load get more and more constrained by energy, the _SuperH_ family or at least its concept will be more relevant than ever. Have fun looking at the instructions set below.

{{< insns >}}
