---
title: Direct database access
description: The access gate and the database lock that every non-daemon open of a dpkms database goes through.
---

dpkms serve is the one process that owns a SQLite database while it runs. Every other dpkms command that opens the database itself (`job`, `detector`, `session`, `entitlement`, `auth key create`, `config validate`, `housekeeping`, `backup`, `instance set-access`) goes through two gates first: the access class, then the database lock. This page describes both, the errors they return and what to do about each.

Postgres and in-memory SQLite have no database file: neither gate applies. They keep the read/write split described below.

## The access gate

### The access marker

An instance's access class is `server.access` (`private`, `protected` or `public`). A protected or public `dpkms serve` records it in `<storage.path>.access`, a small JSON file beside the database:

```json
{"access":"protected","instance":"home","daemon_addr":"0.0.0.0:8420","updated_at":"2026-10-25T15:00:00Z"}
```

- serve writes it before it opens the database, and again with the HTTP address once its listener is bound.
- It is written atomically (temporary file, fsync, rename) with mode `0600`.
- It is never removed when serve stops. The point of the marker is to keep refusing direct opens while the daemon is down.
- No marker means the configured `server.access` decides. The gate always applies the stricter of the two: a protected config refuses before serve has ever written the marker, and a protected marker refuses under a private config.
- A marker that cannot be read or parsed refuses every direct open (fail closed). `dpkms instance set-access` repairs it.
- The marker and the lock sit beside the database's real path: a database reached through a symlinked file or directory reads the same marker and takes the same lock.
- A private serve never lowers a recorded class. It prints a note naming the command that does.

`dpkms ps` shows each running instance's class in its ACCESS column. That column is for discovery only: the gate reads the marker.

### What the gate refuses

On a protected or public instance every direct open is refused, reads included. The daemon's API, with its authentication and policy, is the way in.

```
instance "home" is access-controlled (protected): direct database access is disabled (/srv/dpkms/db.sqlite)
```

The suggested fix names the daemon API (with the address the daemon last listened on), `dpkms serve` to start it, and `dpkms instance set-access private` to lower the class. The exit code is 5 (`UNAUTHORIZED`): the refusal is permanent, and retrying meets it again.

### Lowering the class

```
dpkms shutdown
dpkms instance set-access private
```

`set-access` takes the database lock, so the daemon must be stopped. Run it as the user that owns the database (not checked on Windows). It writes an `access.set` entry (from, to, instance, database, pid, account) to the instance's audit log before the marker changes, and changes nothing if that write fails. It accepts `private`, `protected` or `public`. A later protected or public `dpkms serve` records its own class again.

### Maintenance on a protected instance

`dpkms housekeeping ...`, `dpkms backup`, `dpkms auth key create` and `dpkms session revoke` take `--maintenance-override`. The last two are the bootstrap and recovery path: the first owner key on a fresh protected instance is `dpkms auth key create --principal owner --maintenance-override`. The override runs the maintenance without lowering the class:

- run it as the user that owns the database file, the daemon's user (not checked on Windows);
- stop the daemon first: the override takes the database lock;
- it appends a `maintenance.override` entry to the instance's audit log (action, class, instance, database, pid, account as `user:<name> (<uid or SID>)`) before anything else, migrations included, and fails if it cannot.

Without the flag, housekeeping and backup on a protected or public instance are refused like any direct open, and the fix names the flag.

### Local federation targets

A federation entry given a path (or `file://` URL) writes straight into the target database. That write would skip the target's federation token, policy engine and lock, so a target whose marker is protected or public is refused: `dpkms serve` fails at startup naming the entry, `dpkms config validate` reports it, and a push already running fails. Configure such a target by its daemon's URL instead; the push then goes through `POST /api/v1/federation/push`. A push into a private target is a write command like any other: it takes the target's lock, so it fails while the target's daemon runs (use the URL then too). Reading the target's watermark takes no lock.

## The database lock

### Mechanism

serve takes an exclusive advisory lock on `<storage.path>.lock` before it opens the database and holds it until it exits.

- Unix (macOS, Linux): `flock(LOCK_EX | LOCK_NB)`.
- Windows: `LockFileEx(LOCKFILE_EXCLUSIVE_LOCK | LOCKFILE_FAIL_IMMEDIATELY)`.

The kernel releases the lock when the holding process exits, however it exits, so there is no stale lock to clean up. The `.lock` file is never removed: leave it in place. After taking the lock the holder writes `{pid, role, instance, started_at}` into the file. That body only names the holder in error messages; the lock is the authority.

serve waits up to 10 seconds for a held lock, which covers a restart that overlaps the previous serve's shutdown. A second serve on a live database fails after that, naming the holder.

There is no handoff protocol. Stopping the daemon releases the lock; the next process to ask for it gets it.

### Write commands

A command that writes takes the lock without waiting, for as long as it has the database open:

- held by a daemon: the command fails at once;
- held by another command: it retries for 2 seconds, then fails.

```
database /srv/dpkms/db.sqlite is in use: database lock held by daemon (pid 4242, instance "home", since 2026-10-25T15:00:00Z): /srv/dpkms/db.sqlite.lock
```

The exit code is 6 (`TRANSIENT`): the database is busy, and the command succeeds once the lock is free. For a daemon holder the fix is to stop it (`dpkms shutdown`) or use a read command. A write is never turned into a no-op.

`housekeeping` (VACUUM, `wal_checkpoint(TRUNCATE)`) is a write command. Its `--dry-run` preview only reads.

### Read commands

A command that only reads (its declared side effect is `read`: `job list|status|log`, `detector list`, `session list`, `entitlement list`, `config validate`, `doctor`) takes no lock and never waits. It opens the database read-only (`mode=ro` and `query_only` on SQLite, `default_transaction_read_only` on Postgres), so no statement it runs can write, and it runs no migrations. SQLite's WAL lets such a reader run beside the daemon.

A read refuses a database that does not exist or has no schema yet (it never creates one), and one at a schema version other than the binary's, and the fix says what to do: start `dpkms serve`, or run any write command, to create or migrate it.

`dpkms config validate` never needs an openable store. With no database yet it checks pipeline overrides against the built-in steps; when the store cannot be read (a protected or public instance, another schema version) it skips installed steps and warns.

### Backups

A plain `dpkms backup` of a private instance takes no lock: its `VACUUM INTO` snapshot is consistent beside a running daemon. With `--maintenance-override` on a protected or public instance it takes the lock.

## File permissions

The marker is cooperative: it stops dpkms commands and well-behaved tools, not a process that opens the file itself. For a protected or public instance the operating system has to do the rest:

- run the daemon as a dedicated user;
- `chmod 0600` the database, its `-wal` and `-shm` files and the `.lock` and `.access` sidecars;
- `chmod 0700` the blob directory (`storage.blob.local.path`). The local blob store creates its directories `0700` and its files `0600`.

Every other user then fails at `open(2)`, and the marker check becomes the clearer error in front of that refusal.

`dpkms doctor` checks this: on a protected or public instance (by config or marker), any of those files with a group or other permission bit is a warning, and `dpkms doctor --strict` fails on it. It also compares `server.access` with the marker. serve runs the same file-mode check at startup and prints a warning per file; `dpkms serve --strict-permissions` refuses to start instead. Neither check runs on Windows, where access is an ACL.

## For contributors

- The migration ladder runs in one process at a time: the daemon, or the one write command holding the lock. Do not add idempotency shims to migrations for concurrent runs; fix the opener instead.
- Open the store from a dpkms command through `openStore(cmd)` / `newService(cmd)` (`internal/directopen`), never with `storageutil.Open` directly. Declare the command's side effect: `read` gets the read-only path, anything else the locked one.
- Design background: the single-writer and policy-boundary analyses under `docs/analysis/` in the repository.
