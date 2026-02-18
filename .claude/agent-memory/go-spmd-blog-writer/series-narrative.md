# SPMD Blog Series Narrative Arc

## Series Structure (4 posts, all published)

### Post 1: "Data Parallelism: simpler solution for Golang?" (2025-06-19)
- **Role**: Foundation. Introduces ALL core concepts.
- **Opens with**: Why Go lacks SIMD, three approaches to SIMD (auto-vectorization, libraries, language-level)
- **Key argument**: Language-level support keeps code readable and maintainable
- **Introduces**: `go for`, `lanes.Varying[T]`, `uniform` (concept), `lane`, `mask`, `reduce.Add`. Explains why types-in-packages instead of keywords (backward compatibility).
- **Examples**: Simple sum, odd/even counting, bit counting, divergent control flow (array summing)
- **All examples use interactive shortcodes** (step-by-step lane visualization)
- **Format**: TOML front matter (+++), no <!--more-->
- **Closing**: "Let me know if there is anything that need clarification"
- **Link**: -> practical-vector

### Post 2: "What if? Practical parallel data." (2025-06-21)
- **Role**: Real-world application. Shows SPMD applied to Go stdlib operations.
- **Opens with**: Directly into printf verb finding (no preamble)
- **Introduces**: `reduce.Any`, `reduce.FindFirstSet`, `lanes.Count` for index tracking
- **Examples**: Printf '%' finder, hex.Encode, bytes.ToUpper
- **All examples use interactive shortcodes**
- **Key pattern**: Shows existing Go stdlib code THEN the SPMD version
- **Adds `description` field** in front matter
- **Uses <!--more-->** after first shortcode
- **Summary**: Categorizes patterns (scanning, computation+memory, conditional execution)
- **Closing**: "readable code and high performance don't have to be mutually exclusive"
- **Links**: <- go-data-parallelism, -> cross-lane-communication

### Post 3: "Cross-Lane Communication: When Lanes Need to Talk" (2025-07-12)
- **Role**: Advanced concepts. Introduces cross-lane operations.
- **Opens with**: "Most SPMD examples show lanes working independently" -- escalates complexity
- **Introduces**: `lanes.Swizzle`, `lanes.Rotate`, `lanes.ShiftRight/Left`, `lanes.From`, `lanes.Varying[T, 4]` constrained type
- **Example**: Base64 decoding (single complex algorithm)
- **Key difference**: Uses standard markdown code blocks, NOT interactive shortcodes
- **Credits**: Miguel Young de la Sota prominently, links his article
- **Honest about complexity**: Full section "The Complexity Question" discussing trade-offs
- **Presents 3 design options**: Full suite, reduction only, gradual introduction
- **Links to full source**: `**[View Complete Source Code](../../examples/base64-decoder/)**`
- **Links**: <- practical-vector, -> go-spmd-ipv4-parser

### Post 4: "Putting It All Together" (2025-07-13)
- **Role**: Capstone. Combines ALL techniques in a real parser.
- **Format switch**: Uses YAML front matter (---) instead of TOML
- **Adds `tags` field**: ["golang", "performance", "networking", "SIMD", "SPMD"]
- **Opens with**: References previous posts, introduces research foundation
- **Introduces**: `reduce.Mask`, `reduce.Add`, `lanes.Varying[T, 16]`, phase-based algorithm design
- **Shows existing Go stdlib code THEN SPMD version** (before/after pattern)
- **Phases**: Character analysis -> Validation -> Dot extraction -> Field processing
- **Key insight**: Discusses compiler optimization with array range vs range[N]
- **Error handling**: Shows how SPMD can IMPROVE error reporting
- **Ends with open question**: "If most developers can write data parallel code... it is worth it"
- **References section**: Formal list of links
- **Links to full source**: `**[View Complete Source Code](../../examples/ipv4-parser/)**`
- **Declares series complete**: "This concludes our SPMD Go blog series"
- **Links**: <- cross-lane-communication (no next)

## Progression of Complexity
1. Independent lane operations (sum, counting)
2. Masked execution (if/else, for loops)
3. Reduction operations (Any, All, Add, FindFirstSet)
4. Uniform tracking with lanes.Count
5. Cross-lane communication (Swizzle, Rotate)
6. Constrained varying types (lanes.Varying[T, 4], lanes.Varying[T, 16])
7. Multi-phase algorithms combining all techniques
8. Error handling in SPMD context

## Recurring Themes
- **Readability vs performance**: Can we have both?
- **Go stdlib performance gaps**: Links to specific Go issues
- **ISPC and Mojo as prior art**: Referenced throughout
- **Compiler freedom**: "There is no requirement on the compiler for how to implement this"
- **Portability**: Same code works across SIMD widths
- **Trade-off honesty**: Always discusses what's lost alongside what's gained
- **TinyGo as PoC path**: Mentioned in post 4 as practical implementation approach

## Series Navigation Format
```markdown
---

**Previous in series:** [Title](../slug/) - Brief description.

**Next in series:** [Title](../slug/) - Brief description.
```
- Horizontal rule before navigation
- Both links bolded with **Previous/Next in series:**
- Brief description after the dash
- Relative links using `../slug/` format
- Last post has no "Next" link, states series conclusion
