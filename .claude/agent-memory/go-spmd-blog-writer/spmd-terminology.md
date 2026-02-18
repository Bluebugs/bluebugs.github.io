# SPMD Terminology and Conventions

## Core Terms (always bold on first use, backtick-wrapped after)
- **`lanes.Varying[T]`**: Type from `lanes` package representing a SIMD register containing multiple values of type T, one per lane. Unconstrained form.
- **`lanes.Varying[T, N]`**: Constrained varying type where N specifies the group size (e.g., `lanes.Varying[byte, 4]` for groups of 4)
- **`lane`**: Each value/position in a SIMD register. Numbered starting from 1 in prose ("Lane 1"), but 0-indexed in code context
- **`mask`**: Boolean per-lane tracking which lanes are active. "Used to enable or disable lanes during computation"
- **`uniform`**: Conceptual term (NOT a keyword) - a variable with the same value across all lanes. Regular Go variables are uniform by default.
- **`go for`**: The SPMD loop construct. Always written as two words with backticks: `go for`

### Why Types Instead of Keywords
- `varying` and `uniform` were originally envisioned as Go keywords
- Adding keywords to Go breaks backward compatibility (any code using these as identifiers breaks)
- Solution: make them generic types in existing `lanes` and `reduce` packages
- `varying` -> `lanes.Varying[T]` (lives in lanes package since lanes make values vary)
- `uniform` -> regular Go variable (or `reduce.Uniform[T]` when explicit type needed)
- Inside `go for` loops, the loop variable is implicitly `lanes.Varying[T]`

## Standard Library Packages
- `lanes` package: Lane operations and varying types
  - `lanes.Varying[T]`: Unconstrained varying type
  - `lanes.Varying[T, N]`: Constrained varying type (groups of N)
  - `lanes.Count(v)`: Returns number of SIMD lanes for type
  - `lanes.Index()`: Current lane index
  - `lanes.Broadcast(value, lane)`: Broadcast from one lane
  - `lanes.Rotate(value, offset)`: Rotate values across lanes
  - `lanes.Shuffle(value, indices)`: Arbitrary permutation
  - `lanes.Swizzle(data, indices)`: Parallel table lookup
  - `lanes.ShiftRight(value, amount)`: Bitwise shift right
  - `lanes.ShiftLeft(value, amount)`: Bitwise shift left
  - `lanes.From(slice)`: Create varying from slice
- `reduce` package: Varying-to-uniform reductions
  - `reduce.Uniform[T]`: Explicit uniform type (rarely needed, regular Go vars are uniform)
  - `reduce.Add(v)`: Sum across lanes
  - `reduce.Any(v)`: True if any lane is true
  - `reduce.All(v)`: True if all lanes are true
  - `reduce.Or(v)`: Bitwise OR reduction
  - `reduce.Add(v)`: Sum reduction (formerly reduce.Sum)
  - `reduce.Mask(v)`: Convert bool lanes to bitmask
  - `reduce.FindFirstSet(v)`: Index of first true lane

## Syntax Conventions in Code
- `lanes.Varying[T]` for unconstrained varying: `lanes.Varying[int]`, `lanes.Varying[bool]`
- `lanes.Varying[T, N]` for constrained varying: `lanes.Varying[byte, 4]`, `lanes.Varying[uint8, 16]`
- `range[N]` for constrained SPMD loops: `go for i := range[4] 16`
- `go for _, v := range slice` for basic SPMD loops (v is implicitly varying)
- `go for i, v := range slice` when index needed (i and v are implicitly varying)
- `go for i := range N` for integer range (i is implicitly varying)

## How Concepts Are Introduced
1. **Post 1** (go-data-parallelism): Defines vocabulary, shows basic sum, if/else masking, for loop masking, divergent control flow. Explains why types-in-packages instead of keywords.
2. **Post 2** (practical-vector): Applies to real Go stdlib: printf verb finding, hex encode, bytes.ToUpper
3. **Post 3** (cross-lane-communication): Introduces Swizzle, Rotate, constrained `lanes.Varying[T, 4]`. Uses base64 decoding
4. **Post 4** (go-spmd-ipv4-parser): Combines all concepts. Uses `lanes.Varying[T, 16]`, reduce.Mask, reduce.FindFirstSet

## Key Distinctions Made
- `go for` (SPMD data parallelism) vs `go func()` (goroutine concurrency)
- uniform (scalar, same across lanes) vs varying (vector, per-lane) -- conceptual, not keyword-level
- `reduce.Any` makes `if` act like normal `if` with jump (uniform exit)
- Continue in inner loop = per-lane mask, Break = permanent per-lane mask

## Visualization Conventions (in shortcodes)
- 4 lanes shown (Lane 1-4 or Lane 0-3)
- Active lanes: white background
- Inactive/masked lanes: grey background (#f0f0f0), muted text (#aaa), italic
- Mask row shows true/false per lane
- Values shown as "-" when not yet assigned
- Step-by-step with Previous/Next buttons
- Code pane on left, visualization on right
- Info pane below explains current step

## Shortcode HTML Class for Varying Types
- OLD: `<span class="gohypo">varying</span> <span class="goty">int</span>`
- NEW: `<span class="gofn">lanes.Varying</span><span class="gopunct">[</span><span class="goty">int</span><span class="gopunct">]</span>`
- The `.gohypo` class (orange) was used for hypothetical keywords
- Now uses `.gofn` class (yellow) since it's a package function/type, not a keyword
