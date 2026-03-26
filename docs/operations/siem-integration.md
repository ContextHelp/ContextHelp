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
# batch export — all entries as NDJSON
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

## Real-Time Forwarding

### Syslog (UDP / TCP / TLS)

Each write to the audit log is forwarded to a remote syslog receiver.

```yaml
audit:
  syslog:
    enabled: true
    protocol: udp          # udp | tcp | tls
    address: siem.corp.example.com:514
    facility: local0
```

TLS note: system trust store used; custom CA not yet supported — set
`protocol: tcp` and terminate TLS at a reverse proxy if needed.

### Webhook

Each audit event is POSTed as JSON to a configurable URL (async, non-blocking).

```yaml
audit:
  webhook:
    enabled: true
    url: https://ingest.example.com/audit
```

Payload is a JSON-encoded `AuditEntry`:

```json
{
  "id": "evt-abc123",
  "event_type": "delete",
  "object_id": "obj-xyz",
  "actor": "alice@corp.example.com",
  "payload": {"reason": "user request"},
  "created_at": "2026-03-26T12:00:00Z"
}
```

Delivery is best-effort (one attempt, fire-and-forget). For guaranteed delivery
add a queue in front of the webhook receiver.

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

### HTTP Event Collector (webhook)

Set the webhook URL to your HEC endpoint:

```yaml
audit:
  webhook:
    enabled: true
    url: https://splunk.corp.example.com:8088/services/collector/event
```

Add a `Splunk <token>` `Authorization` header at the HEC receiver or via a
reverse proxy that injects the token header.

### CEF via Syslog

Many Splunk deployments ingest CEF over syslog:

```yaml
audit:
  syslog:
    enabled: true
    protocol: tcp
    address: splunk-syslog.corp.example.com:5000
    facility: local0
```

Use `ctxt audit export --format cef` for batch backfill.

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

### Logstash (webhook → pipeline)

Point the webhook at a Logstash HTTP input:

```yaml
audit:
  webhook:
    enabled: true
    url: http://logstash.corp.example.com:5044
```

Logstash pipeline:

```
input {
  http { port => 5044 codec => json }
}
filter {
  date { match => ["created_at", "ISO8601"] }
}
output {
  elasticsearch { hosts => ["https://elastic.corp.example.com:9200"] }
}
```

### Syslog (Logstash syslog input)

```yaml
audit:
  syslog:
    enabled: true
    protocol: tcp
    address: logstash.corp.example.com:5140
    facility: local0
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

### Datadog Logs API (webhook)

Use the Datadog HTTP logs intake as the webhook endpoint:

```yaml
audit:
  webhook:
    enabled: true
    url: https://http-intake.logs.datadoghq.com/api/v2/logs
```

Add `DD-API-KEY` via a proxy that injects the header, or use a lightweight
relay (e.g. Vector, Fluent Bit) between ctxt and the Datadog endpoint.

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
