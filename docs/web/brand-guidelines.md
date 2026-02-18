# Brand Guidelines (context.help)

## Overview

This document defines the visual identity, voice, and messaging framework for **context.help** — a decentralized, local-first context engine built on dPKMS (substrate) and ctxt (brain).

## Brand Positioning

**Tagline:** "Your knowledge, always current, always yours"

**Core Promise:** Transform raw multimodal content into structured, contextualized knowledge consumable by humans and AI agents alike — with full sovereignty and zero platform lock-in.

**Market Position:** The foundational "context layer" for personal and organizational AI, emphasizing:
- Local-first architecture with optional cloud sync
- Decentralized registry federation
- Knowledge sovereignty and portability
- Intelligence without vendor lock-in

---

## Target Audiences

### Primary: Context Publishers ("Context Specialists")
Practitioners who package expertise into subscribable knowledge products.

**Tone:** Professional, empowering, technical-but-accessible
**Key Messages:**
- "Package expertise that stays current"
- "Distribute without building infrastructure"
- "Monetize with subscriptions and credits"
- "Traceability without DRM"

### Secondary: Small Teams (Buyers)
Teams inside larger orgs and small companies seeking curated knowledge feeds.

**Tone:** Pragmatic, ROI-focused, collaborative
**Key Messages:**
- "Subscribe to curated registries that improve decisions"
- "Keep knowledge current without adding meetings"
- "External expertise, clear boundaries"

### Tertiary: Individuals (Power Users)
Researchers, founders, engineers who maintain personal knowledge workflows.

**Tone:** Direct, tool-focused, efficiency-oriented
**Key Messages:**
- "Plug in high-signal context feeds"
- "Pull just enough context when needed"
- "Works with your existing tools"

---

## Visual Identity

### Color Palette

**Primary Colors:**
- **Deep Navy:** `#1a2332` - Trust, depth, sovereignty
- **Bright Cyan:** `#00d9ff` - Intelligence, connectivity, signals
- **Warm White:** `#fdfcf9` - Clarity, openness, accessibility

**Secondary Colors:**
- **Slate Gray:** `#4a5568` - Utility, infrastructure, substrate
- **Mint Green:** `#3dd68c` - Growth, knowledge, freshness
- **Amber:** `#ffc247` - Insight, alerts, value

**Semantic Colors:**
- **Success/Active:** `#10b981` (emerald green)
- **Warning/Attention:** `#f59e0b` (amber)
- **Error/Critical:** `#ef4444` (red)
- **Info/Neutral:** `#6366f1` (indigo)

**System Colors:**
- **dPKMS (Substrate):** Deep Navy + Slate Gray (foundation, reliability)
- **ctxt (Brain):** Bright Cyan + Mint Green (intelligence, action)

### Typography

**Headings:**
- Font Family: **Inter** (with system fallback: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif)
- Weights: 700 (bold) for H1-H2, 600 (semibold) for H3-H4
- Style: Clean, geometric, technical-friendly

**Body Text:**
- Font Family: **Inter** (with system fallback)
- Weight: 400 (regular) for body, 500 (medium) for emphasis
- Style: Readable, modern, professional

**Monospace (Code/CLI):**
- Font Family: **JetBrains Mono** (with fallback: "Fira Code", "Consolas", "Monaco", monospace)
- Weight: 400 (regular) for code blocks, 500 (medium) for inline code
- Style: Developer-friendly, clear distinction from prose

**Scale:**
```
H1: 48px / 3rem (tight line-height: 1.1)
H2: 36px / 2.25rem (line-height: 1.2)
H3: 28px / 1.75rem (line-height: 1.3)
H4: 20px / 1.25rem (line-height: 1.4)
Body: 16px / 1rem (line-height: 1.6)
Small: 14px / 0.875rem (line-height: 1.5)
Caption: 12px / 0.75rem (line-height: 1.4)
```

### Logo & Iconography

**Logo Concept:**
- Wordmark: "context.help" in Inter Bold
- Symbol: Interconnected nodes forming a brain/network hybrid (knowledge graph metaphor)
- Variants: Full logo, icon-only, monochrome

**Icon Style:**
- Geometric, minimal, 2px stroke weight
- Rounded corners (4px radius) for approachability
- Use outlined style (not filled) for tools/actions
- Use filled style for status/states

**Visual Metaphors:**
- **Knowledge Graph:** Connected nodes, flowing edges
- **Substrate:** Foundation layer, grid patterns, structure
- **Intelligence:** Neurons, signals, light rays
- **Sovereignty:** Lock, shield, home base

---

## Voice & Tone

### Brand Voice Attributes

**Core Principles:**
1. **Precise:** Clear technical accuracy without jargon overload
2. **Empowering:** Users are in control, not dependent on platforms
3. **Pragmatic:** Focus on outcomes and jobs-to-be-done
4. **Respectful:** Acknowledge user expertise and intelligence

**What We Are:**
- Direct and honest about capabilities and limitations
- Technical when needed, accessible when possible
- Confident without being arrogant
- Focused on user sovereignty and portability

**What We Avoid:**
- Marketing hype and superlatives ("revolutionary", "game-changing")
- Vendor lock-in language ("our ecosystem", "our platform")
- Buzzwords without substance ("synergy", "paradigm shift")
- Talking down to users or oversimplifying technical concepts

### Tone by Context

| Context | Tone | Example |
|---------|------|---------|
| **Marketing Pages** | Confident, clear, benefit-focused | "Transform raw content into structured knowledge. No vendor lock-in, no platform dependence." |
| **Documentation** | Precise, instructional, helpful | "The dPKMS job queue guarantees crash-safe recovery and resumable execution." |
| **CLI Output** | Concise, informative, actionable | "✓ 3 items processed, 2 entities resolved" |
| **Error Messages** | Clear, actionable, blame-free | "Pipeline failed: Missing API key. Set OPENAI_API_KEY in ~/.ctxt/config.toml" |
| **Social/Community** | Friendly, collaborative, curious | "Interesting use case! How are you handling entity resolution for domain-specific terms?" |

---

## Messaging Framework

### Value Propositions

**For Context Publishers:**
- "Package your expertise into subscribable, queryable knowledge products"
- "Distribute without building infrastructure — we handle sync, federation, and receipts"
- "Monetize with subscriptions and credits while maintaining traceability"
- "Provide value beyond PDFs — structured, agent-usable, always current"

**For Teams:**
- "Subscribe to curated registries that improve retrieval and decisions"
- "Keep knowledge current without adding meetings or overhead"
- "Bring external expertise into your workflows with clear boundaries"

**For Individuals:**
- "Plug high-signal context feeds into your existing tools"
- "Pull just enough context when you need it"
- "Own your knowledge graph — export, migrate, or self-host anytime"

### Key Differentiators

1. **Local-First Architecture**
   - "Your data stays on your machine until you choose to sync"
   - "Works offline, syncs when ready"

2. **Decentralized Registries**
   - "Subscribe to knowledge feeds from anyone, anywhere"
   - "No central platform gatekeeping access"

3. **Knowledge Sovereignty**
   - "Export everything: Markdown + JSON + SQLite bundles"
   - "No proprietary formats, no lock-in"

4. **Two-Package Design**
   - "dPKMS (substrate): Durability, correctness, sovereignty"
   - "ctxt (brain): Intelligence, usability, actionability"

5. **Plugin Architecture**
   - "Extend without forking — plugins hook into formal APIs"
   - "Community extensions without core modification"

---

## Writing Guidelines

### Grammar & Style

**Capitalization:**
- Product names: "ContextHelp", "dPKMS", "ctxt"
- Commands: Always use backticks: `ctxt analyze`, `dpkms serve`
- Features: Sentence case unless proper noun: "knowledge graph", "transactional job queue"

**Mentions:**
- Format as: `@namespace.slug`
- Example: "Tag this as `@ux.research-synthesis`"

**Terminology:**
- Use "knowledge objects" (not "documents" or "items")
- Use "registries" (not "repositories" or "feeds")
- Use "entities" (not "topics" or "concepts")
- Use "pipelines" (not "workflows" or "processes")
- Use "mentions" (not "tags" or "references") when referring to `@namespace.slug`

**Active Voice:**
- Preferred: "dPKMS executes jobs safely"
- Avoid: "Jobs are executed by dPKMS"

**Second Person:**
- Use "you" when addressing users
- Example: "You can subscribe to external registries"

### Content Patterns

**Feature Descriptions:**
```
[Feature Name]
[One-line benefit statement]
[2-3 sentences explaining how it works]
[Optional: Link to detailed docs]
```

**CLI Help Text:**
```
[Command] [args]  [Short description]

[Longer description if needed]

Options:
  --flag    Description of flag
```

**Error Messages:**
```
[Icon] [What went wrong]
[Why it happened or what was expected]
[What to do next — actionable step]
```

**Changelog Entries:**
```
### [Version] - [Date]

**Added**
- Feature name: Brief description of capability

**Changed**
- Area: What changed and why

**Fixed**
- Issue: What was broken and now works
```

---

## Marketing Assets

### Homepage Hero Section

**Headline:** "Your knowledge, always current, always yours"

**Subheadline:** "A local-first context engine that transforms multimodal content into structured, queryable knowledge — for humans and AI agents."

**Primary CTA:** "Get Started" (link to installation docs)
**Secondary CTA:** "Read the Docs" (link to docs/README.md)

### Feature Grid (Homepage)

1. **Universal Capture**
   - Icon: Inbox/funnel
   - Text: "CLI, browser, mobile — capture from anywhere, process locally"

2. **Multimodal Pipelines**
   - Icon: Flow diagram
   - Text: "Text, URL, image, audio, video — unified processing"

3. **Knowledge Graph**
   - Icon: Connected nodes
   - Text: "Entities and mentions create a semantic identity layer"

4. **Decentralized Registries**
   - Icon: Network topology
   - Text: "Subscribe to knowledge feeds without platform lock-in"

5. **Full Sovereignty**
   - Icon: Lock/home
   - Text: "Export everything. Migrate anytime. No proprietary formats."

6. **Plugin Architecture**
   - Icon: Puzzle piece
   - Text: "Extend pipelines, storage, and commands without forking"

### Social Media Guidelines

**Profile Bio:**
"Local-first context engine. Transform content into structured knowledge. Built on dPKMS + ctxt. Open source (AGPL-3.0)."

**Post Voice:**
- Technical but accessible
- Share use cases, not just features
- Celebrate community contributions
- Ask questions, invite feedback

**Hashtags:**
- Primary: #contexthelp #dpkms #localfirst
- Secondary: #knowledgemanagement #pkm #semanticweb #openai

---

## Design System

### Component Library

**Buttons:**
- Primary: Bright Cyan background, white text, 8px border-radius
- Secondary: Transparent background, Bright Cyan border/text
- Tertiary: Text-only, underline on hover

**Cards:**
- Background: Warm White
- Border: 1px solid Slate Gray
- Shadow: 0 2px 8px rgba(26, 35, 50, 0.08)
- Border-radius: 12px

**Code Blocks:**
- Background: `#f6f8fa` (light mode) / `#0d1117` (dark mode)
- Border: 1px solid `#e1e4e8` (light) / `#30363d` (dark)
- Font: JetBrains Mono
- Syntax highlighting: GitHub theme

**Tables:**
- Header: Deep Navy background, white text
- Rows: Alternating Warm White / Light Gray
- Border: 1px solid Slate Gray

### Spacing Scale
```
xs:  4px   / 0.25rem
sm:  8px   / 0.5rem
md:  16px  / 1rem
lg:  24px  / 1.5rem
xl:  32px  / 2rem
2xl: 48px  / 3rem
3xl: 64px  / 4rem
```

### Breakpoints
```
Mobile:  < 768px
Tablet:  768px - 1024px
Desktop: > 1024px
Wide:    > 1440px
```

---

## Usage Examples

### Landing Page Hero (HTML/CSS)
```html
<header class="hero">
  <h1 class="hero-title">Your knowledge, always current, always yours</h1>
  <p class="hero-subtitle">A local-first context engine that transforms multimodal content into structured, queryable knowledge.</p>
  <div class="hero-cta">
    <a href="/docs/install" class="btn btn-primary">Get Started</a>
    <a href="/docs" class="btn btn-secondary">Read the Docs</a>
  </div>
</header>
```

### CLI Output Example
```bash
$ ctxt analyze --url https://example.com/article

✓ Enqueued job #142 (pipeline: url-standard)
⟳ Processing... (dpkms worker)
✓ Extracted 3 mentions: @research.ux-patterns, @tools.figma, @concepts.design-system
✓ Resolved 2 entities, created 1 new entity
✓ Saved knowledge object: obj_kn9x7m2p

View: ctxt open obj_kn9x7m2p
```

### Error Message Example
```bash
$ ctxt analyze --url https://private.site

✗ Pipeline failed: HTTP 403 Forbidden
  The URL requires authentication or is blocked.

  Try:
  • Check if you have access to this resource
  • Use --header "Authorization: Bearer TOKEN" if needed
  • Use `ctxt analyze --file` to process a local copy instead
```

---

## Implementation Checklist

- [ ] Design logo and icon system
- [ ] Create Figma/Sketch component library
- [ ] Build website with brand-compliant styles
- [ ] Update CLI output to match tone guidelines
- [ ] Create social media templates
- [ ] Write brand application guide for contributors
- [ ] Document brand usage rules for community

---

## Brand Evolution

This is a living document. As context.help grows, these guidelines will evolve based on:
- User feedback and community input
- Market positioning changes
- Technical architecture shifts
- New feature launches

**Version:** 1.0.0
**Last Updated:** 2026-01-27
**Maintainer:** ContextHelp Core Team
