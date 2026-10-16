# SIEM Integration Guide

SIEM-ready audit log export for Splunk, Elastic/ELK, and Datadog.

---

## Formats

| Format   | Flag              | Description                               |
|----------|-------------------|-------------------------------------------|
| `json`   | `--format json`   | NDJSON — one JSON object per line         |
| `cef`    | `--format cef`    | ArcSight Common Event Format v0           |
| `syslog` | `--format syslog` | RFC 5424 structured-data text (one/line)  |

---

## CLI Export

```
# all entries as NDJSON
ctxt audit export --format json > audit.ndjson

# CEF export for last 24 h
ctxt audit export --format cef --since 24h > audit.cef

# syslog-text export filtered by actor
ctxt audit export --format syslog --actor alice@corp.example.com
```

List (paginated view, not for piping):

```
ctxt audit list --since 7d --event-type delete
```

---

## Scheduled Shipping

Nothing forwards the audit log as it is written. Ship it by running
`ctxt audit export` on a schedule and pointing your SIEM agent at the output
file. Export is unlimited unless you pass `--limit`.

```
# cron, hourly: append the last hour (plus a margin) as NDJSON
0 * * * * ctxt audit export --format json --since 65m >> /var/log/ctxt/audit.ndjson
```

The margin makes consecutive windows overlap; deduplicate on `id` in the
SIEM.

---

## Splunk

### Universal Forwarder (file monitor)

```
ctxt audit export --format json >> /var/log/ctxt/audit.ndjson
```

`inputs.conf` — monitor the file:

```
[monitor:///var/log/ctxt/audit.ndjson]
index = ctxt_audit
sourcetype = _json
```

`props.conf` — parse timestamps:

```
[_json]
TIME_FORMAT = %Y-%m-%dT%H:%M:%SZ
TIME_PREFIX = "created_at":"
```

---

## Elastic / ELK Stack

### Filebeat (NDJSON)

```
ctxt audit export --format json >> /var/log/ctxt/audit.ndjson
```

`filebeat.yml`:

```yaml
filebeat.inputs:
  - type: log
    paths:
      - /var/log/ctxt/audit.ndjson
    json.keys_under_root: true
    json.add_error_key: true

output.elasticsearch:
  hosts: ["https://elastic.corp.example.com:9200"]
  index: "ctxt-audit-%{+yyyy.MM.dd}"
```

---

## Datadog

### Log Agent (NDJSON file)

```
ctxt audit export --format json >> /var/log/ctxt/audit.ndjson
```

`/etc/datadog-agent/conf.d/ctxt_audit.yaml`:

```yaml
logs:
  - type: file
    path: /var/log/ctxt/audit.ndjson
    service: ctxt
    source: ctxt-audit
    log_processing_rules:
      - type: multi_line
        name: json_single_line
        pattern: '^\{'
```

---

## AuditEntry Schema Reference

| Field        | Type   | Description                                 |
|--------------|--------|---------------------------------------------|
| `id`         | string | Unique event ID (UUID-like)                 |
| `event_type` | string | Operation: create, update, delete, sync, …  |
| `object_id`  | string | Affected resource identifier                |
| `actor`      | string | Principal who performed the operation       |
| `payload`    | object | Operation-specific metadata (JSON)          |
| `created_at` | string | RFC 3339 UTC timestamp                      |

---

## See Also

- [incident-response.md](incident-response.md) — what to do when audit shows compromise
- [compliance.md](compliance.md) — GDPR / SOC 2 control mapping
