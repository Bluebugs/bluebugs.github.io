# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a Hugo-based static website for Cedric Bail's personal blog, hosted on GitHub Pages. The site focuses on Go programming, SIMD/SPMD concepts, and technical discussions about data parallelism.

## Build Commands

### Development
- `hugo server --buildDrafts` - Start development server with draft content
- `hugo server` - Start development server (production mode)

### Production Build
- `hugo` - Build static site to `public/` directory
- `hugo --minify` - Build with minification for production

### Content Management
- `hugo new content/blogs/[title].md` - Create new blog post
- `hugo list drafts` - List all draft content

## Architecture

### Content Structure
- **Blog posts**: Located in `content/blogs/` with Hugo front matter
- **Interactive demos**: Custom Hugo shortcodes in `layouts/shortcodes/` with embedded JavaScript
- **Styling**: Custom CSS in `assets/ananke/css/` extending the Ananke theme

### Key Components

#### SPMD Demos
Interactive JavaScript demos illustrating SPMD (Single Program Multiple Data) concepts:
- `layouts/shortcodes/spmd-*.html` - Individual demo components
- Each demo includes embedded CSS and JavaScript for visualization
- Demos show lane-based execution with real-time stepping through code

#### Theme Integration
- Uses Hugo Ananke theme as base with custom CSS overrides
- Custom shortcodes for technical content presentation
- Responsive design with specialized styling for code demonstrations

### Hugo Configuration
- **Config file**: `hugo.toml` with custom parameters for blog pagination and styling
- **Theme**: Ananke with custom CSS extensions
- **Modules**: Uses hugo-admonitions for enhanced content blocks
- **Build settings**: Drafts enabled for development

### Static Assets
- **Images**: Stored in `static/images/` and served from `/images/`
- **JSON data**: `static/json/insns.json` for instruction set data
- **Generated content**: Hugo builds to `public/` directory

## Development Workflow

### Adding New Blog Posts
1. Create new markdown file in `content/blogs/`
2. Include proper Hugo front matter with date, title, and featured_image
3. Use custom shortcodes for interactive demos if needed
4. Test with `hugo server --buildDrafts`

### Working with SPMD Demos
- Each demo is a self-contained shortcode with embedded CSS/JS
- Lane visualization uses grid layouts with step-by-step execution
- Uniform vs varying value concepts are central to the demos

### Deployment
- Site builds automatically via GitHub Actions to GitHub Pages
- `public/` directory contains the built static site
- Base URL configured for `bluebugs.github.io`

## Go Integration

The repository includes Go modules (`go.mod`) for:
- Hugo module dependencies
- Potential Go code examples that can be executed and tested
- SPMD concept implementations and demonstrations