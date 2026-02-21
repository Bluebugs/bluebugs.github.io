# Front Matter Patterns

## TOML Format (used in posts 1-3, non-SPMD posts)
```toml
+++
date = '2025-06-19T18:48:59-07:00'
draft = false
featured_image = 'images/banff.jpg'
featured_image_class = 'cover bg-center'
title = 'Data Parallelism: simpler solution for Golang?'
+++
```

## YAML Format (used in post 4)
```yaml
---
title: "Putting It All Together"
date: 2025-07-13T14:00:00-07:00
description: "Fast IPv4 Parsing with SPMD Go"
featured_image: 'images/lakelouise.jpg'
featured_image_class: 'cover bg-center'
tags: ["golang", "performance", "networking", "SIMD", "SPMD"]
---
```

## Fields Used Across Posts
| Field | Post 1 | Post 2 | Post 3 | Post 4 | Notes |
|-------|--------|--------|--------|--------|-------|
| date | Yes | Yes | Yes | Yes | Always PST timezone (-07:00) |
| draft | false | (absent) | (absent) | (absent) | Only explicitly set in post 1 |
| title | Yes | Yes | Yes | Yes | Single quotes in TOML, double in YAML |
| featured_image | Yes | Yes | Yes | Yes | Always 'images/name.jpg' |
| featured_image_class | Yes | Yes | Yes | Yes | Always 'cover bg-center' |
| description | No | Yes | Yes | Yes | Added starting from post 2 |
| tags | No | No | No | Yes | Only in post 4 (YAML format) |

## Non-SPMD Posts
- tests-debt: TOML, has description, no tags
- layoff-tech-debt: TOML, draft=true, no description
- first-post (SuperH): TOML, has description, no tags

## Recommendations for New Posts
- Use YAML (---) format for consistency with the latest post
- Always include: title, date, description, featured_image, featured_image_class
- Include tags: ["go", "spmd", "simd", ...relevant topics]
- Set draft: true for review
- Use PST timezone (-07:00 or -08:00 depending on DST)
- Choose featured_image from: banff.jpg, lakelouise.jpg, kicking-horse.jpg, strathcona.jpg, bugaboo.jpg
- Lake Louise (lakelouise.jpg) is most used for SPMD posts (posts 2, 3, 4)
- Banff (banff.jpg) used for post 1

## Image Usage
- banff.jpg: Post 1 (go-data-parallelism)
- lakelouise.jpg: Posts 2, 3, 4 (practical-vector, cross-lane, ipv4-parser)
- kicking-horse.jpg: tests-debt
- strathcona.jpg: layoff-tech-debt
- bugaboo.jpg: unused (available for new posts)
