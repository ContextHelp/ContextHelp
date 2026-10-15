---
name: pii-mask
description: Masks email addresses and phone numbers in captured text
version: 1.0.0
author: ContextHelp
license: MIT
supported_languages: ["go"]
execution:
  type: external
  executable: pii-mask
  minimum_isolation: process
  config_schema:
    type: object
    additionalProperties: false
    properties:
      email_mask:
        type: string
        description: Text that replaces each email address
      phone_mask:
        type: string
        description: Text that replaces each phone number
  resource_requirements:
    max_memory: 256MB
    timeout: 10s
    max_output: 64MB
---

# pii-mask

Build the executable into this directory before installing it:

    go build -o examples/steps/pii-mask/pii-mask ./examples/steps/pii-mask
    dpkms pipeline step install pii-mask --path examples/steps/pii-mask

See docs/manual/admin-extensibility/custom-steps.md.
