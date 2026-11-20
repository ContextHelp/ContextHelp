---
name: gh-stars-classify
description: Stub star classifier for the repo-star e2e test
version: 1.0.0
author: ContextHelp
license: MIT
supported_languages: ["go"]
execution:
  type: external
  executable: classify
  minimum_isolation: process
  config_schema:
    type: object
    additionalProperties: false
    properties:
      confidence:
        type: number
        description: Confidence reported for a star candidate
  resource_requirements:
    max_memory: 1GB
    timeout: 10s
    max_output: 64MB
---

# gh-stars-classify (test stub)

Speaks the ADR-058 protocol with additive ops. Marks a repository that is
not already starred with `star-candidate`.
