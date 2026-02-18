# Logo & Icon Concepts (context.help)

## Overview

This document presents logo and icon concepts for context.help, aligned with the brand guidelines established in `brand-guidelines.md`.

**Core Visual Metaphor:** Knowledge graph as interconnected nodes forming a brain/network hybrid — representing the synthesis of human intelligence (brain/ctxt) and structural foundation (network/dPKMS).

---

## Logo Concepts

### Concept 1: "Neural Network Graph"

**Description:**
A minimalist representation combining:
- 5-7 circular nodes arranged in a brain-like formation
- Connecting lines showing knowledge flow
- Central node emphasized (the "context" hub)

**Variations:**

**A. Full Logo (Horizontal)**
```
[Icon]  context.help
        dPKMS + ctxt
```

**B. Compact Logo (Horizontal)**
```
[Icon]  context.help
```

**C. Stacked Logo (Vertical)**
```
    [Icon]
context.help
```

**D. Icon Only**
```
[Icon]
```

**Visual Structure:**
```
      ○ (Bright Cyan)
     /|\
    ○ ● ○ (Deep Navy center, Cyan outer)
     \|/
      ○ (Mint Green)
```

- Center node: Deep Navy (#1a2332) — the "context" hub
- Outer nodes: Bright Cyan (#00d9ff) and Mint Green (#3dd68c) — distributed knowledge
- Connecting lines: Slate Gray (#4a5568), 2px stroke
- Subtle glow effect on center node to suggest intelligence/processing

---

### Concept 2: "Substrate + Brain"

**Description:**
A two-layer design representing the dPKMS/ctxt architecture:
- Bottom layer: Grid/foundation pattern (substrate/dPKMS)
- Top layer: Neural connections (intelligence/ctxt)

**Visual Structure:**
```
  Neural Layer (Bright Cyan):
    ○─○─○
     \│/
      ○

  ═══════ (separator line, subtle)

  Grid Layer (Deep Navy):
  ╔═══╗
  ║ • ║ (foundation pattern)
  ╚═══╝
```

**Application:**
- Grid pattern uses Deep Navy
- Neural nodes use Bright Cyan
- Connecting lines use gradient from Cyan to Navy
- Can animate: grid appears first (substrate), then neural layer (brain)

---

### Concept 3: "Context Loop"

**Description:**
A continuous loop representing the knowledge cycle:
1. Capture (input node)
2. Process (pipeline)
3. Store (graph)
4. Retrieve (output)

**Visual Structure:**
```
     Capture
        ↓
    Process → Store
        ↑
     Retrieve
```

Rendered as:
- Circular flow with 4 nodes at cardinal points
- Arrows showing directional flow (clockwise)
- Central hub where all paths intersect
- Uses gradient from Bright Cyan → Mint Green around the loop

**Symbolism:**
- Emphasizes "always current" (continuous refresh)
- Shows closed-loop system (sovereignty)
- Central hub = user's context

---

### Concept 4: "The Mention Mark"

**Description:**
Abstract representation of the `@mention` system as logo element:
- Stylized @ symbol integrated with network nodes
- @ represents semantic identity
- Nodes around @ represent entities

**Visual Structure:**
```
    ○
   /│\
  ○ @ ○
   \│/
    ○
```

- @ symbol: Deep Navy, custom typography
- Surrounding nodes: Bright Cyan
- Connecting lines form the circular part of @
- Works well at small sizes (favicon, app icon)

---

## Recommended Primary Logo: Concept 1 (Neural Network Graph)

**Rationale:**
- Most versatile across sizes (scales from favicon to billboard)
- Clearly communicates "knowledge graph" without being literal
- Balances technical precision with approachability
- Distinguishes from generic "node network" logos through brain-like arrangement

**Technical Specifications:**

### Full Logo Lockup
```
Width: 240px (icon: 48px, spacing: 16px, wordmark: 176px)
Height: 48px
Minimum size: 120px wide (scales proportionally)
Clear space: 16px on all sides
```

### Icon Specifications
```
Canvas: 48x48px (scales to 16, 32, 64, 128, 256, 512)
Node size: 6px diameter (outer), 12px diameter (center)
Stroke width: 2px
Corner radius: N/A (circular nodes)
Export formats: SVG (primary), PNG (48, 128, 256, 512), ICO (16, 32, 48)
```

### Wordmark Specifications
```
Font: Inter Bold (700 weight)
Size: 24px for "context.help"
Size: 12px for "dPKMS + ctxt" (optional tagline)
Color: Deep Navy (#1a2332) on light backgrounds
Color: Warm White (#fdfcf9) on dark backgrounds
Letter spacing: -0.02em (tight)
```

---

## Icon System

### Primary Icon: Neural Network Graph (matches logo)

**Usage:** App icon, favicon, social media profile

**Variations:**

**Light Background:**
```svg
<svg viewBox="0 0 48 48">
  <!-- Outer nodes: Bright Cyan -->
  <circle cx="24" cy="8" r="3" fill="#00d9ff"/>
  <circle cx="8" cy="24" r="3" fill="#00d9ff"/>
  <circle cx="40" cy="24" r="3" fill="#00d9ff"/>
  <circle cx="16" cy="38" r="3" fill="#3dd68c"/>
  <circle cx="32" cy="38" r="3" fill="#3dd68c"/>

  <!-- Connecting lines: Slate Gray -->
  <line x1="24" y1="8" x2="24" y2="18" stroke="#4a5568" stroke-width="2"/>
  <line x1="8" y1="24" x2="18" y2="24" stroke="#4a5568" stroke-width="2"/>
  <line x1="40" y1="24" x2="30" y2="24" stroke="#4a5568" stroke-width="2"/>
  <line x1="16" y1="38" x2="21" y2="28" stroke="#4a5568" stroke-width="2"/>
  <line x1="32" y1="38" x2="27" y2="28" stroke="#4a5568" stroke-width="2"/>

  <!-- Center node: Deep Navy with glow -->
  <circle cx="24" cy="24" r="6" fill="#1a2332"/>
  <circle cx="24" cy="24" r="8" fill="#00d9ff" opacity="0.2"/>
</svg>
```

**Dark Background:**
Same structure, but:
- Center node: Warm White (#fdfcf9)
- Outer nodes: Keep Bright Cyan and Mint Green
- Glow: Bright Cyan with increased opacity (0.4)

### Secondary Icons: Feature Icons

Following the brand guidelines (geometric, minimal, 2px stroke, rounded corners):

#### 1. **Pipeline Icon**
```
Metaphor: Flow diagram
Shape: Horizontal flow with 3 stages
Style: Outlined rectangles connected by arrows
Colors: Bright Cyan
```

#### 2. **Knowledge Graph Icon**
```
Metaphor: Connected nodes
Shape: 4-5 circles with interconnecting lines
Style: Outlined circles, solid lines
Colors: Mint Green
```

#### 3. **Registry Icon**
```
Metaphor: Distributed network
Shape: Central node with satellite nodes
Style: Outlined circles, dashed lines to show federation
Colors: Bright Cyan
```

#### 4. **Entity Icon**
```
Metaphor: Semantic identity
Shape: @ symbol with subtle node accent
Style: Outlined @ with small circle at top-right
Colors: Deep Navy
```

#### 5. **Jobs/Queue Icon**
```
Metaphor: Processing pipeline
Shape: Stacked horizontal bars with play indicator
Style: Outlined rectangles, filled triangle (play)
Colors: Amber (#ffc247)
```

#### 6. **Storage Icon**
```
Metaphor: Database cylinder
Shape: Classic DB icon with layered disks
Style: Outlined cylinder
Colors: Slate Gray
```

#### 7. **Sovereignty Icon**
```
Metaphor: Home/lock
Shape: House outline with lock symbol inside
Style: Outlined shapes
Colors: Deep Navy
```

#### 8. **Plugin Icon**
```
Metaphor: Puzzle piece
Shape: Single jigsaw piece with connection points
Style: Outlined piece
Colors: Mint Green
```

---

## Logo Usage Guidelines

### Do's
- ✓ Use logo on solid backgrounds (light or dark)
- ✓ Maintain minimum clear space (16px)
- ✓ Use provided color variations for background contexts
- ✓ Use SVG format for web and scalable applications
- ✓ Center-align logo in hero sections
- ✓ Use icon-only version when space is limited (< 120px width)

### Don'ts
- ✗ Don't place logo on busy backgrounds or images
- ✗ Don't rotate or skew the logo
- ✗ Don't change node colors or arrangement
- ✗ Don't add effects (drop shadow, bevel, emboss)
- ✗ Don't place logo on colored backgrounds that reduce contrast below 4.5:1
- ✗ Don't recreate or trace logo — use provided files

### Color Variations

**Primary (Light Background):**
- Nodes: Bright Cyan (#00d9ff), Mint Green (#3dd68c)
- Center: Deep Navy (#1a2332)
- Wordmark: Deep Navy (#1a2332)

**Inverted (Dark Background):**
- Nodes: Bright Cyan (#00d9ff), Mint Green (#3dd68c)
- Center: Warm White (#fdfcf9)
- Wordmark: Warm White (#fdfcf9)

**Monochrome (Single-color contexts):**
- All elements: Same color as surrounding text
- Use when color reproduction is limited

**Grayscale:**
- Nodes: #666666
- Center: #1a2332
- Lines: #999999
- Use for print applications where color is unavailable

---

## Animation Guidelines

### Logo Animation (Loading/Splash Screen)

**Concept:** "Knowledge Formation"

1. Grid substrate fades in (0-0.3s)
2. Outer nodes appear sequentially (0.3-0.6s, stagger: 0.1s)
3. Connecting lines draw from nodes to center (0.6-0.9s)
4. Center node pulses into existence (0.9-1.2s)
5. Subtle glow effect on center (1.2-1.5s)
6. Wordmark fades in (1.5-1.8s)

**Duration:** 1.8 seconds total
**Easing:** ease-out for drawing, ease-in-out for pulses
**Loop:** Single play on load, can repeat pulse on center node

### Icon Animation (State Changes)

**Processing State:**
```
- Center node pulses (1s interval)
- Glow effect oscillates opacity (0.2 → 0.4 → 0.2)
- Outer nodes subtle fade (0.8 → 1.0 → 0.8)
```

**Success State:**
```
- Quick scale up (1.0 → 1.15 → 1.0, 0.3s)
- Green tint overlay (0.3s fade in/out)
```

**Error State:**
```
- Horizontal shake (-2px → +2px → 0, 0.4s)
- Red tint overlay (0.3s fade in, stays)
```

---

## File Naming Convention

```
logo/
  context-help-logo-full.svg          # Full horizontal lockup
  context-help-logo-compact.svg       # Compact horizontal
  context-help-logo-stacked.svg       # Vertical lockup
  context-help-logo-dark.svg          # Dark background variant
  context-help-icon.svg               # Icon only
  context-help-icon-dark.svg          # Icon for dark backgrounds
  context-help-monochrome.svg         # Single-color version

icons/
  feature-pipeline.svg                # Pipeline feature icon
  feature-graph.svg                   # Knowledge graph icon
  feature-registry.svg                # Registry icon
  feature-entity.svg                  # Entity icon
  feature-jobs.svg                    # Jobs/queue icon
  feature-storage.svg                 # Storage icon
  feature-sovereignty.svg             # Sovereignty icon
  feature-plugin.svg                  # Plugin icon

favicon/
  favicon.ico                         # Multi-size ICO (16, 32, 48)
  favicon-16x16.png                   # PNG fallback
  favicon-32x32.png
  favicon-48x48.png
  apple-touch-icon.png                # iOS (180x180)
  android-chrome-192x192.png          # Android
  android-chrome-512x512.png

social/
  og-image.png                        # Open Graph (1200x630)
  twitter-card.png                    # Twitter (1200x600)
  linkedin-banner.png                 # LinkedIn (1584x396)
```

---

## Implementation Checklist

### Phase 1: Core Assets
- [ ] Design primary logo (Concept 1: Neural Network Graph)
- [ ] Create SVG files (full, compact, stacked, icon-only)
- [ ] Generate color variations (light, dark, monochrome, grayscale)
- [ ] Export favicon sizes (ICO, PNG, Apple, Android)

### Phase 2: Feature Icons
- [ ] Design 8 feature icons following brand system
- [ ] Create SVG files with consistent stroke width and sizing
- [ ] Document icon usage in component library

### Phase 3: Social & Marketing
- [ ] Create Open Graph images (1200x630)
- [ ] Create Twitter card images (1200x600)
- [ ] Create LinkedIn banner (1584x396)
- [ ] Design email signature lockup

### Phase 4: Animation & Interactive
- [ ] Implement logo loading animation (CSS/Lottie)
- [ ] Create state change animations for icon
- [ ] Document animation timing and easing

### Phase 5: Documentation
- [ ] Create usage guide PDF
- [ ] Build interactive logo playground (web)
- [ ] Write contributor guidelines for logo usage

---

## Next Steps

1. **Create SVG implementations** of Concept 1 (Neural Network Graph)
2. **Generate favicon suite** for web deployment
3. **Design feature icon set** (8 icons) following brand system
4. **Build logo playground** to demonstrate usage rules
5. **Create social media templates** with logo lockups

---

**Version:** 1.0.0
**Last Updated:** 2026-01-27
**Maintainer:** ContextHelp Design Team
