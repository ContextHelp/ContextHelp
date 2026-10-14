# Incident Response Runbook

Operational playbook for security incidents affecting ctxt / dPKMS deployments.

**See also:** [SECURITY.md](../SECURITY.md) — policy, severity levels, vulnerability reporting.

---

## Quick Reference

| Scenario | Section |
|---|---|
| Compromised secret / API key | [§1](#1-compromised-secret) |
| Secret committed to git history | [§2](#2-secret-in-git-history) |
| Unauthorized API access | [§3](#3-unauthorized-api-access) |
| Malicious registry or step | [§4](#4-malicious-registry-or-step) |

---

## Where to look

dpkms has no access log and no rate limiter. The evidence you have:

- **Process logs**: stderr of `dpkms serve`. Under `dpkms install`: `~/Library/Logs/dpkms/{out,err}.log` (macOS), `~/.local/share/dpkms/dpkms.{out,err}.log` (Linux). `-V` / `-VV` raise verbosity; logs are not sanitized, so treat them as sensitive.
- **Audit log** (in the dpkms database): `ctxt audit list --event-type security.auth_failure` (also `security.acl_denial`, `security.quota_exhausted`), `ctxt audit export`. Needs `admin:audit` over HTTP.
- **Security alerts**: `security.alerts.webhook_url` / `smtp`, if configured, fire on auth-failure and ACL-denial bursts.
- **Web UI sessions**: `dpkms session list --all`.
- **Reverse proxy logs**, if dpkms sits behind one: the only per-request record of source IPs.

---

## 1. Compromised Secret

Secret (provider API key, dpkms token, registry token, federation token) obtained or used by an unauthorized party.

### Detection Signals

- Unexpected charges or usage on the provider dashboard (OpenAI, Anthropic, Gemini, OpenRouter)
- `security.auth_failure` entries or alerts from `security.alerts`
- A secret scanner (gitleaks, trufflehog) fires
- The secret shows up in a log, `ctxt config show` output, or a shared file

### Immediate Actions (<1h)

1. **Revoke** the compromised secret; do not wait to confirm misuse:
   - Provider key: revoke in the provider console (OpenAI <https://platform.openai.com/api-keys>, Anthropic <https://console.anthropic.com/settings/keys>).
   - dpkms token: delete its entry from `server.auth.static.tokens` and restart dpkms. Web UI sessions minted with it end at once.
   - Registry token: `ctxt registry logout <name>`, then revoke it on the registry side.
   - `federation.token`: change it on both peers.
2. **Rotate**: store the replacement (`dpkms secret set <KEY> --prompt` for provider keys; a new 32+ character token in the gitignored secrets config for `server.auth`), then restart dpkms.
3. **Scope**: find every copy:
   ```
   grep -rn "<key-prefix>" ~/.config/contexthelp/ ~/Library/Logs/dpkms/ ~/.local/share/dpkms/
   git log -p -S "<key-prefix>" --all
   ```
4. **Snapshot** logs, the audit log (`ctxt audit export > audit.json`) and provider usage records before anything rotates them.
5. **Notify** the security contact in [SECURITY.md](../SECURITY.md) with a timeline draft.

### Follow-up Actions (<24h)

6. **Audit** provider usage for the exposure window.
7. **Assess data exposure**: a leaked `reader` token reads every object; `writer` also captures; `admin` also deletes and reads the audit log.
8. **Cost impact**: review billing; request credits if applicable.
9. **Close the leak path**: `ctxt config doctor` for plaintext secrets and loose permissions; move secrets to the secrets backend ([../security/secrets-backends.md](../security/secrets-backends.md)).
10. **Notify affected users** if their data may have been read.

### Post-Incident Review Checklist

- [ ] Root cause identified (leak path: env, config file, log line, code)
- [ ] Timeline documented (exposure start → detection → revocation)
- [ ] All systems holding the old secret updated
- [ ] Provider usage reviewed; unauthorized calls enumerated
- [ ] Alerting gap closed (`security.alerts`)
- [ ] Incident report filed
- [ ] Retrospective scheduled within 7 days

---

## 2. Secret in Git History

Sensitive value committed and pushed; may be in forks, CI caches, pull request diffs.

### Detection Signals

- CI secret scan (gitleaks) fails on push / PR
- `git log -p -S "<key-prefix>" --all` returns hits
- Someone reports the raw key in a diff or the GitHub UI

### Immediate Actions (<1h)

1. **Revoke** the exposed secret (see §1 step 1).
2. **Rotate**: generate a replacement; deploy.
3. **Limit access** if the repo is public: make it private temporarily, or contact GitHub support to purge the commit.
4. **Preserve evidence**: copy the raw `git log` output before rewriting history.
5. **Remove from history** (coordinate first on a team repo):
   ```
   git filter-repo --invert-paths --path <file-with-secret>
   # or strip by string:
   git filter-repo --replace-text <(echo '<secret>==><REDACTED>')
   git push --force --all
   git push --force --tags
   ```
6. **Invalidate CI caches**: GitHub Actions → Settings → Caches → delete all.
7. **Tell collaborators to re-clone**; stale local copies still contain the secret.

### Follow-up Actions (<24h)

8. **Audit forks** for cached copies of the commit.
9. **Rotate secondary secrets** from the same commit or file.
10. **Scan full history** for other secrets: `gitleaks detect --redact` (this repo: `bash scripts/secret-scan-tree.sh` for the working tree).
11. **Add a pre-commit scanner** if missing (gitleaks pre-commit hook) and gitignore the secrets file.

### Post-Incident Review Checklist

- [ ] All copies of the secret revoked and rotated
- [ ] Git history rewritten and force-pushed
- [ ] CI/CD caches invalidated
- [ ] Collaborators re-cloned
- [ ] Fork audit completed
- [ ] Pre-commit scanner in place
- [ ] Incident report filed

---

## 3. Unauthorized API Access

Someone calling the dpkms REST, gRPC or MCP API without authorization, or with a stolen token or web UI session.

### Detection Signals

- `security.auth_failure` / `security.acl_denial` entries clustering on one principal, or alerts from `security.alerts`
- Unknown source IPs in reverse proxy logs
- Objects, captures or deletions nobody on the team made (`ctxt log`, `ctxt list --sort updated`)
- Unknown web UI sessions in `dpkms session list --all`
- Provider billing jump from unexpected enrichment volume

### Immediate Actions (<1h)

1. **Cut access**:
   - Block the source at the reverse proxy or firewall.
   - Remove the abused token from `server.auth.static.tokens` and restart dpkms; `dpkms session revoke --principal <name>` signs its browsers out.
   - If the instance should not be reachable at all: set `server.access: private` (loopback only) and restart.
2. **Rotate** every token that shared the same role.
3. **Raise log verbosity** for the investigation: restart with `dpkms serve -V`.
4. **Snapshot** proxy logs, process logs and `ctxt audit export` output.
5. **Alert** the security contact in [SECURITY.md](../SECURITY.md).

### Follow-up Actions (<24h)

6. **Assess what was reachable**: the token's role bounds it (`reader`: read everything incl. MCP; `writer`: also capture and edit; `admin`: also delete, audit, pipelines). Object reads are not audited, so assume a reader saw everything.
7. **Check for writes**: `ctxt log` (changelog) and `ctxt list --sort updated` for unexpected objects or edits.
8. **Harden**: one token per client with the lowest role it needs; `server.allowed_hosts` on protected instances; TLS and rate limiting at the proxy ([../manual/operations/reverse-proxy.md](../manual/operations/reverse-proxy.md)); `security.alerts` delivery.
9. **Notify affected users** if their data was read.

### Post-Incident Review Checklist

- [ ] Source(s) identified and blocked
- [ ] Abused token(s) removed and rotated; sessions revoked
- [ ] Scope of reads/writes assessed
- [ ] Proxy rate limiting and TLS verified
- [ ] `security.alerts` delivery configured
- [ ] Affected users notified (if applicable)
- [ ] Incident report filed

---

## 4. Malicious Registry or Step

There is no plugin system ([ADR-078](../decisions/ADR-078-no-plugin-system.md)) and no installed external step is executed today, so the live risk is registry data written into the graph.

### Detection Signals

- Unexpected entities, aliases or taxonomy after `ctxt registry sync`
- Signature errors on `ctxt registry sync` (signature missing or invalid)
- A registry advisory or community report

### Immediate Actions (<1h)

1. **Stop syncing**: set the registry's `trust_level: sandboxed` (no graph writes) or remove it from `registries`, and `ctxt registry delete <name>`.
2. **Revoke its credentials**: `ctxt registry logout <name>`.
3. **Require signatures**: `registries_global.require_signatures: true`.
4. **Remove installed steps** from that source: `dpkms pipeline step list`, then `dpkms pipeline step uninstall <name>`.
5. **Snapshot** the database (`dpkms backup`) before cleaning up, as evidence.

### Follow-up Actions (<24h)

6. **Find what it wrote**: entities from the registry (`ctxt entity list`), and objects referencing them.
7. **Restore** from a backup taken before the bad sync if cleanup is not practical (`dpkms restore`).
8. **Report** to the registry operator.

### Post-Incident Review Checklist

- [ ] Registry sandboxed or removed; credentials revoked
- [ ] Signatures required
- [ ] Written entities identified and removed or restored
- [ ] Registry operator notified
- [ ] Incident report filed

---

## Filing an Incident Report

Store in your operational incident log, one file per incident, named
`YYYY-MM-DD-<slug>.md`.

Minimum fields:

```
date_detected:
date_resolved:
scenario:          # one of: compromised-secret | secret-in-git | unauth-api | malicious-registry
severity:          # critical | high | medium | low
summary:
timeline:          # bullet list: timestamp → action
root_cause:
impact:
remediation:
follow_ups:        # open action items with owners
```

---

*Author: $USER — last updated 2026-03-25*
