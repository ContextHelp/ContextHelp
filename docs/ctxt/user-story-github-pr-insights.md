# User Story: GitHub PR Review Insights

## Context

**Primary Persona:** Marcus (The Privacy-First Software Architect)

**Why Marcus is the perfect fit:**
- ⭐⭐⭐⭐⭐ Tech savviness (lives in terminal)
- Works in a **FinTech bank** where **data leakage is fireable**
- Needs to understand proprietary **internal codebases**
- Uses ContextHelp locally on his M3 Max
- Ingests company git repos
- Already uses **Private Agent Profile** with `internal-docs` registry

**His pain point:** Marcus needs to learn from his team's collective wisdom embedded in PR reviews, but he can't use cloud-based AI tools. The GitHub PR Review Watcher plugin would let him capture team conventions, security patterns, and code quality insights **locally** without ever sending data to external services.

## Secondary Persona

**Aiden (Agent Developer)**
- Building specialized AI agents (Legal Bot, Coding Bot)
- Needs to scope visibility to specific contexts
- Could use PR insights to **train domain-specific agents** on team conventions

---

## User Stories

### **Team Knowledge & Collaboration**

72. As a Developer I want to automatically extract and surface code review insights from my team's PR reviews so I can learn team conventions without reading every review

73. As a Tech Lead I want to track recurring patterns in code reviews to identify knowledge gaps and training opportunities

74. As a Privacy-First Developer I want to capture team knowledge from GitHub PR reviews entirely locally without sending data to cloud services

75. As an Onboarding Developer I want to query established team conventions and best practices discovered from historical code reviews

76. As a Manager I want to identify subject matter experts by analyzing review patterns and insight quality over time

---

## Plugin Implementation

The GitHub PR Review Watcher plugin perfectly serves Marcus's world:
- **Local-first**: No data leaves his machine
- **Privacy-preserving**: Complies with FinTech security requirements
- **Extracts valuable team knowledge**: Captures conventions, patterns, and expert opinions
- **Proprietary codebase friendly**: Helps understand the bank's internal patterns

See [plugins-github-pr-review-watcher.md](../plugins/plugins-github-pr-review-watcher.md) for full plugin specification.
