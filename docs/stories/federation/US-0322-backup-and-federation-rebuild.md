# US-0322: Backup and Federation Rebuild

**System Types:** dpkms (self-hosted)
**Personas:** [Operations](../../personas/operations.md)

---

## User Goal

As an operator, I want to backup an instance (DB + config), restore it on a new
host, and have federation sync rebuild all downstream DBs automatically after
`dpkms serve` starts — so no manual federation reconstruction is needed.

---

## Context

Each dPKMS instance stores two artifacts of significance: the SQLite DB (all
objects, watermarks) and `config.yaml` (AI providers, pipelines, federation
topology). Federation topology is pure config — downstream DBs are fully
derivable from source objects. Housekeeping (VACUUM, index rebuild) is
per-instance and federation-unaware. After restore + serve, the federation
goroutines re-push all objects since the downstream's last watermark (or from
zero if the downstream DB was deleted). No special federation backup step is
required — the source DB is the source of truth.

ADR-064: backup scope = one DB + one config per instance; push-only DAG;
downstream DBs are always derivable.

---

## Acceptance Criteria

- [ ] `dpkms backup` creates `backup-<timestamp>.tar.gz` containing:
  - `db.sqlite` (instance DB)
  - `config.yaml` (includes `federations:` block)
- [ ] `config.yaml` inside archive contains full `federations:` entries
  (topology is config, not data — must travel with backup)
- [ ] `dpkms restore <backup.tar.gz>` unpacks both files to the configured data
  dir, overwriting existing files after confirmation
- [ ] after `dpkms serve`, federation goroutines start automatically and push
  all objects to configured targets (async or inline per config)
- [ ] downstream DBs are repopulated without operator intervention
- [ ] no special "federation backup" command — downstream DBs are derivable from
  source; backing up source is sufficient
- [ ] housekeeping (`VACUUM`, index rebuild) runs per-instance only; no
  federation-aware housekeeping needed

---

## Implementation Notes

### Backup

```
backup():
  archive = tar.gz(db.sqlite, config.yaml)
  write → backup-<RFC3339>.tar.gz
```

### Restore

```
restore(archive):
  prompt confirm if data dir non-empty
  untar → data_dir/db.sqlite, data_dir/config.yaml
  print "restored; run dpkms serve to start"
```

### Serve → federation rebuild

```
serve():
  load config.yaml
  for each federation target in config.federations:
    spawn pusher goroutine
    pusher reads watermark from federation_watermarks table
    pusher pushes all objects with id > watermark to target
    watermark advances after each successful push batch
```

### Housekeeping (per-instance, no federation scope)

```
housekeeping():
  VACUUM
  PRAGMA optimize
  rebuild indexes
  # no cross-instance ops; each instance runs its own housekeeping
```

### Config structure (relevant excerpt)

```yaml
federations:
  - name: local-merged
    target: /var/dpkms/merged/db.sqlite
    mode: async          # or inline
    interval: 30s
  - name: team-server
    target: http://team.internal:8080
    mode: async
    interval: 60s
```

---

## E2E Test Checklist

- [ ] run `dpkms backup` on source instance; verify archive contains `db.sqlite`
  and `config.yaml` with `federations:` block intact
- [ ] stop source; delete the downstream merged DB
- [ ] run `dpkms restore <archive>` on source host; verify both files unpacked
- [ ] run `dpkms serve`; observe federation goroutines start in logs
- [ ] wait one sync interval; verify downstream merged DB repopulated with all
  objects from source
- [ ] verify object count in downstream matches source
- [ ] verify no duplicate objects in downstream (content-hash dedup)
- [ ] run `dpkms housekeeping compact` on source; confirm no federation ops
  triggered (logs show only local VACUUM)

---

## Related Stories

- [US-0318](./US-0318-configure-federation.md) — configure federation topology
- [US-0319](./US-0319-async-federation-push.md) — async push goroutine lifecycle
- [US-0320](./US-0320-inline-federation-push.md) — inline push on ingest
- [US-0321](./US-0321-federation-lifecycle.md) — serve startup / shutdown
- [US-0323](./US-0323-dag-federation-chain.md) — DAG chain push
- [US-0034](../operations/US-0034-export-and-backup-all-knowledge.md) — general
  export/backup story

---

## E2E Tests

- planned: `test/e2e/federation/backup_rebuild_test.go::TestBackup_ExportThenRestore`
- planned: `test/e2e/federation/backup_rebuild_test.go::TestBackup_FederationStateSurvivesRebuild`
- planned: `test/e2e/federation/backup_rebuild_test.go::TestBackup_PartialRestoreFlagsRetry`
