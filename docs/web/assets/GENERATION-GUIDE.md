# Asset Generation Guide

This guide shows how to generate remaining brand assets from the SVG source files.

## Prerequisites

### Option 1: ImageMagick (Recommended)
```bash
# macOS
brew install imagemagick

# Ubuntu/Debian
sudo apt-get install imagemagick

# Verify installation
convert --version
```

### Option 2: Inkscape (Alternative)
```bash
# macOS
brew install inkscape

# Ubuntu/Debian
sudo apt-get install inkscape
```

### Option 3: Online Tools (No Installation)
- https://realfavicongenerator.net/ (favicons)
- https://favicon.io/ (favicons)
- https://www.canva.com/ (social media images)
- https://www.figma.com/ (comprehensive design tool)

---

## 1. Favicon Generation

### Using ImageMagick

```bash
cd ./assets

# Create favicon directory if not exists
mkdir -p favicon

# Generate PNG favicons from SVG
convert -background none logo/context-help-icon.svg -resize 16x16 favicon/favicon-16x16.png
convert -background none logo/context-help-icon.svg -resize 32x32 favicon/favicon-32x32.png
convert -background none logo/context-help-icon.svg -resize 48x48 favicon/favicon-48x48.png
convert -background none logo/context-help-icon.svg -resize 180x180 favicon/apple-touch-icon.png
convert -background none logo/context-help-icon.svg -resize 192x192 favicon/android-chrome-192x192.png
convert -background none logo/context-help-icon.svg -resize 512x512 favicon/android-chrome-512x512.png

# Create multi-size ICO file
convert favicon/favicon-16x16.png favicon/favicon-32x32.png favicon/favicon-48x48.png favicon/favicon.ico
```

### Using Inkscape

```bash
cd ./assets
mkdir -p favicon

# Export specific sizes
inkscape logo/context-help-icon.svg --export-type=png --export-filename=favicon/favicon-16x16.png -w 16 -h 16
inkscape logo/context-help-icon.svg --export-type=png --export-filename=favicon/favicon-32x32.png -w 32 -h 32
inkscape logo/context-help-icon.svg --export-type=png --export-filename=favicon/favicon-48x48.png -w 48 -h 48
inkscape logo/context-help-icon.svg --export-type=png --export-filename=favicon/apple-touch-icon.png -w 180 -h 180
inkscape logo/context-help-icon.svg --export-type=png --export-filename=favicon/android-chrome-192x192.png -w 192 -h 192
inkscape logo/context-help-icon.svg --export-type=png --export-filename=favicon/android-chrome-512x512.png -w 512 -h 512
```

### Using RealFaviconGenerator (Online)

1. Go to https://realfavicongenerator.net/
2. Upload `logo/context-help-icon.svg`
3. Customize settings:
   - iOS: Use 180x180 with no background
   - Android: Use 192x192 and 512x512
   - Windows: Use 144x144
4. Download generated package
5. Extract to `favicon/` directory

### HTML Integration

Add to your `<head>` section:

```html
<!-- Favicons -->
<link rel="icon" type="image/svg+xml" href="/assets/logo/context-help-icon.svg">
<link rel="icon" type="image/png" sizes="32x32" href="/assets/favicon/favicon-32x32.png">
<link rel="icon" type="image/png" sizes="16x16" href="/assets/favicon/favicon-16x16.png">
<link rel="apple-touch-icon" sizes="180x180" href="/assets/favicon/apple-touch-icon.png">

<!-- Android -->
<link rel="manifest" href="/site.webmanifest">

<!-- site.webmanifest -->
{
  "name": "context.help",
  "short_name": "ctxt",
  "icons": [
    {
      "src": "/assets/favicon/android-chrome-192x192.png",
      "sizes": "192x192",
      "type": "image/png"
    },
    {
      "src": "/assets/favicon/android-chrome-512x512.png",
      "sizes": "512x512",
      "type": "image/png"
    }
  ],
  "theme_color": "#1a2332",
  "background_color": "#fdfcf9",
  "display": "standalone"
}
```

---

## 2. Social Media Assets

### Open Graph Image (1200×630px)

**Recommended Approach: Use Figma/Canva**

1. Create new design:
   - Dimensions: 1200×630px
   - Background: Deep Navy (#1a2332)

2. Add elements:
   - Icon: context-help-icon-dark.svg (120×120px, centered-left)
   - Headline: "context.help" (72px, Inter Bold, white)
   - Subheadline: "Local-first context engine" (32px, Inter, light gray)
   - Accent: Bright Cyan line or gradient

3. Export as PNG: `social/og-image.png`

**Alternative: Using ImageMagick**

```bash
cd ./assets
mkdir -p social

# Create base canvas with navy background
convert -size 1200x630 xc:"#1a2332" social/og-base.png

# Composite icon (requires manual positioning)
convert social/og-base.png \
  logo/context-help-icon-dark.svg -resize 120x120 \
  -gravity west -geometry +100+0 \
  -composite social/og-temp.png

# Add text (requires ImageMagick with freetype)
convert social/og-temp.png \
  -font Inter-Bold -pointsize 72 -fill "#fdfcf9" \
  -gravity west -annotate +250+0 "context.help" \
  social/og-image.png

# Clean up temp files
rm social/og-temp.png social/og-base.png
```

**HTML Integration:**

```html
<!-- Open Graph -->
<meta property="og:title" content="context.help">
<meta property="og:description" content="Local-first context engine that transforms multimodal content into structured knowledge">
<meta property="og:image" content="https://context.help/assets/social/og-image.png">
<meta property="og:url" content="https://context.help">
<meta property="og:type" content="website">

<!-- Twitter Card -->
<meta name="twitter:card" content="summary_large_image">
<meta name="twitter:title" content="context.help">
<meta name="twitter:description" content="Local-first context engine that transforms multimodal content into structured knowledge">
<meta name="twitter:image" content="https://context.help/assets/social/twitter-card.png">
```

### Twitter Card (1200×600px)

Same approach as Open Graph, but adjust dimensions:
- Canvas: 1200×600px
- Keep same design elements, adjust vertical centering

### LinkedIn Banner (1584×396px)

Horizontal banner format:
- Canvas: 1584×396px
- Icon: Left-aligned (80×80px)
- Wordmark: Center-aligned
- Tagline: "dPKMS + ctxt" below wordmark

---

## 3. Logo PNG Exports (for backward compatibility)

Some platforms don't support SVG. Export PNG versions:

```bash
cd ./assets
mkdir -p logo/png

# Export logos at various sizes
convert -background none logo/context-help-logo-compact.svg -resize 480x96 logo/png/logo-compact-1x.png
convert -background none logo/context-help-logo-compact.svg -resize 960x192 logo/png/logo-compact-2x.png
convert -background none logo/context-help-logo-compact.svg -resize 1440x288 logo/png/logo-compact-3x.png

convert -background none logo/context-help-icon.svg -resize 64x64 logo/png/icon-64.png
convert -background none logo/context-help-icon.svg -resize 128x128 logo/png/icon-128.png
convert -background none logo/context-help-icon.svg -resize 256x256 logo/png/icon-256.png
convert -background none logo/context-help-icon.svg -resize 512x512 logo/png/icon-512.png
```

---

## 4. Monochrome Variants

For contexts where color isn't available (print, single-color screens):

```bash
# Create monochrome SVG manually or use ImageMagick
convert logo/context-help-icon.svg -colorspace Gray logo/context-help-icon-mono.svg
```

Or edit the SVG manually:
1. Open `context-help-icon.svg` in a text editor
2. Replace all `fill` and `stroke` color values with `currentColor`
3. Save as `context-help-icon-mono.svg`

This allows the icon to inherit the text color of its parent element.

---

## 5. Print Assets (Optional)

For high-quality print materials:

```bash
# Export as PDF (vector, scalable)
inkscape logo/context-help-logo-full.svg --export-type=pdf --export-filename=logo/print/logo-full.pdf

# Export as EPS (for legacy design tools)
inkscape logo/context-help-logo-full.svg --export-type=eps --export-filename=logo/print/logo-full.eps

# High-res PNG for print (300 DPI)
convert -background none -density 300 logo/context-help-logo-full.svg logo/print/logo-full-300dpi.png
```

---

## Verification Checklist

After generation, verify all assets:

### Favicons
- [ ] favicon.ico (16, 32, 48px)
- [ ] favicon-16x16.png
- [ ] favicon-32x32.png
- [ ] favicon-48x48.png
- [ ] apple-touch-icon.png (180x180)
- [ ] android-chrome-192x192.png
- [ ] android-chrome-512x512.png
- [ ] site.webmanifest

### Social Media
- [ ] og-image.png (1200x630)
- [ ] twitter-card.png (1200x600)
- [ ] linkedin-banner.png (1584x396)

### Logo PNGs
- [ ] logo-compact-1x.png (480x96)
- [ ] logo-compact-2x.png (960x192)
- [ ] logo-compact-3x.png (1440x288)
- [ ] icon-64.png through icon-512.png

### Quality Checks
- [ ] Transparent backgrounds where appropriate
- [ ] Sharp edges (no blur or artifacts)
- [ ] Correct colors (compare to brand guidelines)
- [ ] File sizes reasonable (<200KB per image)

---

## Automated Generation Script

Create a shell script to generate all assets at once:

```bash
#!/bin/bash
# generate-assets.sh

set -e

ASSETS_DIR="./assets"
cd "$ASSETS_DIR"

echo "Creating directories..."
mkdir -p favicon logo/png social

echo "Generating favicons..."
convert -background none logo/context-help-icon.svg -resize 16x16 favicon/favicon-16x16.png
convert -background none logo/context-help-icon.svg -resize 32x32 favicon/favicon-32x32.png
convert -background none logo/context-help-icon.svg -resize 48x48 favicon/favicon-48x48.png
convert -background none logo/context-help-icon.svg -resize 180x180 favicon/apple-touch-icon.png
convert -background none logo/context-help-icon.svg -resize 192x192 favicon/android-chrome-192x192.png
convert -background none logo/context-help-icon.svg -resize 512x512 favicon/android-chrome-512x512.png
convert favicon/favicon-16x16.png favicon/favicon-32x32.png favicon/favicon-48x48.png favicon/favicon.ico

echo "Generating logo PNGs..."
convert -background none logo/context-help-logo-compact.svg -resize 480x96 logo/png/logo-compact-1x.png
convert -background none logo/context-help-logo-compact.svg -resize 960x192 logo/png/logo-compact-2x.png
convert -background none logo/context-help-icon.svg -resize 64x64 logo/png/icon-64.png
convert -background none logo/context-help-icon.svg -resize 128x128 logo/png/icon-128.png
convert -background none logo/context-help-icon.svg -resize 256x256 logo/png/icon-256.png
convert -background none logo/context-help-icon.svg -resize 512x512 logo/png/icon-512.png

echo "Done! Manual steps remaining:"
echo "  1. Create social media images using Figma/Canva"
echo "  2. Create site.webmanifest"
echo "  3. Test favicons in browser"
echo "  4. Test social preview on social networks"
```

Save as `assets/generate-assets.sh` and run:
```bash
chmod +x assets/generate-assets.sh
./assets/generate-assets.sh
```

---

## Next Steps

1. Run automated generation script (or manual commands)
2. Create social media images using Figma/Canva
3. Test favicon display in various browsers
4. Test social preview using:
   - Facebook Sharing Debugger: https://developers.facebook.com/tools/debug/
   - Twitter Card Validator: https://cards-dev.twitter.com/validator
   - LinkedIn Post Inspector: https://www.linkedin.com/post-inspector/

5. Update website templates with asset references
6. Commit generated assets to repository

---

**Questions?** See `assets/README.md` for asset usage or `docs/web/brand-guidelines.md` for design guidelines.
