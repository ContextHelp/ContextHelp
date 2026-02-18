# ctxt Personas

**Version:** 0.1.0

This directory contains persona documentation for **`ctxt`** — the agentic brain that provides meaningful behaviors and intelligent context management for end users.

---

## Overview

Unlike [dPKMS personas](../../dpkms/personas/) which focus on technical roles (developers, operators, integrators), **ctxt personas** represent the actual end users who interact with the system through various interfaces (CLI, TUI, Web, Mobile, Browser Extension).

These personas help us understand:
- **Who** uses ctxt in their daily workflow
- **Why** they need context management
- **How** they interact with the system
- **What** problems they're solving

---

## Persona Categories

### End Users (Real People, Real Problems)
**Document:** [personas-end-users.md](./personas-end-users.md)

These are the people who use ctxt to solve real-world knowledge management problems:

1. **The Overwhelmed PhD Candidate** - Academic researcher with thousands of PDFs
2. **The Privacy-First Software Architect** - FinTech engineer needing local AI
3. **The Investigative Journalist** - Analyzing leaked document dumps offline
4. **The Sci-Fi Novelist** - Managing complex fictional universe lore
5. **The Corporate Legal Associate** - Discovery and legal research
6. **The Bio-Hacker / Self-Quantifier** - Health data and research synthesis
7. **The Investment Analyst** - Financial research and sentiment analysis
8. **The Indie Game Developer** - Asset management and code recall
9. **The Chief of Staff** - Organizational knowledge hub
10. **The Digital Rights Activist** - Censorship-resistant knowledge distribution

**Key Characteristics:**
- Varied technical sophistication (⭐ to ⭐⭐⭐⭐⭐)
- Privacy-conscious or legally required to be
- Domain experts in their field, not necessarily in technology
- Need intelligence without cloud dependencies
- Value sovereignty over their data

---

### User Roles (Technical Interaction Patterns)
**Document:** [personas-user-roles.md](./personas-user-roles.md)

These personas represent the technical ways people interact with ctxt:

2. **Developer / High-Performance Developer** (Devin) - gRPC integration for IDE
3. **Support Engineer** (Sarah) - Client deployment and troubleshooting
4. **Operator** (Oliver) - Service monitoring and health checks
5. **System Administrator** (Alex) - Configuration management at scale
6. **Security Engineer** (Sam) - Secret management and compliance
7. **Performance Engineer** (Pat) - Pipeline tuning and optimization
8. **Researcher** (Dr. Riya) - Offline local file ingestion
9. **Podcaster** (Paul) - Audio transcription and search
10. **Data Scientist** (Dana) - ML model and pipeline customization
11. **Debugger** (Dave) - Raw mode troubleshooting
12. **Operations Engineer** (Ophelia) - Storage backend migration
13. **Registry Maintainer** (Rex) - Taxonomy publishing and governance
14. **Agent Developer** (Aiden) - Scoped AI agent profiles
15. **Security Architect** (Arjun) - Zero-trust policy enforcement
16. **System Integrator** (Ian) - REST API web dashboard integration
17. **Scripter** (Scott) - CLI automation and piping
18. **API Consumer** (Alice) - Mobile app integration
19. **Plugin Developer** (Paige) - Extension development

**Key Characteristics:**
- Technical competence varies from power user to expert
- Need specific interfaces (CLI, API, gRPC, SDK)
- Focus on integration, automation, or customization
- Often building on top of ctxt for others
- Care about performance, security, extensibility

---

## Persona Matrix

| Persona Type | Primary Need | Main Interface | Privacy Level | Tech Savvy |
|--------------|--------------|----------------|---------------|------------|
| **End Users** | Solve domain problems | CLI/TUI/Web | High | ⭐-⭐⭐⭐⭐ |
| **User Roles** | Technical integration | API/gRPC/CLI | Varies | ⭐⭐⭐-⭐⭐⭐⭐⭐ |
| **[dPKMS Personas](../../dpkms/personas/)** | Build/extend platform | Go SDK/Plugins | N/A | ⭐⭐⭐⭐⭐ |

---

## How to Use These Personas

### For Product Development
- **Feature prioritization**: Which personas are underserved?
- **Interface design**: What does Elena (PhD) need vs Marcus (Architect)?
- **Documentation**: Write for Priya (⭐ tech) differently than Scott (⭐⭐⭐⭐⭐)

### For Design Decisions
- **Privacy-first by default**: Multiple personas require strict privacy
- **Local-first architecture**: Journalist Sarah, Activist Elias need offline
- **Flexibility**: Support both GUI (Priya) and CLI (Marcus, Scott)
- **Sovereignty**: Everyone needs control over their data

### For Testing
- **User acceptance testing**: Recruit people matching these profiles
- **Scenario testing**: Use persona stories as test scenarios
- **Accessibility testing**: Different tech savvy levels require different UX

### For Documentation
- **Getting Started**: Write for Elena and Priya (lower tech savvy)
- **API Docs**: Write for Devin, Ian, Alice (technical integrators)
- **Operations Guide**: Write for Oliver, Alex, Ophelia (ops roles)
- **Security Guide**: Write for Sam, Arjun (security roles)

---

## See Also

**Related Documentation:**
- [../non-negotiables.md](../non-negotiables.md) - Design principles driven by persona needs
- [../user-stories.md](../user-stories.md) - Concrete user stories derived from personas
- [../interface-mappings.md](../interface-mappings.md) - How each interface serves persona needs
- [../../dpkms/personas/](../../dpkms/personas/) - Technical personas for platform builders

**Interface Documentation:**
- [../tui.md](../tui.md) - Terminal interface for power users
- [../webui.md](../webui.md) - Web interface for accessibility
- [../api-cli.md](../api-cli.md) - CLI for automation and scripting

**System Architecture:**
- [../../architecture.md](../../architecture.md) - How ctxt serves these personas
- [../featureset.md](../featureset.md) - Feature matrix across personas
