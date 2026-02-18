# ContextHelp Brand Assets

This directory contains all official logo, icon, and visual assets for context.help.

## Directory Structure

```
assets/
├── logo/                           # Logo files
│   ├── context-help-logo-full.svg        # Full lockup (icon + wordmark + tagline)
│   ├── context-help-logo-compact.svg     # Compact lockup (icon + wordmark)
│   ├── context-help-logo-stacked.svg     # Stacked lockup (vertical)
│   ├── context-help-icon.svg             # Icon only (light background)
│   └── context-help-icon-dark.svg        # Icon only (dark background)
│
├── icons/                          # Feature and UI icons
│   └── feature/
│       ├── pipeline.svg                  # Pipeline/flow icon
│       ├── knowledge-graph.svg           # Knowledge graph icon
│       ├── registry.svg                  # Registry/federation icon
│       ├── entity.svg                    # Entity/@mention icon
│       ├── jobs.svg                      # Jobs/queue icon
│       ├── storage.svg                   # Storage/database icon
│       ├── sovereignty.svg               # Sovereignty/ownership icon
│       └── plugin.svg                    # Plugin/extensibility icon
│
├── favicon/                        # Favicon files (to be generated)
│   ├── favicon.ico
│   ├── favicon-16x16.png
│   ├── favicon-32x32.png
│   ├── apple-touch-icon.png
│   └── android-chrome-*.png
│
└── social/                         # Social media assets (to be created)
    ├── og-image.png                      # Open Graph (1200x630)
    ├── twitter-card.png                  # Twitter (1200x600)
    └── linkedin-banner.png               # LinkedIn (1584x396)
```

## Logo Usage

### Quick Reference

| Logo Variant | When to Use | Minimum Width |
|--------------|-------------|---------------|
| **Full** | Marketing pages, hero sections | 240px |
| **Compact** | Navigation bars, headers | 180px |
| **Stacked** | Tight vertical spaces, mobile | 120px |
| **Icon Only** | App icons, favicons, avatars | 48px |

### Color Variants

- **Default (Light Background):** Use `context-help-icon.svg` and logo files as-is
- **Dark Background:** Use `context-help-icon-dark.svg` (center node is white instead of navy)
- **Monochrome:** For contexts where color is unavailable, use single-color rendering

### Clear Space

Maintain minimum clear space of 16px on all sides of the logo.

### HTML Usage Examples

**Hero Section (Full Logo):**
```html
<img src="/assets/logo/context-help-logo-full.svg" alt="context.help" width="280" height="48">
```

**Navigation Bar (Compact Logo):**
```html
<img src="/assets/logo/context-help-logo-compact.svg" alt="context.help" width="230" height="48">
```

**Favicon:**
```html
<link rel="icon" type="image/svg+xml" href="/assets/logo/context-help-icon.svg">
```

**Dark Mode Support:**
```html
<picture>
  <source srcset="/assets/logo/context-help-icon-dark.svg" media="(prefers-color-scheme: dark)">
  <img src="/assets/logo/context-help-icon.svg" alt="context.help">
</picture>
```

## Feature Icons Usage

Feature icons are designed at 24x24px with 2px stroke weight, following the brand guidelines.

### HTML Usage

```html
<!-- In feature grid -->
<div class="feature">
  <img src="/assets/icons/feature/pipeline.svg" alt="" width="24" height="24">
  <h3>Multimodal Pipelines</h3>
  <p>Process text, URLs, images, audio, and video</p>
</div>
```

### Icon Reference

| Icon | File | Use Case | Brand Color |
|------|------|----------|-------------|
| Pipeline | `pipeline.svg` | Flow diagrams, processing steps | Bright Cyan |
| Knowledge Graph | `knowledge-graph.svg` | Graph features, entity connections | Mint Green |
| Registry | `registry.svg` | Federation, subscriptions | Bright Cyan |
| Entity | `entity.svg` | @mentions, semantic identity | Deep Navy |
| Jobs | `jobs.svg` | Queue status, background tasks | Amber |
| Storage | `storage.svg` | Database, persistence | Slate Gray |
| Sovereignty | `sovereignty.svg` | Privacy, ownership, control | Deep Navy |
| Plugin | `plugin.svg` | Extensibility, integrations | Mint Green |

## Brand Colors

Reference from `docs/web/brand-guidelines.md`:

```css
/* Primary */
--color-navy: #1a2332;
--color-cyan: #00d9ff;
--color-white: #fdfcf9;

/* Secondary */
--color-slate: #4a5568;
--color-mint: #3dd68c;
--color-amber: #ffc247;

/* Semantic */
--color-success: #10b981;
--color-warning: #f59e0b;
--color-error: #ef4444;
--color-info: #6366f1;
```

## Typography

**Headings & Body:**
- Font: Inter (with system fallback)
- Fallback stack: `-apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif`

**Code/Monospace:**
- Font: JetBrains Mono
- Fallback stack: `'Fira Code', 'Consolas', 'Monaco', monospace`

## License

All brand assets in this directory are part of the ContextHelp project and are licensed under AGPL-3.0.

**Usage Rights:**
- ✓ Use in official ContextHelp documentation
- ✓ Use in community projects that integrate with ContextHelp
- ✓ Use in articles, tutorials, and educational content about ContextHelp
- ✗ Do not use to imply official endorsement without permission
- ✗ Do not modify the logo or use it in derivative branding

## Next Steps

### To Be Generated

1. **Favicon Suite** (from icon SVG):
   - favicon.ico (16, 32, 48px multi-size)
   - PNG exports (16, 32, 48, 180, 192, 512px)
   - Apple touch icons
   - Android chrome icons

2. **Social Media Assets**:
   - Open Graph image (1200x630px)
   - Twitter card (1200x600px)
   - LinkedIn banner (1584x396px)

3. **Additional Exports**:
   - PNG exports of logos (1x, 2x, 3x)
   - Monochrome variants
   - Print-ready formats (PDF, EPS)

### Generation Tools

To generate favicons from the icon SVG:
```bash
# Using ImageMagick or similar
convert context-help-icon.svg -resize 16x16 favicon-16x16.png
convert context-help-icon.svg -resize 32x32 favicon-32x32.png
convert context-help-icon.svg -resize 48x48 favicon-48x48.png

# Create multi-size ICO
convert favicon-16x16.png favicon-32x32.png favicon-48x48.png favicon.ico
```

Or use online tools:
- https://realfavicongenerator.net/
- https://favicon.io/

## Questions or Issues?

For brand guidelines questions or asset requests:
1. Check `docs/web/brand-guidelines.md` for detailed guidelines
2. Review `docs/web/logo-concepts.md` for design rationale
3. Open an issue in the repository

---

**Version:** 1.0.0
**Last Updated:** 2026-01-27
