# E2B Design System

Extracted from https://e2b.dev on 2026-01-26

## Overview

E2B's design system is characterized by a brutalist-meets-futuristic aesthetic with strong typography, high contrast, monospaced elements, and ASCII-art-inspired decorative treatments. The design balances technical precision with playful creativity.

---

## Typography

### Font Family
- **Primary**: IBM Plex Sans
- **Weights Available**: 400 (regular), 500, 600, 700, 500 italic
- **Font Features**:
  - Stylistic sets `ss02` and `ss03` enabled
  - Optimized legibility with `text-rendering: optimizeLegibility`
  - Antialiasing: `-webkit-font-smoothing: antialiased`, `-moz-osx-font-smoothing: grayscale`

### Typography Hierarchy
- **Hero Headlines**: Large, bold, uppercase treatments with ASCII decorations
- **Section Headers**: Uppercase labels in brackets like `[FEATURES]`, `[GET STARTED]`
- **Body Text**: Clean, legible IBM Plex Sans in regular weight
- **Code/Technical**: Monospaced with syntax highlighting

### Distinctive Typography Patterns
- Uppercase section labels
- Bracketed text for emphasis: `[TEXT]`
- ASCII art decorations: `===`, `***`, `---`, `>>>`, `<<<`
- Animated text rotators for dynamic content

---

## Color Palette

### Core Colors
E2B uses a dual-theme system with light and dark modes:

#### Light Mode (Default)
- **Background**: Clean white/off-white
- **Text**: High contrast black
- **Accents**: Strategic use of color for CTAs and highlights

#### Dark Mode
- **Implementation**: CSS variables prefixed with `--dark--`
- **Inversion**: Background dotted elements receive `filter: invert(1)`
- **Trigger**: `prefers-color-scheme: dark` media query

### Functional Colors
- **Code Background**: `--color--bg-codesnippet`
- **Scrollbar Thumb**: `#333`
- **Interactive Elements**: Transform-based hover states rather than color changes

---

## Spacing System

### Layout
- **Grid**: Responsive with major breakpoint at 992px (tablet/desktop)
- **Container**: Generous padding and whitespace
- **Component Spacing**: Consistent vertical rhythm

### Responsive Breakpoints
```css
/* Mobile/Tablet */
@media (max-width: 991px)

/* Desktop */
@media (min-width: 992px)
```

---

## Interactive Elements

### Buttons & CTAs
- **Hover States**:
  - Transform: `translate(-4px, -4px)` on larger screens
  - Creates dynamic, offset effect
- **Transitions**: `0.15s ease`
- **Toggle Buttons**: Full ARIA support with `role="button"` and `aria-pressed` states

### Links
- Simple, clean styling
- Consistent hover treatments
- Arrow decorators for external/action links: `→`, `↗`

---

## Components

### Code Blocks
```css
background: var(--color--bg-codesnippet);
scrollbar-width: thin;
scrollbar-color: #333 transparent;
```

- **Syntax Highlighting**: Custom color overrides
- **Scrollbars**: Thin, minimal styling
- **Line Numbers**: Standard formatting

### Cards
- Clean borders
- Minimal shadows
- Hover effects with 4px translation
- Content hierarchy with clear headings

### Navigation
- Uppercase labels
- ARIA-labeled interactive elements
- Full keyboard support
- Responsive mobile menu

---

## Animation & Motion

### Timing Functions
- **Standard Transitions**: `0.15s ease`
- **Flip Animation**: `28s cubic-bezier(0.8, 0, 0.2, 1) infinite`
- **Scroll Animation**: `15s linear infinite` for carousels

### Performance Optimizations
```css
will-change: transform;
backface-visibility: hidden;
```

### Responsive Animation Behavior
- Animations disabled on mobile (`@media > 991px`)
- Touch detection via `ontouchstart` and `DocumentTouch`
- Reduced motion support implied through performance focus

### Animation Patterns
1. **Carousel/Flip**: Long-duration cubic-bezier for smooth infinite loops
2. **Hover States**: Quick 0.15s ease transitions
3. **Page Load**: Staggered reveals using `animation-delay`

---

## Visual Effects & Decorative Elements

### ASCII Art Decorators
E2B extensively uses ASCII-like decorative elements:

```
***            Dividers/separators
===            Underlines
>>>            Directional indicators
[TEXT]         Bracketed labels
(↓↓)           Interactive hints
✶✶             Sparkle decorations
@@@            Block patterns
---            Lines
```

### Dotted/Grid Backgrounds
- Subtle background patterns
- Inverted in dark mode for consistency

### Geometric Patterns
- Terminal-style boxes
- Grid layouts with ASCII borders
- Monospaced alignment

---

## Accessibility

### Features
- **Keyboard Navigation**: Full support with `preventDefault` handlers
- **ARIA Labels**: Comprehensive labeling for screen readers
- **Toggle States**: `aria-pressed` for button states
- **Semantic HTML**: Proper heading hierarchy and landmarks
- **User Select**: Strategic `user-select: none` for UI chrome

### Font Rendering
```css
text-rendering: optimizeLegibility;
-webkit-font-smoothing: antialiased;
-moz-osx-font-smoothing: grayscale;
```

---

## Unique Design Characteristics

### Brutalist Elements
1. **Raw Typography**: Uppercase, bold headers
2. **Minimal Decoration**: Focus on content and function
3. **High Contrast**: Black and white foundation
4. **Geometric Precision**: Grid-based layouts

### Technical/Developer Focus
1. **Monospaced Elements**: Code-first aesthetic
2. **Terminal Aesthetics**: Command-line inspired UI
3. **ASCII Decorations**: Playful technical references
4. **System Stats**: Real-time data displays (CPU, RAM)

### Motion Philosophy
- Subtle, purposeful animations
- Performance-first (GPU acceleration)
- Disabled on mobile for better UX
- Long-duration ambient animations for brand personality

---

## Implementation Notes

### CSS Architecture
- CSS variables for theming
- Mobile-first responsive approach
- Performance-optimized animations
- Vendor prefixes for cross-browser support

### Touch Support
```javascript
// Touch detection
'ontouchstart' in window || DocumentTouch
```

### Scroll Behavior
- Smooth scrolling for carousels
- Thin custom scrollbars
- Optimized for performance

---

## Brand Expression

### Voice & Tone
- **Technical but Friendly**: Professional with playful ASCII touches
- **Developer-First**: Speaks the language of engineers
- **Future-Forward**: Sci-fi/terminal aesthetics
- **Open & Accessible**: Clear documentation and examples

### Visual Metaphors
1. **Sandbox/Container**: Boxed UI elements
2. **Terminal/CLI**: Monospaced, code-like treatments
3. **System/Technical**: CPU/RAM displays, technical specs
4. **Connectivity**: Arrows, flow indicators

---

## Usage Guidelines

### Do's
✓ Use IBM Plex Sans for all text
✓ Maintain high contrast ratios
✓ Embrace ASCII decorative elements
✓ Use uppercase for labels and emphasis
✓ Apply subtle hover transforms
✓ Optimize animations for performance
✓ Support both light and dark modes

### Don'ts
✗ Don't use soft, rounded shapes (stay geometric)
✗ Don't over-animate (keep it subtle)
✗ Don't use gradients excessively
✗ Don't mix font families
✗ Don't create heavy animations on mobile

---

## Key Takeaways

E2B's design system is a masterclass in **technical brutalism with personality**:

1. **Distinctive Typography**: IBM Plex Sans with bold uppercase treatments
2. **ASCII Aesthetics**: Playful technical decorations throughout
3. **Performance-First Motion**: Optimized animations with GPU acceleration
4. **Dual-Theme Excellence**: Seamless light/dark mode switching
5. **Developer-Centric**: Terminal-inspired UI that speaks to engineers
6. **Accessible Foundation**: Comprehensive ARIA and keyboard support

The system successfully balances **technical precision** with **creative expression**, creating a memorable brand identity that resonates with its developer audience while maintaining exceptional usability and performance.
