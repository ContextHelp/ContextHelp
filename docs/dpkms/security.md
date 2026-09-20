# Security

This document defines the security model, responsibilities, and implementation guidelines for **ContextHelp**.
The system is designed to be **local-first**, **decentralized**, and **registry-agnostic**, which requires clear boundaries around trust, data flow, and execution.

## Threat Model

ContextHelp must protect users from:

- Unauthorized access to local knowledge
- Malicious or misconfigured registries
- Compromised plugins or pipelines
- Supply-chain attacks via registry data or integrations
- Unsafe remote execution or data exfiltration
- Denial-of-service conditions within the engine
- Privilege escalation through agents, APIs, or CLI tooling

The engine assumes:

- The local machine is not fully trusted
- Registries and external providers may be untrusted
- Plugins may come from unverified third parties
- Pipelines may consume unstructured or adversarial inputs
- Agents operate within a bounded context defined by configuration

## Local-First Security Guarantees

ContextHelp follows these principles:

- No external communication occurs unless the user explicitly configures registries or integrations.
- All enrichment and processing occur locally by default.
- Bookmarks, pipelines, and cache data remain private unless exported intentionally.
- No automatic telemetry, analytics, or background transmissions are permitted.

## Process Isolation & Execution Boundaries

Pipeline execution must respect clear isolation:

- Pipelines cannot write outside their assigned working directory.
- External calls (LLM providers, metadata fetchers) must be opt-in and explicitly configured.
- Plugins run in a restricted context with:
  - No access to OS-level exec without permission
  - Limited filesystem access
  - Strict sanitization of inputs and outputs

Agents consuming ContextHelp data must operate within defined scopes and may not directly mutate stored data unless explicitly allowed.

## Registry Trust Model

Registries are external providers and may be:

- Public
- Private
- Authenticated
- Paid
- Rate-limited
- Untrusted

ContextHelp must:

- Treat all registry data as **untrusted input**.
- Validate registry responses before merging or caching.
- Apply schema validation for taxonomy, bookmark, and weight data.
- Maintain a clear separation between:
  - **Local data** (trusted)
  - **Registry data** (unverified)
- Support cryptographic signatures or version pinning in the future.

The registry — not the object — is the unit of trust; visibility tiers are deployed as separate registries (see [tiered-deployment.md](tiered-deployment.md)).

If a registry behaves incorrectly (malformed JSON, incorrect payload, protocol violation), ContextHelp must:

- Reject the data
- Log the error
- Continue operating without degradation

## API Security

### REST API
The server must:

- Bind to localhost by default
- Support TLS termination when exposed externally
- Require explicit configuration for public access
- Provide authentication options (token, session, mTLS)

Endpoints must validate:

- Input size limits
- Rate limits
- Payload schemas
- Agent profile permissions

### gRPC API
The gRPC interface must:

- Validate message schemas
- Reject oversized requests
- Implement optional auth interceptors
- Respect the same agent-scoping rules as REST

## CLI Security

The `ch` CLI must:

- Never send data externally unless explicitly required by the command
- Warn when using external registries or integrations for the first time
- Store configuration securely with correct file permissions
- Prevent unsafe shell execution in flags or arguments

## Data Security & Storage

Local storage must:

- Validate bookmark schema on read and write
- Protect against corrupted or maliciously modified JSON files
- Handle concurrency conflicts safely
- Support optional encrypted storage in future versions

Bookmark entries must never include:

- Arbitrary executable payloads
- Unsanitized remote registry content
- Unbounded raw binary data without size checks

## Plugin & Pipeline Security

Plugins and pipelines must adhere to:

- Permission-based capability exposure
- Strict type validation for inputs and outputs
- No direct external network access unless granted explicitly
- Timeouts, memory limits, and cancellation controls

A misbehaving plugin must not:

- Crash the engine
- Access unrelated data
- Escape its execution context

## Registry Authentication

Registries may require:

- API keys
- OAuth2
- JWT
- Signed URLs
- mTLS
- Paid subscription tokens

ContextHelp must:

- Store credentials locally in user-controlled config files
- Never log credentials
- Support environment-variable-based secrets
- Apply backoff + retry logic but avoid leaking sensitive data

## Denial-of-Service Protection

To protect the local engine:

- Enforce input size limits (text, binary, base64)
- Throttle expensive pipelines
- Cap concurrency for background jobs
- Apply quotas per registry (requests/minute)
- Cache registry responses with ETag / Last-Modified headers

## Secure Defaults

ContextHelp must ship with:

- No external registries enabled by default
- No plugin auto-installation
- Localhost-only API binding
- Minimal privileges for all components
- Clear warnings when enabling remote features

## Future Enhancements

Potential improvements to be considered:

- Sandboxed WASM plugin runtime
- Optional encrypted bookmark database
- Signed registry metadata with trust scores
- Distributed trust networks for registry provenance
- Formal verification of critical schemas
- Per-agent capability restrictions (RBAC for agents)

## Summary

Security in ContextHelp is centered on:

- Local-first privacy
- Treating registries as untrusted providers
- Strong isolation around pipelines and plugins
- Explicit configuration for anything remote
- Schema validation at every boundary
- Principle of least privilege across components

This ensures ContextHelp remains safe, predictable, and trustworthy as a decentralized knowledge engine.
