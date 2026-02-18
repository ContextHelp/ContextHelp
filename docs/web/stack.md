# Web Stack (context.help)

This document describes the intended implementation stack for the public website.

## Goals

- Fast marketing site (SEO, performance)
- Easy iteration on copy and pages
- Clear separation from node runtime and cloud service internals

## Recommended stack

- Framework: Next.js (App Router)
- Styling: Tailwind CSS
- Content: MDX files in-repo (or a headless CMS later)
- Analytics: privacy-respecting analytics (pluggable)
- Deployment: Vercel or equivalent static/edge hosting

## Page types

- Homepage
- Publishers (how to publish a registry)
- Buyers (why subscribe)
- Marketplace index + registry detail pages
- Docs link-out (to `docs/`)

## Data sources (later)

- Marketplace listings API (cloud)
- Registry metadata (public capability docs)
