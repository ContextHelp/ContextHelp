---
status: paper
---

# US-0321: Multi-Instance dpkms ps + Lifecycle

**System Types:** dpkms (self-hosted)
**Personas:** [Operations](../../personas/operations.md)

---

## User Goal

As an operator, I want to run multiple dpkms instances (one per profile),
manage them with `dpkms ps`, `dpkms stop`, and `dpkms reboot`, so I can
operate a multi-tenant or multi-profile setup from a single host without
manual PID tracking.

---

## Context

Each dpkms instance binds a port, owns a DB path, and writes a pidfile at
`$data_dir/dpkms-{port}.pid`. The `ps` subcommand reads all pidfiles in the
data directory, checks liveness, and prints a table. `stop` sends SIGTERM to
the target process. `reboot` = stop + start. Stale pidfiles (process gone)
are cleaned up automatically on `ps` or `serve`. Each instance is fully
independent: separate config.yaml, separate SQLite DB, separate port.

---

## Acceptance Criteria

- [ ] `dpkms ps` shows all live instances: port, profile, PID, uptime
- [ ] `dpkms ps` cleans up stale pidfiles (process gone) automatically
- [ ] `dpkms stop --port 8081` stops the instance on that port (SIGTERM)
- [ ] `dpkms stop --port 8081` returns error if no instance on that port
- [ ] `dpkms reboot --port 8081` restarts instance (stop then start)
- [ ] Reboot preserves the original config and DB path for that port
- [ ] Each instance uses its own DB path and config.yaml (no sharing)
- [ ] Pidfile written at `$data_dir/dpkms-{port}.pid` on `serve`
- [ ] Pidfile removed on clean shutdown (SIGTERM handler)
- [ ] `dpkms serve` on startup cleans stale pidfile for its own port
      before binding (crash recovery)

---

## Implementation Notes

### Pidfile Layout

```
$data_dir/
  dpkms-8080.pid    # contains: PID\nprofile\nconfig_path\nstart_time
  dpkms-8081.pid
  dpkms-8082.pid    # stale if process 8082's PID no longer running
```

### ps Command

```
cmd ps:
    pidfiles = glob("$data_dir/dpkms-*.pid")
    rows = []
    for each pidfile:
        data = read(pidfile)
        pid = data.pid
        if process_exists(pid):
            rows.append({port, profile, pid, uptime: now - data.start_time})
        else:
            trash(pidfile)     // stale: clean up
    print table(rows)
```

### stop Command

```
cmd stop --port P:
    pidfile = "$data_dir/dpkms-{P}.pid"
    if not exists(pidfile):
        return error("no instance on port " + P)
    data = read(pidfile)
    kill(data.pid, SIGTERM)
    wait_for_exit(data.pid, timeout=10s)
```

### reboot Command

```
cmd reboot --port P:
    data = read("$data_dir/dpkms-{P}.pid")
    stop(P)
    start(config: data.config_path)   // re-exec with same config
```

### serve startup (crash recovery)

```
cmd serve --port P --config C:
    pidfile = "$data_dir/dpkms-{P}.pid"
    if exists(pidfile):
        old_pid = read(pidfile).pid
        if not process_exists(old_pid):
            trash(pidfile)    // stale from crash; clean before bind
        else:
            return error("port " + P + " already in use (pid " + old_pid + ")")
    bind(P)
    write_pidfile(P, config: C, start_time: now)
    defer remove(pidfile)
```

---

## E2E Test Checklist

- [ ] Start instance on 8080; `dpkms ps` shows one row with correct port/PID
- [ ] Start second instance on 8081; `dpkms ps` shows two rows
- [ ] `dpkms stop --port 8081`; `dpkms ps` shows one row (8080 only)
- [ ] `dpkms reboot --port 8080`; `dpkms ps` shows 8080 with new PID
- [ ] Kill 8080 with `kill -9`; `dpkms ps` cleans stale pidfile; shows zero rows
- [ ] `dpkms serve` after crash clears stale pidfile and starts cleanly
- [ ] `dpkms stop --port 9999` (no instance) returns non-zero exit + error message
- [ ] Verify each instance has separate DB: insert object in 8080; not visible in 8081

---

## Related Stories

- [US-0318](./US-0318-configure-federation-target.md) — configure federation target
- [US-0319](./US-0319-async-push.md) — async background push
- [US-0320](./US-0320-inline-push-during-ingest.md) — inline push during ingest
- [US-0322](./US-0322-backup-and-rebuild.md) — backup/restore per instance
- [US-0323](./US-0323-dag-chain.md) — DAG chain across instances

---

## E2E Tests

- planned: `test/e2e/federation/multi_instance_test.go::TestMultiInstance_BootstrapTwoInstances`
- planned: `test/e2e/federation/multi_instance_test.go::TestMultiInstance_PromoteToMerged`
- planned: `test/e2e/federation/multi_instance_test.go::TestMultiInstance_DecommissionTarget`
- planned: `test/e2e/federation/multi_instance_test.go::TestMultiInstance_ReassignTarget`
