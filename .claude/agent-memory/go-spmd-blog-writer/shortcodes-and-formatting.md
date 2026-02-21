# Hugo Shortcodes and Formatting

## Available Shortcodes

### Interactive SPMD Demos (used in Post 1 and Post 2)
All in `layouts/shortcodes/`:
- `spmd-sum.html` - Sum example with 4 lanes, reduce.Add
- `spmd-oddeven.html` - Odd/even counting with if/else masking
- `spmd-countbits.html` - Bit counting with nested for loop, uniform values display
- `spmd-countarray.html` - Divergent control flow, variable-length inner loops
- `spmd-printf-verbs.html` - Printf '%' finder with reduce.Any and lanes.Count
- `spmd-hex.html` - Hex encoding with if/else masking
- `spmd-toupper.html` - ToUpper with two-phase scan/convert pattern

Usage: `{{< spmd-sum >}}`, `{{< spmd-hex >}}`, etc.

### Utility Shortcodes
- `readfile.html` - Embed and syntax-highlight a Go file: `{{< readfile file="examples/file.go" >}}`
- `insns.html` - SuperH instruction set interactive table (first-post only)

### Hugo Admonitions Module
Configured via: `[[module.imports]] path = "github.com/KKKZOZ/hugo-admonitions"`
Available for note/warning/tip blocks (not currently used in SPMD posts but available)

## Shortcode Architecture

### Naming Pattern
- IDs use suffix pattern: `spmd-demo_sum`, `spmd-lane-0-v_sum`
- Classes use shared CSS from `spmd-demos.css`
- Each shortcode is self-contained with embedded `<style>`, `<div>`, `<script>`
- Some use Hugo `$uid := .Ordinal` for unique container IDs (hex, toupper)

### CSS Classes (from spmd-demos.css)
- `.spmd-container` - Outer wrapper
- `.spmd-demo` - Flex row container (code + viz)
- `.spmd-code-pane` - Left side with Go code
- `.spmd-viz-pane` - Right side with lane grid
- `.spmd-info-pane` - Bottom description panel
- `.spmd-controls` - Previous/Next buttons
- `.spmd-lane-data-grid` - Grid for lane values (auto + repeat(4, 1fr))
- `.spmd-grid-header`, `.spmd-grid-label`, `.spmd-grid-cell`
- `.spmd-final-result` - Results display area
- `.inactive-lane` - Grey out masked lanes
- `.spmd-inline-code` - Inline code in descriptions (dark bg, monospace)

### Go Syntax Highlighting Classes (custom, not Hugo built-in)
- `.gokw` - Keywords (purple: #c586c0)
- `.gofn` - Functions and package types like lanes.Varying (yellow: #dcdcaa)
- `.goty` - Types (teal: #4ec9b0)
- `.gohypo` - DEPRECATED: was for hypothetical keywords varying/uniform. Now use .gofn for lanes.Varying
- `.govar` - Variables (light blue: #9cdcfe)
- `.goop` - Operators (light grey: #d4d4d4)
- `.gonum` - Numbers (green: #b5cea8)
- `.gopunct` - Punctuation including generic brackets [] (light grey: #d4d4d4)
- `.gocomment` - Comments (green: #6a9955)
- `.highlight` - Current line highlight (dark: #44475a)

### Varying Type HTML Pattern
Old: `<span class="gohypo">varying</span> <span class="goty">int</span>`
New: `<span class="gofn">lanes.Varying</span><span class="gopunct">[</span><span class="goty">int</span><span class="gopunct">]</span>`
Constrained: `<span class="gofn">lanes.Varying</span><span class="gopunct">[</span><span class="goty">byte</span><span class="gopunct">,</span> <span class="gonum">4</span><span class="gopunct">]</span>`

## Code Block Patterns

### Standard Markdown Code Blocks (Posts 3-4)
```go
// code here
```
Used for longer code examples in cross-lane and ipv4 posts.

### Interactive Shortcodes (Posts 1-2)
Used for step-through demonstrations with lane visualization.

### When to Use Which
- **Interactive shortcodes**: When showing step-by-step lane execution helps understanding
- **Standard code blocks**: For longer algorithms, reference implementations, before/after comparisons
- **readfile shortcode**: For linking to full example files in examples/ directory

## Hugo Configuration Notes (hugo.toml)
- Theme: ananke
- Custom CSS: `["custom.css", "spmd-demos.css"]` in params.custom_css
- Drafts built by default: `builddrafts = true`
- Pagination: 3 posts per page
- Goldmark markdown with block attributes enabled
- Disqus comments: shortname "bluebugs"
- Hugo admonitions module imported

## Content Summary Separator
`<!--more-->` used in posts 2-4 to control excerpt on listing pages.
Not used in post 1 (go-data-parallelism).

## Image Conventions
- All images in `static/images/`
- Featured images: Canadian Rocky Mountain landscapes (JPG)
- Referenced as `'images/filename.jpg'` (relative to static root)
- `featured_image_class: 'cover bg-center'` consistently used
- Available images: banff.jpg, lakelouise.jpg, kicking-horse.jpg, strathcona.jpg, bugaboo.jpg
