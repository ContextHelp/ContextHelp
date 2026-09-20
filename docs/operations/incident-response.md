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
| Malicious plugin | [§4](#4-malicious-plugin) |

---

## 1. Compromised Secret

Secret (API key, token, password) obtained or used by unauthorized party.

### Detection Signals

- Unexpected charges on provider dashboard (OpenAI, Anthropic, etc.)
- API usage spikes in logs: `grep -r "429\|401" /var/log/ctxt/`
- Alert from provider's anomaly detection
- Internal secret scanner (trufflehog, gitleaks) fires
- Colleague reports seeing key in plain text

### Immediate Actions (<1h)

1. **Revoke** the compromised secret — do not wait to confirm misuse:
   - OpenAI: <https://platform.openai.com/api-keys>
   - Anthropic: <https://console.anthropic.com/settings/keys>
   - Registry token: `ctxt config secrets revoke <name>`
   - Other: contact provider support
2. **Rotate** — generate replacement key; update config; restart services.
3. **Scope** — determine which systems held the secret:
   ```
   grep -r "<key-prefix>" /var/log/ctxt/ ~/.config/ctxt/
   git log -p -S "<key-prefix>" --all
   ```
4. **Snapshot** — preserve logs and usage records before rotation purges them.
5. **Notify** security@context.help with timeline draft.

### Follow-up Actions (<24h)

6. **Audit** provider usage logs for the exposure window; note anomalous calls.
7. **Assess data exposure** — what resources could have been read/written?
8. **Cost impact** — review billing; request credits/dispute if applicable.
9. **Add monitoring** — alert on sudden usage spikes for the replaced credential.
10. **Update rotation schedule** — tighten if interval was > 90 days.
11. **Notify affected users** if their data may have been accessed.

### Post-Incident Review Checklist

- [ ] Root cause identified (leak path: env, config file, log line, code)
- [ ] Timeline documented (exposure start → detection → revocation)
- [ ] All systems holding the old secret updated
- [ ] Provider usage log reviewed; unauthorized calls enumerated
- [ ] Monitoring / alerting gap closed
- [ ] Policy updated (rotation schedule, secret storage guidelines)
- [ ] Incident report filed in your operational incident log
- [ ] Retrospective scheduled within 7 days

---

## 2. Secret in Git History

Sensitive value committed and pushed; may be in forks, CI caches, pull request diffs.

### Detection Signals

- CI secret-scanner (trufflehog, gitleaks) fails on push / PR
- `git log -p -S "<key-prefix>" --all` returns hits
- `git filter-repo --analyze` shows secrets in blobs
- Colleague reports seeing raw key in diff / GitHub UI

### Immediate Actions (<1h)

1. **Revoke** the exposed secret immediately (see §1 step 1).
2. **Rotate** — generate replacement; deploy.
3. **Block public access** if repo is public — make private temporarily or delete
   the offending commit via GitHub support emergency contact.
4. **Preserve evidence** — copy raw `git log` output before rewriting history.
5. **Remove from history** (private coord required if team repo):
   ```
   git filter-repo --invert-paths --path <file-with-secret>
   # or strip by string:
   git filter-repo --replace-text <(echo '<secret>==><REDACTED>')
   git push --force --all
   git push --force --tags
   ```
6. **Invalidate CI caches** — GitHub Actions: Settings → Caches → delete all.
7. **Notify team** to re-clone; stale local copies still contain the secret.

### Follow-up Actions (<24h)

8. **Audit forks** — check if any public forks cached the commit.
9. **Rotate any secondary secrets** that co-located in the same commit/file.
10. **Scan full history** for additional secrets:
    ```
    trufflehog git file://. --since-commit HEAD~500 --only-verified
    ```
11. **Add pre-commit hook** if not present:
    ```
    # .pre-commit-config.yaml
    repos:
      - repo: https://github.com/gitleaks/gitleaks
        rev: v8.x.x
        hooks: [{id: gitleaks}]
    ```
12. **Update .gitignore / .ctxtignore** to block future commits of secret files.

### Post-Incident Review Checklist

- [ ] All copies of the secret revoked and rotated
- [ ] Git history rewritten and force-pushed
- [ ] CI/CD caches invalidated
- [ ] Team re-cloned or confirmed clean local state
- [ ] Fork audit completed
- [ ] Pre-commit scanner added / verified
- [ ] .gitignore updated
- [ ] Incident report filed

---

## 3. Unauthorized API Access

External party issuing requests to the ctxt HTTP API or dPKMS endpoints
without authorization, or with a stolen session/token.

### Detection Signals

- Unexpected source IPs in access logs
- Requests from unknown user-agents: `grep -v "ctxt/\|curl" /var/log/ctxt/access.log`
- Rate-limit (429) spikes from a single IP
- Auth failures (401/403) clustering on one token
- Audit log entries for operations not initiated by known users
- Provider billing jump tied to inbound API volume

### Immediate Actions (<1h)

1. **Block offending IP(s)** at network/reverse-proxy level:
   ```
   # Caddy snippet
   @blocked { remote_ip <ip>/<cidr> }
   abort @blocked
   ```
2. **Revoke the abused token/session** via `ctxt config secrets revoke <name>`.
3. **Rotate all tokens** that shared the same scope.
4. **Enable request logging** at DEBUG level if not already:
   ```
   ctxt server --log-level debug 2>/var/log/ctxt/debug.log
   ```
5. **Capture snapshot** of current access log before rotation overwrites it.
6. **Alert** security@context.help.

### Follow-up Actions (<24h)

7. **Correlate** blocked IPs against known threat-intel feeds.
8. **Audit** what the attacker read/wrote: replay access log against entity index.
9. **Assess data exposure** — were private notes, secrets, or PII accessed?
10. **Review auth configuration** — token expiry, scope restrictions, IP allow-lists.
11. **Add rate limiting** if absent:
    ```
    # Caddy snippet
    rate_limit {remote_host} 60r/m
    ```
12. **Add anomaly alert** — flag >N requests/min from single IP.
13. **Notify affected users** if their data was accessed.

### Post-Incident Review Checklist

- [ ] Offending source(s) identified and blocked
- [ ] Abused token(s) revoked and rotated
- [ ] Access log reviewed; scope of read/write enumerated
- [ ] Auth hardening applied (expiry, scopes, IP allow-list)
- [ ] Rate limiting verified active
- [ ] Anomaly alerting configured
- [ ] Affected users notified (if applicable)
- [ ] Incident report filed

---

## 4. Malicious Plugin

Plugin (local or registry) found to exfiltrate data, execute arbitrary code,
or abuse ctxt permissions beyond declared scope.

### Detection Signals

- Unexpected outbound network calls from plugin process
- Plugin accessing file paths outside its declared `allow_paths`
- Unusual CPU/memory usage by plugin subprocess
- `ctxt plugin audit <name>` reports undeclared permissions
- Registry advisory or community report
- `strace` / `dtrace` on plugin process shows suspicious syscalls

### Immediate Actions (<1h)

1. **Disable the plugin immediately**:
   ```
   ctxt plugin disable <name>
   # or remove entirely:
   ctxt plugin remove <name>
   ```
2. **Kill any running plugin subprocess**:
   ```
   pkill -f "ctxt-plugin-<name>"
   ```
3. **Revoke plugin's token/credentials** if it was granted any.
4. **Isolate** — if plugin had write access to index, freeze mutations:
   ```
   ctxt server --read-only
   ```
5. **Preserve evidence** — copy plugin binary, config, and logs before cleanup.
6. **Alert** security@context.help; include plugin name, version, source URL.

### Follow-up Actions (<24h)

7. **Forensic review** — decompile / inspect plugin binary for malicious code.
8. **Audit permissions** the plugin held: `ctxt plugin show <name> --permissions`.
9. **Check all data the plugin touched** — entities, notes, secrets read/written.
10. **Scan other installed plugins** for similar indicators:
    ```
    ctxt plugin audit --all
    ```
11. **Report to registry** if sourced from ctxt marketplace; request takedown.
12. **Notify other users** who may have installed the same plugin version.
13. **Review plugin install policy** — consider requiring signed plugins or
    allowlist-only installs.

### Post-Incident Review Checklist

- [ ] Plugin disabled and removed from all affected systems
- [ ] Plugin subprocess confirmed dead
- [ ] Plugin credentials/tokens revoked
- [ ] Data access scope determined (what was read/written/exfiltrated)
- [ ] Forensic analysis completed; malicious code documented
- [ ] Other plugins audited for same indicators
- [ ] Registry notified; takedown confirmed
- [ ] Other affected users notified
- [ ] Plugin install policy tightened (signing, allowlist)
- [ ] Incident report filed

---

## Filing an Incident Report

Store in your operational incident log, one file per incident, named
`YYYY-MM-DD-<slug>.md`.

Minimum fields:

```
date_detected:
date_resolved:
scenario:          # one of: compromised-secret | secret-in-git | unauth-api | malicious-plugin
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
