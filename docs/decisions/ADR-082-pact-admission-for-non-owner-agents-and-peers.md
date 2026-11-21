# ADR-082 – PACT Admission for Non-Owner Agents and Federation Peers

> **Status:** Accepted (2026-10-16). Adopting PACT before agent sharing and federation proceed is an owner decision of 2026-10-15; the sections that record it say "owner-decided". The remaining sections were accepted on 2026-10-16 as written, including the four defaults confirmed that day: per-account overrides are set by `admin:principals` holders, instance `-2` also stops existing person agents, lowering a limit never revokes, and the limit covers person principals only (default `-1`). The open questions at the end stay open.
>
> **Implementation is gated on the publication of `hop.top/pact-go`** (tag `pact-go/v1.0.0-alpha.1` and the `hop-top/pact-go` mirror). None of the code this ADR describes is in the tree yet: no `internal/pactgw`, no `internal/pactholder`, no `ctxt agent holder`, no `pact:` config, no agent or approval tables. Code that imports the library merges only after the tag resolves through the module proxy (§9).
>
> Two decisions bind that implementation. **`pact.default` is `off` when unset**: an instance runs no PACT evaluation until the operator sets a mode, and a route class is `observe` or `enforce` only when configured so. **The person-key hold (§11) applies only when PACT enforcement is on** for the route class a key would use; with enforcement off, API keys on `person` principals work as ADR-079 describes.
> **Date:** 2026-10-16
> **Author:** jadb
> **Applies to:** dPKMS, ctxt
> **Amends:** ADR-079 (§1 "Agents are keys, not principals", plan decision D16; §2 enterprise OIDC; §5 admin bundle), ADR-080 (§1 vocabulary; §6 federation row; §9 read audit; audience membership sources)
> **Depends on:** ADR-079 (principals, identities, API keys), ADR-080 (share policies, access log)
> **Amended:** 2026-10-20 — design notes from the library work: the parts §4 and §9 found missing now exist in the library; the holder key is a 0600 file, not the OS keychain; the gateway keys are in memory in the first cut; there is no `pact.keys` yet. Revised on port: `pact.default` is `off`, the person-key hold applies only under enforcement, and the library's published tag gates all implementation (see [Amendment 2026-10-20](#amendment-2026-10-20-design-notes-and-implementation-status))
> **Amended:** 2026-10-20 — library module renamed to `hop.top/pact-go` (upstream reorganization); the Decision, §9 and Implementation Notes name the new module, the 2026-10-16 Context keeps `hop.top/pact`
> **Amended by:** ADR-083 (steward access): §10 "No data-admin tier" is designed there, with the registry rules (credential delivery, pending widening changes) that keep administering from becoming data access; open question 7 is answered by it
> **References:** ADR-064 (federation), ADR-068 (MCP read surface), ADR-074 (same-owner device sync), ADR-077 (ctxt is a pure API client), ADR-078 (no plugin system)
> **Protocol:** PACT, Purpose-bound Access Control & Trust, draft spec 0.1.0, wire 0.1. "Draft §N" below means that section. Section numbers follow the source repository's current draft; the read-only `hop-top/spec-pact` mirror lags it.

---

## Context

### The gap in ADR-079 and ADR-080

ADR-079 §1 says an agent is an API key on the principal it acts for, bound to a share policy that narrows that principal's view. That is enough while the principal is the owner. It is not enough once a non-owner principal (a colleague or a partner) points an agent at shared data:

- **A key cannot tell an agent from a person's CLI.** A bearer secret proves only that someone holds it. dpkms cannot tell a scripted agent from a person pasting the key into a terminal, so it cannot apply different rules to either.
- **An agent has no identity of its own.** Rotating the key makes a new "agent" in the access log. Nothing records which software acted, who operates it, or that the replacement is the same agent.
- **Nothing binds a request to a purpose or to its exact content.** A stolen or replayed key works for any route its scopes allow, with any parameters, for as long as it lives. Nothing says why the request was made.
- **Federation peers authenticate the same way.** A `peer` principal's API key (ADR-079 §1) authorises whole-corpus pushes (ADR-064) on a bearer secret alone.

Sharing (ADR-080: audiences, share policies, the scoped driver, the render layer, the backstop, the read audit) is built for people in browsers. It stays correct for them. The open question is how agents of non-owner principals, and peers, are admitted. Until it is answered, API keys on `person` principals are refused where PACT enforcement is on (§11).

### What PACT is

PACT defines how an agent presents a profile, a declared intent and evidence to an existing access-controlled gateway. The gateway publishes a signed, per-operation requirement contract. The agent answers with a signed presentation bound to its enrolled key, to one actual request and to a single-use nonce. A validator returns `ADMISSIBLE`, `INADMISSIBLE` or `DEFERRED` and a signed receipt. The environment keeps authentication, roles, resource permissions and final authorization: "A successful PACT admission result never independently grants resource access" (draft §1). Draft §2.3:

`execute = native_authentication_valid AND pact_admission_admissible AND native_authorization_allows AND native_execution_conditions_satisfied`

The draft says of itself that it "does not claim that an implementation, interoperability suite, or independent security assessment has been completed". That shapes everything below.

### What exists today, read at poly-pact `main` 0d9fe3a (2026-10-16)

> *Amended 2026-10-20 (below):* the validator, requirement-contract issuance, gateway middleware and holder/presentation construction this table lists as not present now exist in the library. ctxt does not use them yet; that is future work gated on the library's published tag. The table stays as read on 2026-10-16.

The protocol's source of truth is the repository `poly-pact` (spec, Go reference implementation, language SDKs). `hop-top/spec-pact` is a read-only mirror of its `spec/` tree and holds one commit, older than the source draft: it lacks draft §13.3.1 (transport profiles), §17.5 (actor binding and execution identity) and the conformance-profile text of §22.1. The Go module path is `hop.top/pact` (`go/go.mod`), not `hop.top/spec-pact`. *Amended 2026-10-20:* the Go library is now the module `hop.top/pact-go` (`go/go.mod`, tags `pact-go/vX`, mirror `hop-top/pact-go`); `hop.top/pact` is the CLI module and ctxt does not import it.

| Piece | State |
|---|---|
| Draft, schema, 20 informative examples, integration profile documents (`basic`, `oauth`, `saml`, `mtls`, `files`, `abac`, `rebac`, `ldap`, `aws`, `k8s`, `db`; `mcp` work in progress), identity-mapping, request-binding and freshness profiles, language-neutral fixtures | Draft |
| Go `core/{strictjson,jcs,digest,jws,clock,binding}` and wire types, limits, enums | Exist |
| Go `adapter` (draft §17.1 contract), `adapter/adaptertest`, `adapter/enroll` (+ `memory`), `adapter/freshness`, `adapters/memory` | Exist |
| `pactc` conformance runner (library and CLI), `pactconf`, `pactfix` | Exist. `pactc` compares only the decision (or Problem code) of an evaluate receipt; signature, claims and check results are not compared yet; outcomes, learning and schema entries are skipped |
| Validator (draft §11.3), requirement-contract issuance, holder/presentation construction, receipt issuance packaging | **Not present, and not in the module's package plan** |
| Reputation service, learning controller | **Not present** |
| `adapters/{oauth,mtls,saml,...}` (including an Entra or Graph adapter), `adapter/task` | Planned. No MCP adapter is planned beyond its profile document |
| Conformance profile `single-owner` (draft §22.1: omit Reputation and Learning) | Registered in `registry.json` as draft; the profile document does not exist yet |
| Published tag, module-proxy resolution, GitHub mirror `hop-top/pact` named by the module README | **None.** `hop.top/pact` does not resolve through the proxy, and the mirror was not found with the available GitHub credentials on 2026-10-16 |

ctxt-side facts this ADR builds on, all re-read in the tree at `next` `f62e9d7b` (paths below exist there): principals of kind `owner|person|device|peer` and `principal_identities` rows (`internal/storage/auth.go`); federation push gated to peer principals (`handlers_federation.go`, `PEER_REQUIRED`); the route table that classifies every `/api/v1` route as owner-only, capability-gated or open, with its response shape (`internal/server/http/capabilities.go`); the compiled `sharing.Viewer` and its policy key; the buffered `access_log` writer (`internal/sharing/audit.go`, `storage.AccessEntry`); and the instance signing key, an Ed25519 pair in the OS keychain used for signed config bundles and sync (`dpkms key`, `internal/bundle`).

### Constraints

- **Local-first, swappable.** PACT must not add a mandatory outside service. The validator and publisher run inside dpkms.
- **ctxt is unreleased.** No compatibility shims; existing keys are migrated, not grandfathered.
- **kit is the convention authority; grep kit first.** kit has no PACT code. Its `transport/authn` stays the native authentication path (ADR-079 §6). PACT's strict JWS, JCS and digest rules are a different, narrower profile and are consumed from the PACT module, not rebuilt in kit or ctxt.
- **No vendoring.** Sibling hop-top modules are consumed by tag in CI and by workspace replace locally (`CI_DROP_REPLACES` in the Makefile). `hop.top/pact-go` is the exception: ctxt commits no local `replace` and no `go.work` `use` for it, and consumes it only by published tag.

---

## Decision

**dpkms adds PACT admission as an extra AND condition for requests made by the agents of non-owner principals and by federation peers, as the draft's in-gateway form (draft §4.4). Native authorization stays exactly what ADR-079 and ADR-080 define: scopes, the share-policy viewer, capabilities, the response backstop. PACT can refuse a request those layers would allow. It never grants anything, never widens a viewer and never replaces a native denial. An agent becomes a stable, enrolled identity under the principal it acts for, separate from the bearer key and from the PACT holder key. Rollout is observation first, enforcement per route class. The protocol logic comes from the `hop.top/pact-go` Go module; none of it is written in ctxt.**

### 1. Where PACT applies (owner-decided)

| Surface | PACT? | Why |
|---|---|---|
| Agents acting for a non-owner `person` | **Yes** | The case in the Context. Replaces API keys on person principals (§3, §11). |
| Federation peers pushing to this instance (ADR-064, `peer` principal) | **Yes** | The pusher is a holder; its key is a bearer secret over a whole corpus today (§9). |
| Later: Cloud and marketplace callers | **Yes, later** | Not designed here. |
| Owner's own requests (owner keys, including keys narrowed by a share policy) | No by default | The owner is the trust root and bypasses share policies (ADR-080 §2). |
| Agents of the owner | **Optional, per agent** | An owner agent stays a key unless the owner enrolls it, for example on an untrusted remote host (§3). Enrolled, it is admitted like any other enrolled agent. |
| Device keys (capture, same-owner sync, ADR-074) | No | Owner-trusted devices. |
| Human browser sessions: invite, magic link, passkey, trusted proxy, UI sessions | No | PACT admits agents, not people. These keep the browser rules of ADR-079 §3. |

Options considered. **All principals, owner included:** adds a signature round trip to every local request and protects nothing the owner does not already control. **Every request, humans too:** a person at a browser holds no holder key and PACT has no human flow. **Non-owner agents and peers (chosen).** An owner agent that runs on an untrusted remote host opts in by being enrolled (§3); owner-decided 2026-10-16.

### 2. Composition with native authorization

For a route class where PACT is enforced:

`execute = authenticated principal (ADR-079 chain) AND PACT admissible AND route scope held AND (capability open AND viewer predicate and render, ADR-080) AND backstop passes`

- PACT is evaluated **before** any viewer-scoped read. A PACT failure reveals nothing about rows; it answers with the draft's transport errors (`INVALID_PROOF`, `CONTEXT_MISMATCH`, `DEPENDENCY_UNAVAILABLE`, `SECURITY_HOLD`, draft §13.4), which never disclose membership or the existence of an object. "Hidden means absent" (ADR-080 §4) is untouched: a row outside the viewer is still a 404 after admission.
- PACT never grants. A pass leaves scopes, audiences, grants, `deny_classes` and the key's share policy exactly as they were. The viewer is compiled as before; PACT adds no input to it.
- The unknown, unavailable or expired case is never a pass. A required source, the replay store, the hold store or the receipt store being unavailable yields `DEFERRED` (draft §18).

### 3. Agent identity and the amendment of ADR-079 D16 (owner-decided in outline)

**Options.** (A) Keep "agents are keys": rotating a key mints a new agent, so PACT's stable `agent_id` and its rule that rotation must not multiply feedback or inherit trust (draft §6.4, §22.2) cannot hold. (B) A separate `agent` principal kind with a parent link: still rejected for the reason ADR-079 gave, that it leaves open whether grants flow through the parent, and it would let an agent carry state a principal does not. (C) **An agent is an enrolled identity of one principal, not a principal (chosen).**

**The amendment.** ADR-079 §1 reads "Agents are keys, not principals" and the plan's D16 reads "Keys". They are amended as follows:

> An AI agent or script is never a principal. It holds no grants, joins no audience and has no scopes of its own. It acts for exactly one principal, and can never hold more scopes or see more data than that principal, narrowed further by the share policy bound to its credential (ADR-080 §2).
>
> - **Agents of the owner and of devices** stay as ADR-079 describes by default: an API key on that principal, optionally bound to a narrowing share policy. Revoking the key ends the agent; the access log attributes reads by key. The owner may enroll any of its own agents (opt-in per agent, owner-decided 2026-10-16), for example one running on an untrusted host; an enrolled owner agent follows every enrollment rule below, and its owner key is then valid only as that agent's credential on enforced route classes. Device keys are not enrollable.
> - **Agents of a non-owner principal (a `person` or a `peer`) are enrolled.** Enrollment creates a stable `agent_id` under that principal, binds it to one native credential (its API key identity) and to one PACT holder key, and records the operator who answers for it. The API key is the native credential, the holder key proves each request, and the `agent_id` is the identity that outlives both. Where PACT enforcement is on, an API key on a non-owner person principal is valid only as the credential of an enrolled agent.

**Rules.**

- `agent_id` is an absolute URI assigned by dpkms under its `public_url`, never reused (draft §6.1 permits a gateway-assigned URI). It is the same identifier in the access log, in receipts and in contracts.
- **Three separate things.** The bearer API key (ADR-079 identity, authenticates the workload), the PACT holder key (ES256 key pair held by the agent, thumbprint `holder_jkt`, draft §6.2) and the `agent_id`. A bearer key never doubles as a holder key, and holding the bearer key does not enroll anything (identity mapping IM-13, IM-21).
- **One credential per agent.** Each enrolled agent has its own API key identity; a key identity is enrolled for at most one agent. The draft allows several agents to share one credential if each is enrolled separately and selected by the `agent_id` the presentation names (IM-16 to IM-18); dpkms does not use that form, because a dedicated credential is the preferred form (IM-19) and revocation stays one row.
- **Rotation.** Keys rotate under the `agent_id`. Both the bearer key and the holder key can be replaced; the `agent_id`, the principal and the audit history stay. A holder thumbprint, once enrolled, is never enrolled again for any agent (IM-9). Rotation is an administrative act: the old holder key proves nothing about authority to rotate, and neither does the agent's bearer key (draft §6.4, IM-25).
- **Who may enroll, rotate or revoke.** Only an authenticated human path of the principal, or `admin:principals`, never a request authenticated by the agent's own credentials. The exact scope and the approval model are open (see Open questions); this ADR fixes the negative rule. Approval is configurable (below).
- **Enrollment limits (owner-decided 2026-10-16).** How many agents a `person` principal may enroll is an instance setting with a per-account override.
  - *Instance setting*, `-2` forbidden, `-1` none, `0` unlimited, `N>0` at most N. `-2` is a hard stop: no enrollment under any `person`, per-account overrides are ignored, and existing enrolled agents of person principals are refused on enforced classes like held keys (rows kept). `-1` allows none unless an account override says otherwise.
  - *Per-account override*, `-1` disable for this account, `0` unlimited, `N>0` at most N; unset follows the instance setting. It replaces the instance value for that account in either direction (an account may be allowed more than the instance default), except under `-2`.
  - *Who sets it.* Holders of `admin:principals`, never the account itself or an agent's credentials.
  - *Semantics.* The count is active enrolled agents; revoked and compromised agents do not count. Enrolling past the limit is refused with a stable error code. Lowering a limit, or switching an account to `-1`, never revokes existing agents; it stops new enrollments. Owner enrollments (above) and peer enrollments (§12) are outside this limit.
  - *Default (proposed).* `-1`, so agent access to shared data is a deliberate owner act, which matches the person-key hold under enforcement (§11).
- **Enrollment approval (owner-decided 2026-10-16).** Whether an enrollment inside the limit needs approval is configurable; the default is that it does.
  - *Instance setting*: `required` (default) or `none`. Per-account override (same authority as the limit override): `required`, `none`, or unset to follow the instance.
  - *`required`*: a request creates a `pending` enrollment that is admitted nowhere. The owner, or a holder of `admin:principals` other than the requester, approves or rejects it; the decision, the approver and the time are recorded. A pending request counts against the account's limit, so a flood cannot be queued past it, and it expires after a TTL (value open, see Numbers).
  - *`none`*: the enrollment is active at once, within the limit.
  - Owner enrollments need no approval; the owner is the approver. Approval never widens what the agent may see; the agent's view stays the principal's view narrowed by its key policy.
- **Compromise is terminal.** An authority marks a holder key compromised; the `agent_id` is never recovered. A replacement is a new `agent_id` (IM-28). The mark triggers a security hold (draft §6.4, §15.4).
- **Authority.** An enrolled agent's effective view is the principal's view, narrowed by its key's share policy if any. It inherits suspension and revocation of the principal in one request: revoking a principal ends every agent and every key under it.
- **Realm.** The PACT realm is one dpkms instance: `tenant_id` identifies the instance (or, on a business-owned instance, the organisation), `environment_id` its gateway environment. A cryptographic identity reused on another instance transfers nothing (draft §21).
- **Peers.** A peer's pushing instance is enrolled the same way under its `peer` principal; the key pair and `agent_id` the pusher presents live in the pushing instance's own secret store (§12).

Schema sketch (instance DB, SQLite and Postgres in lockstep; not binding):

```sql
CREATE TABLE agents (
  id            TEXT PRIMARY KEY,                      -- agent_id URI
  principal_id  TEXT NOT NULL REFERENCES principals(id),
  identity_id   TEXT NOT NULL UNIQUE REFERENCES principal_identities(id), -- native credential, 1:1
  operator      TEXT NOT NULL,
  status        TEXT NOT NULL,                         -- active | revoked | compromised
  enrolled_by   TEXT NOT NULL,                         -- actor, attributable
  created_at    TIMESTAMP NOT NULL,
  revoked_at    TIMESTAMP
);
CREATE TABLE agent_keys (                              -- holder keys; history is kept
  jkt           TEXT PRIMARY KEY,                      -- RFC 7638 thumbprint, never re-enrolled
  agent_id      TEXT NOT NULL REFERENCES agents(id),
  public_jwk    TEXT NOT NULL,
  status        TEXT NOT NULL,                         -- active | retired | revoked | compromised
  created_at    TIMESTAMP NOT NULL,
  retired_at    TIMESTAMP
);
```

### 4. Roles and the honest partial claim

PACT's roles are logical and may be colocated, but "role separation and authority restrictions still apply when components share a process" (draft §4.1).

| PACT role | In dpkms | Status |
|---|---|---|
| Gateway | dpkms HTTP request path (REST `/api/v1`) | This ADR |
| Validator | In-gateway form, same process (draft §4.4); signs with its own key (§8) | Needs the library validator, which did not exist yet. *Amended 2026-10-20:* it exists in the library; the dpkms side (planned `internal/pactgw`) is future work |
| Requirement publisher | dpkms, own key, publishes contracts derived per §5 | Needs library contract issuance, which did not exist yet. *Amended 2026-10-20:* it exists in the library; the dpkms side (planned `internal/pactgw`) is future work |
| Holder | ctxt (ADR-077 API client) for agents it runs, other agent software through the same wire rules, and federation peers' pushers | Needs library holder support, which did not exist yet. *Amended 2026-10-20:* it exists in the library; the ctxt side (planned `internal/pactholder`, `ctxt agent holder`) and peers' pushers are future work |
| Reputation | **Not operated.** No snapshots, no reputation field rules, no scoring. Admission and native-authorization outcomes are recorded as receipts and audit entries; execution visibility is declared absent rather than assumed successful (draft §14.1) | Excluded by the single-owner profile |
| Learning | **Not operated.** No adaptive requirement sets, no controllers, no models. Contracts always carry the base (conservative) set | Excluded by the single-owner profile |

What dpkms may say, and when:

- Until the `single-owner` conformance profile document exists and dpkms passes its rows, dpkms describes itself as a **partial role implementation** (draft §22.1: an omission without a declared profile is partial). It never says "full PACT".
- After, it declares `single-owner` in gateway metadata, in contracts and in receipts (draft §21), and runs the §22.2 rows that profile keeps.
- Observation mode is a mode, not a conformance claim. Records made in it say so, and dpkms does not describe it as protection (draft §2.3, §22.1).
- A route class is called enforced only when the conditions in §7 hold for it. An opaque class that cannot expose a usable request mapping stays out of the claim (draft §4.4).

### 5. Mapping to ADR-079 and ADR-080

| ADR concept | PACT concept | Rule |
|---|---|---|
| Principal (`prn_<ULID>`, `owner`) | `OperationContext.principal_id` | Namespaced per identity mapping IM-1 to IM-5. Local principals use an `x-` namespace (proposed `x-dpkms:{instance}:{principal}`, valid under the grammar, to be fixed by the dpkms adapter profile). IdP principals use the adapter's namespace (§10). |
| Route scope (`verb:resource`) and route table entry | `action`, mandatory request mapping | The adapter derives action and resources from the authenticated request and the route table, never from client-supplied fields (draft §10.2). One mapping version for issuance and validation. |
| Share policy (name, version) | `purpose_id` | One purpose per share policy and version, `…/purposes/{policy}/v{version}`. Editing a policy bumps its version, which already invalidates compiled viewers (ADR-080 §2); it also invalidates outstanding contracts (draft §9.4). |
| Audience and grant | Which purposes an agent may declare | The purposes available to an agent are the policies granted to its principal's audiences, intersected with the policy bound to its key. A purpose the principal has no grant for is `UNSUPPORTED_PURPOSE` (draft §10.1). |
| Policy `capabilities` (search modes, entities, export, MCP, events) | Allowed action classes per purpose | A purpose justifies only the operations its policy's capabilities and the route's capability gate open. |
| Scope + policy + route class | Requirement contract | Derived, signed by dpkms as publisher. Base fields: native `principal_id` and audience context, enrolled agent and operator binding, `delegation` reference naming the grant that applies. Only `native` and `evidence` sources are used for authority claims; `self` is never used for identity, authority, tenant, role or delegation (draft §9.2). One effective set, no adaptive variants (§4). |
| Request-bound proof | `Presentation` signed by the holder key | The operation digest covers method, target URI, body, consequential headers (draft §10.2). |
| `access_log` entry | Receipt-shaped record, joined to a signed receipt | §8. |

Honest limits of this mapping:

- **The purpose/operation relation is an installed check.** Draft §10.1: if the environment cannot evaluate whether an operation is consistent with the declared purpose, it must not claim PACT enforces that relation. Until dpkms installs that check for a route class, the class runs in observation. The check's identifier and exact semantics are open.
- **Query routes have no enumerable resource list.** For `GET /objects/{id}` the resource is the object. For list and search routes the objects released depend on the viewer; the resource granularity is open.
- **gRPC and MCP have no declared mapping.** The baseline mapping is HTTP. The MCP adapter profile is work in progress and "must not be cited as support for any claim". Until a mapping exists for a transport, non-owner agents do not reach it in enforcement (their keys are refused there), rather than staying in observation indefinitely.

### 6. What PACT does not change

Native authorization is what it was: the chain and the principal (ADR-079 §3), scopes (§5), the scoped driver, the render layer, derived-data inheritance, the capability gates, the 404-for-hidden rule and the response backstop (ADR-080 §4 to §8). The owner is still unrestricted. ADR-080's tests, including the leak suite, stay the proof that a viewer sees only what policy allows; PACT admission never substitutes for them. PACT adds a refusal path and a signed record, and adds no way to see more.

### 7. Rollout (owner-decided)

1. **Observation per route class.** dpkms evaluates presentations, writes receipts and audit records, and enforces nothing. Records are labelled `observation`. Native access is not conditioned on PACT (draft §2.3). A request without a presentation is recorded as missing, not refused.
2. **Enforcement per route class**, when all of these hold for that class: an adapter mapping and request-binding for its routes; enrolled agents for every non-owner agent key that uses it; a receipt store that is durable before a positive result (otherwise no positive enforced admission, draft §12.2); the atomic replay store; the purpose/operation check installed (§5); the validator and issuance code consumed from the PACT module (§9) and passing the applicable draft §22.2 rows through `pactc`. Operators switch classes one at a time; the mode of each class is visible in discovery and in every receipt.
3. **Person keys (the hold, §11).** The hold exists only under enforcement. When a class is enforced, a non-owner person key without an enrolled agent is refused for it (§11). Migration enrolls the existing keys (their rows are kept) or leaves them refused for that class; a key never keeps working outside admission on an enforced class.

Route classes follow the existing route table: viewer-scoped reads (objects, entities, search modes), non-owner writes, federation push. Owner-only routes are unreachable by non-owners and not PACT classes; credential-management routes need a human session and are not agent routes.

### 8. Receipts, the read audit and signing keys

**Two records, one shape, joined.** `access_log` (ADR-080 §9) is the local record of non-owner requests: counts and, in `access_log_objects`, object IDs with retention. It is becoming receipt-shaped in parallel work: a decision, ordered check results with stable reason codes, an operation digest, denied and backstop-refused requests included. A PACT **receipt** is the signed `pact-admission+jwt` artifact (draft §12.1) for each completed authenticated evaluation: decision, ordered `CheckResult`s, digests of profile, intent, operation and requirements, expiry. The join:

- the access-log entry for an evaluated request stores the receipt's `artifact_digest` and the same operation digest; the receipt does not carry object IDs or raw values (draft §12.1, §19);
- receipts prove what the validator asserted. `access_log` records what was released to whom. Only `access_log_objects` can answer "who saw object X", and it keeps its own shorter retention; receipts are kept for the native audit-retention period (draft §12.2) and outlive the object list. Aligning the two retentions is open;
- a non-owner request outside PACT (a device key, a session, a route class whose mode is off) has an access entry and no receipt, and owner requests are not logged by default (ADR-080 §9); a rejection before an authenticated evaluation (bad signature, oversized request) has neither, only security telemetry (draft §12.2);
- **durability differs.** ADR-080's batch writer may lose its buffer at a crash. A receipt is written synchronously before a positive enforced admission is returned; the matching access entry may still be buffered. If the receipt cannot be recorded, there is no positive enforced admission.

**Signing keys.** The existing instance key is Ed25519 and signs config bundles and sync. The draft's baseline is ES256 over P-256; Ed25519 is accepted only if negotiated explicitly with RFC 9864, and the older `EdDSA` identifier is refused (draft §7.2). **Options:** (a) reuse the instance key and negotiate Ed25519; (b) **dedicated ES256 keys for PACT, one per signing role (requirement publisher, validator) (chosen)**; (c) one PACT key for both roles. Reasons: the baseline needs no negotiation and stays interoperable with `pactc` and other implementations; a key per purpose keeps a bundle-signing compromise away from admission (and the reverse); a key per role keeps the "validator accepts only what a configured publisher signed" boundary real in one process. The keys live in the same protected store as the instance key (OS keychain or the secrets layer), never in config or the environment. They are published as the gateway's key set under a configured origin; rotation is by `kid` with overlap and follows whatever key-lifecycle protocol the instance key eventually gets (not decided here). *Amended 2026-10-20 (design note for the first cut, future work):* the two ES256 keys are generated when dpkms starts and live only in memory; they are not in the keychain or the secrets layer, and there is no config key referencing them.

### 9. Library, ownership and how ctxt consumes it (owner-decided in outline)

- **What ctxt consumes.** The Go module `hop.top/pact-go`: canonical JSON (RFC 8785), strict JSON, JWS, JWK thumbprints, digests, request binding, wire types, the adapter contract and its test suite, enrollment and freshness helpers, the conformance runner. **ctxt writes no protocol crypto, canonicalisation, binding or validator logic.** It writes only the native adapter (route table to `OperationContext`, principal and enrollment lookups against its stores, share-policy checks), the state stores, and the management surface.
- **Holder keys.** ctxt keeps an agent's holder private key in a protected store (OS keychain or the secrets layer), per instance key, never in its config or the environment (draft §6.4). The key pair is generated on the agent's side and only the public JWK is sent to enroll. *Amended 2026-10-20 (decision for the implementation, future work):* the private key is a PEM file, mode 0600, under `$XDG_STATE_HOME/ctxt/pact/<instance>/` (`ctxt agent holder init`), not the OS keychain; a keychain key source can replace it behind the library's key-source interface.
- **When.** Code in ctxt that imports `hop.top/pact-go` merges only against a **published tag**, resolved through the module proxy like `hop.top/kit` and `hop.top/c12n`. The gate is the publication of tag `pact-go/v1.0.0-alpha.1` and the `hop-top/pact-go` mirror. No local `replace` and no `go.work` `use` is committed for it, and no vendor directory. Work that needs no import (enrollment tables, state stores, hold, route classification, contract-field derivation as data) can land first.
- **Gaps are filled upstream, not locally.** The validator, contract issuance and holder construction are missing from the module (see Context). If they stay missing, the choice is to build them in `poly-pact` or to stay in observation; a private copy in ctxt is not an option. Which team builds them, and the module's first tag, are open. *Amended 2026-10-20:* they were built in the library; ctxt keeps no copy.
- **Conformance.** CI runs `pactc` with the draft §22.2 rows applicable to the declared profile against a built dpkms. Because `pactc` compares decisions only today, dpkms adds its own receipt-content tests until the runner does more.
- **Federation.** The peer's holder code is the same library, in the pushing instance.

### 10. Enterprise identity providers and the business-owned instance (owner-decided in outline)

A business can own an instance whose principals and roles come from an external IdP, Microsoft Entra ID first (Google, Okta and Keycloak later, each through its own adapter). This promotes ADR-079 §2's "Enterprise OIDC, later and opt-in" from a sign-in convenience to an authority model for such instances.

- **Authority and principals.** The IdP is the authority for who exists and which roles they hold. An IdP principal is identified by the adapter's namespaced identifier, built from issuer, tenant and the issuer's stable subject (for Entra, `oid`, never `email`, `upn` or `preferred_username`; identity mapping IM-4, IM-5, OAuth profile OAUTH-37). dpkms creates the `person` principal and its `principal_identities` row (`method=oidc`, issuer, subject) on the **first admitted request**, with the roles and audiences the current mapping yields. It never links identities by email (ADR-079 §1 stands). If no mapping yields anything, nothing is created.
- **Role and group mapping.** IdP roles and groups map to dpkms role bundles and audiences through **versioned, issuer-scoped mappings**, stored in the instance. Example: the Entra app role `dpkms.Admin` maps to the `admin` bundle, so IdP admins administer the instance. A same-spelled role from another issuer or tenant never matches (draft §8.5, §17.2). A mapping change bumps its version; compiled viewers and outstanding contracts that depend on it are invalidated, as with policy versions. The ADR-079 ceilings still apply: no mapping yields scopes the mapping's author does not hold, and the owner stays protected (ADR-079 §5).
- **No data-admin tier (owner-decided 2026-10-16).** Administering an instance is not access to its data. Access beyond a principal's grants is never a role. When a business instance needs it (compliance, eDiscovery, legal hold), it is an explicit, audited, time-boxed grant to a named steward, issued through the existing audience and share-policy machinery, with a recorded justification and approver, and every read attributed in the read audit and receipts. The local owner stays the only bypass. The steward flow is designed and built as its own piece of work with its own ADR (ADR-083); this ADR fixes only the rule.
- **Admin through the IdP is not owner.** An IdP admin holds the `admin` bundle as an ordinary `person`. It does not become the owner, does not bypass share policies (ADR-080 §2 owner bypass) and cannot mint or touch owner credentials. There is no data-admin tier (owner decision 2026-10-16); access beyond a principal's grants is a steward grant, and the admin bundle carries neither credentials for other principals nor unapproved widening changes (ADR-083).
- **Break-glass.** The local owner (ADR-079 §4, exactly one per instance) stays as break-glass, authenticated by its own keys and passkey, independent of the IdP. When the IdP or a required membership source is unavailable, everyone else fails closed: PACT-evaluated requests are `DEFERRED` (draft §18), and external memberships drop as below. The owner is not affected.
- **Freshness and revocation of memberships.** External group membership (audiences with IdP group sources merged with local members, cached per viewer; an ADR-080 amendment below) has two independent bounds:
  - **Admission freshness.** Every rule over an external source publishes its own `status_max_age_seconds` in the contract (draft §9.2). A cached positive result older than that is never reused, and an expired favourable result is never served as current (draft §18). The value is per source, not global.
  - **Membership TTL.** The viewer's cached external memberships have a TTL. An event that removes a member or disables an account invalidates the cache entry immediately; the TTL bounds events that were missed. Past the TTL with the source unavailable, external memberships drop and the viewer shrinks to its local members. This second bound is what protects human sessions, which PACT does not cover. For agents the admission freshness already forces `DEFERRED` sooner.
  - Events are an accelerator, not the safety net; correctness rests on the TTL and the status bound. An event feed per IdP is adapter-specific.
- **Delegation evidence.** A delegated IdP token carries the user, the client application (`azp`) and the consented scopes (`scp`). It is evidence for the native delegation check, bound to the enrolled agent whose workload is that client; an agent is never bound to a user alone (identity mapping §3.4, OAuth profile OAUTH-38). PACT adds no scope: a missing scope is a native denial (OAUTH-31). App-only tokens (`roles` without `scp`) are not accepted for person principals. Tokens are not forwarded downstream.
- **What the PACT side can support today.** The OAuth profile covers Entra v2.0 access tokens as a draft with no fixtures: "this profile cannot support an enforced-support claim" until its negative cases exist. For token claims it states that no status mechanism exists, so a `status_max_age` below the token lifetime cannot be met from the token alone. A live membership source (Microsoft Graph or equivalent) has no profile; the Graph delegated-execution profile is planned and unwritten. Where a source cannot establish the demanded freshness, the contract either carries a bound the source can meet, set by dpkms as publisher, or the rule defers (draft §17.2). This ADR does not hide that gap behind a default.
- **Out of scope here.** Mirroring per-document permissions from source systems (SharePoint or Drive ACLs). A separate track if wanted.

### 11. Person-principal API keys under enforcement (owner-decided)

*Revised on port. The 2026-10-16 text made this an interim hold that applies at once; the owner decided instead that it applies only when PACT enforcement is on, and that `pact.default` is `off` (see Status).*

When PACT enforcement is on for a route class, dpkms refuses an API key on a principal of kind `person` that is not the credential of an enrolled agent, on that class, with a stable error. Minting such a key (owner-minted or self-minted under `write:self`) is refused with the same error while any class is enforced. Key rows are kept, not revoked, so the migration in §7 can enroll them. With enforcement off for every class (the default, `pact.default=off`) there is no hold and person keys behave as ADR-079 describes. Unaffected in all modes: owner keys (including those narrowed by a share policy), device keys, peer keys (federation has its own gate), and all session-based access. There is no per-key or per-principal exemption on an enforced class; the refusal is lifted only by enrolling the agent. The stable error code is `AGENT_ACCESS_HELD`.

The hold is not built. It lands with enforcement, behind the library's published tag.

### 12. Federation peers

A `peer` principal (ADR-079 §1) keeps its API key as the native credential. On a PACT-enforced instance the pushing instance is additionally an enrolled agent under that principal and presents a contract-bound, request-bound presentation for each push, signed with a holder key kept on the pushing side. The receiving instance trusts publisher and validator keys that its operator configures per target, never keys learned from discovery (draft §4.2). ADR-080 §6 stands: peers remain owner-trusted replicas, not viewers; PACT adds admission to the push route and nothing to what a peer can read. The draft has no federation of native authority (§21): a peer's roles on one instance transfer nothing to another. Same-owner device sync (ADR-074) is outside PACT. Cross-owner pull remains deferred.

### 13. Amendments to ADR-080

- **§1 Vocabulary, "Principal".** Reads "An agent is a key on one of them"; replaced by the ADR-079 amendment in §3 above: an agent of a non-owner principal is an enrolled identity under it.
- **§2 "Keys narrow, never widen".** Unchanged for keys. An enrolled agent is governed by the same intersection (principal view ∩ key policy); PACT admission is an additional AND (§2).
- **§6 Federation row.** Add: inbound pushes are PACT-admitted (§12); peers stay owner-trusted replicas.
- **§9 Audit.** Entries become receipt-shaped and join signed receipts (§8).
- **Audiences.** `audience_members` may be fed by external group sources merged with local members, cached per viewer with the TTL and invalidation of §10. Local members are never removed by an external source's silence.

---

## Rationale

- **PACT fits because it is additive.** Its central rule is that native authorization stays final and a PACT pass grants nothing. That is exactly the contract ADR-080 needs: the leak-proof viewer stays authoritative, and admission narrows who may ask. A protocol that issued access tokens or replaced scopes would have forced a rewrite of the layer ADR-080 spent its length on.
- **An agent needs an identity that outlives its keys.** The three-way split (bearer key, holder key, `agent_id`) is what lets rotation, compromise and attribution each be handled without conflating them. It is also what lets dpkms tell an agent from a person's CLI at all.
- **Non-owner scope matches the risk.** The owner controls the host; a partner's agent does not. Peers hold a whole-corpus write path. Human sessions already have browser protections and a human flow.
- **Observation first.** The draft anticipates it, and it is the only honest way to find the unmapped routes, unenrolled keys and slow paths before refusing real traffic. The labelled records prevent the observation period from being mistaken for protection.
- **No protocol code in ctxt.** The strict parsing, canonicalisation and thumbprint rules are where two implementations of the same security protocol disagree. Letting the module be the only implementation is cheaper than a second one and keeps conformance meaningful.
- **A dedicated ES256 key** avoids a negotiation the draft treats as exceptional, and separates two jobs that should not share a compromise.

### Alternatives considered

| Alternative | Why not |
|---|---|
| Keep keys with share policies; stop at ADR-079 | No agent identity, no purpose, no request binding, no difference between an agent and a person's CLI. Fine for the owner's own agents, which is why they stay there. |
| `agent` principal kind with a parent link | Rejected in ADR-079 for good reasons that still hold: duplicates what a narrowed key expresses and leaves open whether grants flow through the parent. The enrolled-identity model keeps grants on the principal only. |
| Sender-constrained tokens (DPoP, mTLS-bound keys) | Bind a credential to a client key. They do not carry purpose, an operation digest, an enrolled agent identity separate from the key, or a signed per-request receipt. The draft states its proof is not DPoP. They remain a valid native layer under PACT. |
| Our own signed-request scheme | Re-creates the protocol without its negative tests, and puts crypto in ctxt, which the module boundary exists to prevent. |
| Wait for kit to provide it | kit has nothing, and PACT is not a generic transport-authn concern; it needs share-policy and route knowledge only dpkms has. kit stays the native chain. |
| Enforce for every request, humans and owner included | See §1. |
| Reuse the Ed25519 instance key | See §8. |

---

## Consequences

### Positive

- Agents of colleagues and partners can be admitted without a bearer secret being enough. Rotation keeps identity; compromise is terminal and visible.
- Every admitted, refused or deferred evaluation leaves a record that names the agent, the principal, the purpose and the exact operation, joined to a signed receipt. Denials are audited, not only reads.
- Federation pushes stop depending on one long-lived bearer secret.
- Under enforcement, person keys stop being usable without an enrolled agent, so the gap in the Context closes where PACT is on.
- The owner's experience does not change.

### Negative

- **The foundation is a draft.** No interoperability suite and no independent security assessment exists. The wire may change; the dependency is pinned to a tag, and wire version `0.1` is rejected strictly if it moves.
- **The library is incomplete.** Without the validator, contract issuance and holder code, nothing here can be enforced. Until then dpkms ships no admission at all. *Amended 2026-10-20:* those parts exist in the library; the ctxt side is future work, and dpkms evaluates nothing until the operator sets a mode (`pact.default=off`).
- **Two requests per protected operation.** A contract is single-use, bound to one operation and nonce, and lives at most 60 seconds (draft §9.1, §7.5). Every protected request needs a prior contract request. This is latency and load on agent-heavy workloads, and the draft allows no batching. The cost is accepted for the classes under PACT.
- **Replay and receipt stores are on the request path.** Nonce consumption must be atomic across replicas and retained at least until artifact expiry plus the window (draft §7.5); a full or unavailable store defers. SQLite's single writer and Postgres must both be tested; the Postgres half cannot be verified locally.
- **Enrollment friction.** A partner cannot just paste an API key. They must enroll an agent. That is the point, but the experience is new work.
- **Presentation carriage.** Presentations up to 256 KiB do not fit an ordinary header; dpkms must publish a transport that works (draft §13.2), not merely add a header.
- **Weaker claim than "full PACT".** Reputation and Learning are excluded; dpkms must say so wherever it describes itself.

### Neutral or considerations

- Owners' agents are unchanged, including owner keys with narrowing policies.
- gRPC and MCP are closed to non-owner agents in enforcement until a mapping exists. The MCP capability of a share policy has no effect for them meanwhile.
- A business instance's IdP admins administer but do not see data they were not granted.
- ADR-074 device sync and the principals it might replicate are unchanged; agent enrollments are instance-local like principals (plan D13).

### Risks

| Risk | Mitigation |
|---|---|
| Draft changes under us | Pin a tag; strict version checks; the mirror and source are compared when bumping |
| Observation mistaken for protection | Records labelled; discovery and receipts state the mode per class; no "enforced" wording until §7.2 holds |
| An unmapped route lets a non-owner agent through | Route table already classifies every route and a test fails an unclassified one; PACT class is added to the same table |
| Bearer key of an enrolled agent stolen | Useless alone: requests also need the holder key's signature, a fresh contract nonce and a matching operation |
| Holder key stolen | Compromise mark, terminal; security hold; new `agent_id` |
| IdP outage or stale membership | Per-source `status_max_age`, membership TTL, fail closed; owner break-glass |
| Receipts leak | Receipts carry digests and IDs of evidence, not object IDs or values |

---

## Implementation Notes

- **Order.** (1) agent enrollment and the state stores (nonce/replay, contracts, security holds, receipts) with no `hop.top/pact-go` import; (2) the receipt-shaped access log; (3) the native adapter, contract derivation and observation mode, which need the module's validator and issuance at a published tag; (4) enforcement per class, with the person-key hold (§11, which exists only under enforcement) and migration of held keys; (5) peers; (6) the IdP track. The public documentation, operator guide and honest-limits page land with each step, not after.
- **Config sketch** (names illustrative; *amended 2026-10-20:* the planned shape is `pact.default` and `pact.routes[]` taking `off | observe | enforce`, with `pact.default` `off` when unset, and no `pact.keys` in the first cut): a per-class mode `off | observe | enforce`, defaulting to `off`; `pact.keys` for the publisher and validator key references; per-target trust anchors for peers; per-issuer mapping versions and TTLs for IdP sources.
- **Tests.** Draft §22.2 rows applicable to the declared profile through `pactc`, with receipt-content tests of our own. The ADR-080 leak suite gains partner-agent fixtures run in enforce mode: a partner agent with a narrower key, with an unenrolled key, with a rotated holder key, with a replayed nonce, with a mismatched operation. Every new check is mutation-tested: break it, see the tests fail, restore. Tests that start dpkms use isolated data dirs and closed ports.
- **Schema.** SQLite and Postgres migrations in lockstep; the nonce table is shared by all replicas.

## Amendment 2026-10-20: design notes and implementation status

Status against the tree: **nothing in this section is implemented in the tree.** It records the shape the implementation is to take, from the library work, and what the port changed. All of it is future work, gated on the publication of `hop.top/pact-go` (tag `pact-go/v1.0.0-alpha.1`, mirror `hop-top/pact-go`).

- **Library.** The validator, requirement-contract issuance, the gateway middleware and holder/presentation construction exist in the PACT library. Planned: dpkms uses them in `internal/pactgw` (gateway, validator and publisher in one process, draft §4.4); ctxt uses the holder side in `internal/pactholder`. ctxt still writes no protocol logic (§9).
- **Library tag.** Until the tag resolves, ctxt's `go.mod` does not require the library and CI cannot build packages that import it. No local `replace` and no `go.work` `use` is committed as a stopgap.
- **Holder keys.** Planned: `ctxt agent holder init` generates an ES256 holder key per instance and keeps it in a file only its owner can read (0600, `holder-key.pem`) with the binding (`holder.json`, 0600) under `$XDG_STATE_HOME/ctxt/pact/<instance>/`. Not the OS keychain or the secrets layer, as §9 said; never config or the environment. `ctxt agent holder use` binds the enrolled agent, principal and purpose; `ctxt agent holder show` reads the local state.
- **Config.** Planned: a `pact:` section with `pact.default` and `pact.routes[]` (`route`, `class`), each `off`, `observe` or `enforce`. **`pact.default` is `off` when unset.** (The library-work draft said `observe`; the owner decided `off`.) No `pact.keys` in the first cut.
- **Person-key hold.** Applies only when enforcement is on (§11). Not built.
- **Gateway keys.** Planned: the publisher and validator ES256 keys are generated when dpkms starts and kept in memory only (§8). Holders take the publisher's JWKS from the operator.
- **Stores.** Planned: the replay, contract-record and receipt stores in memory, in one process. Receipts would not survive a restart and replicas would share no nonces; durable and shared stores are further future work.
- **Also future work.** Federation peers' holder (§12), the IdP track (§10), rotation of the gateway keys.

## Open questions (not decided here)

1. **Who builds the missing library parts** (validator, contract issuance, holder, receipt packaging), where, and when the module gets its first tag. Enforcement is blocked on it. *Amended 2026-10-20:* the parts were built in the library; what still blocks implementation is the library's published tag.
2. **The `single-owner` profile.** It is registered but unwritten. What it permits for the contract fields `learning_policy_digest` and `learning_artifact_digest`, and which §22.2 rows it removes, is unknown.
3. **Enrollment authority.** Settled (2026-10-16): enrollment is governed by an instance limit and a per-account override, and approval is configurable with owner approval required by default (§3, Enrollment limits and Enrollment approval). Still open: the scope names.
4. **Purpose and operation check.** Its identifier, semantics, and the exact `purpose_id` URI form; the resource granularity of list, search and facet routes; the namespace for local principals and the `tenant_id`/`environment_id` values, including on business instances.
5. **gRPC and MCP mapping.** When the MCP profile and a gRPC mapping exist. Until then non-owner agents have neither.
6. **Owner's agents on untrusted hosts.** Settled 2026-10-16: yes, opt-in per agent by enrollment (§2, §3).
7. **Data-admin tier for business instances.** Settled 2026-10-16: no tier; access beyond grants is an audited steward grant (§10). The steward flow itself is separate work. Designed in ADR-083.
8. **Numbers.** Defaults for per-source `status_max_age`, the membership TTL, the pending-enrollment TTL, the observation exit criteria, and receipt versus object-list retention.
9. **Live membership and event sources.** No PACT profile defines a live Entra membership source or an event feed; Google, Okta and Keycloak adapters are unscheduled.
10. **Algorithms.** Whether Ed25519 is ever negotiated for PACT artifacts (RFC 9864), and the key-lifecycle protocol that would govern PACT key rotation alongside the instance key.
11. **Contract round trip.** Whether the draft will allow any reuse or batching; the cost in Consequences is accepted as written.

---

## References

- ADR-079 (§1 principals, §2 methods, §4 owner, §5 scopes and ceilings), ADR-080 (§2 policies, §4 hidden means absent, §6 derived data, §8 backstop, §9 audit), ADR-064, ADR-068, ADR-074, ADR-077
- Plan: [`docs/plans/2026-10-14-auth-and-sharing.md`](../plans/2026-10-14-auth-and-sharing.md) (D13, D16, D23)
- PACT draft 0.1.0 (wire 0.1) sections 2.3, 4.1, 4.2, 4.4, 6.1 to 6.4, 7.2, 7.5, 8.5, 9.1 to 9.4, 10.1, 10.2, 12.1, 12.2, 13.2, 13.3.1, 14.1, 17.1, 17.2, 17.5, 18, 21, 22.1, 22.2; integration profiles `oauth`, `mcp`; identity-mapping (IM-1 to IM-29), request-binding and freshness profiles; `poly-pact` Go module `hop.top/pact` at `main` 0d9fe3a
- `internal/storage/auth.go`, `internal/server/http/{capabilities.go,handlers_federation.go}`, `internal/sharing/{viewer.go,audit.go}`, `internal/storage/sharing.go`, `internal/bundle`, `cmd/dpkms/cmd/key.go`, `Makefile`
- [RFC 9864](https://www.rfc-editor.org/rfc/rfc9864), [RFC 8785](https://www.rfc-editor.org/rfc/rfc8785), [RFC 7638](https://www.rfc-editor.org/rfc/rfc7638), [RFC 7515](https://www.rfc-editor.org/rfc/rfc7515), [RFC 9449](https://www.rfc-editor.org/rfc/rfc9449)
