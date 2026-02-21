# Blog Writer Agent Memory

## Quick Reference
- See `writing-style.md` for voice, tone, and structural patterns
- See `spmd-terminology.md` for SPMD vocabulary and conventions
- See `shortcodes-and-formatting.md` for Hugo shortcodes and technical formatting
- See `series-narrative.md` for the arc across the SPMD blog series
- See `front-matter-patterns.md` for Hugo front matter conventions

## Key Findings (Top-Level)

### Author Identity
- Cedric Bail, French (references this: "I am french, so let's go with Champagne")
- Located in Canada (all featured images are Canadian Rockies: Banff, Lake Louise, Kicking Horse, Strathcona, Bugaboo)
- Background in systems/compilers, familiar with SuperH, ISPC, Mojo, CUDA
- Uses Disqus comments (configured in hugo.toml)

### Blog Types
1. **SPMD series** (4 posts, all published): go-data-parallelism, practical-vector, cross-lane-communication, go-spmd-ipv4-parser
2. **Tech opinion pieces** (2 posts): tests-debt (published), layoff-tech-debt (draft)
3. **Hardware/architecture** (1 post): first-post about SuperH

### Syntax Change (2026-02)
- `varying` and `uniform` are NO LONGER keywords -- they are types in packages
- `varying T` -> `lanes.Varying[T]` (unconstrained) or `lanes.Varying[T, N]` (constrained)
- `uniform T` -> regular Go variable (or `reduce.Uniform[T]` when explicit needed)
- Reason: adding keywords to Go breaks backward compatibility
- `go for` syntax unchanged, `range[N]` unchanged
- Loop variables in `go for` are implicitly `lanes.Varying[T]`
- In shortcode HTML: `<span class="gohypo">varying</span>` replaced with `<span class="gofn">lanes.Varying</span><span class="gopunct">[</span>...<span class="gopunct">]</span>`

### Critical Conventions
- SPMD posts use `featured_image_class: 'cover bg-center'`
- Images are Canadian Rocky Mountain photos from `static/images/`
- Front matter mixes TOML (+++) and YAML (---) formats; SPMD posts use TOML for earlier ones, YAML for later (ipv4-parser)
- `<!--more-->` used as content summary separator in posts 2-4
- Series navigation links at bottom with `---` horizontal rule separator
- Code in SPMD intro post uses interactive shortcodes; later posts use standard markdown code blocks
- `readfile` shortcode available for embedding Go files: `{{< readfile file="examples/file.go" >}}`
- Full Go example files linked with: `**[View Complete Source Code](../../examples/base64-decoder/)**`
