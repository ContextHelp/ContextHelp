# AGENTS — cmd/dpkms

For agents touching the dpkms CLI surface. Read this before editing anything under `cmd/dpkms/**`.

## Repo shape

- Bare-repo + worktree layout. The main checkout lives at `./hops/main`; feature work goes in sibling worktrees under `hops/<branch>`.
- Always invoke git as `/usr/bin/git` (avoid the rtk wrapper for git in worktrees).
- No `vendor/`. If one appears, delete it; never run `go mod vendor`.

## Build + verify

- Build: `/usr/bin/env go build -buildvcs=false ./cmd/dpkms/...`
- Test: `/usr/bin/env go test -count=1 ./cmd/dpkms/...`
- Run locally: `/usr/bin/env go run ./cmd/dpkms <args>` or `./bin/dpkms <args>` after `make build`.

## Cobra root + groups

- Root command, persistent flags, viper bindings, and group taxonomy live in `cmd/dpkms/cmd/root.go`.
- Groups (see `commandGroups` map):
  - `LIFECYCLE` — `serve`, `shutdown`, `reboot`, `ps`
  - `DATA` — `backup`, `restore`, `housekeeping`
  - `PIPELINES` — `pipeline`, `detector`, `job`
  - `SECURITY` — `key`, `secret`
  - `DEVELOPMENT` — `dev`
  - `MANAGEMENT` — `version`, `completion`, `install-deps` (hidden by default; surfaced with `--help-all` or `--help-management`)
- New subcommands: register with `rootCmd.AddCommand(...)` from the subcommand's `init()`, then add the command's `Name()` to `commandGroups` so it lands in the right help section.
- Group visibility is applied by `root.ApplyGroupVisibility()` in `Execute()` — kit-owned. Do not reimplement it.

## Kit dependency

- `hop.top/kit/go/console/cli` (aliased `kitcli`) provides the root construction, persistent globals (`-C/--chdir`, `--quiet`, `--no-color`, `--format`, `--verbose`, `--confirm`, `--max-ops`, `--policy`, `--api-version`), and group/help mechanics.
- Output formatting: `hop.top/kit/go/console/output` — owns `-o/--output` (output path), `--format`, `--cols`, `--format-opt`, `--format-help`. Subcommand-local flags must NOT collide with these names; if a destination directory is needed, use `--output-dir`.
- Hook composition runs in this order: kit chain (chdir → identity → peer → progress) → `Hooks.PrePersistentRunE` (where dpkms wires its own `--output`→`--format` shim and `logger.Init`).
- Use kit packages for `log`, `output`, `cli`, `upgrade` — never re-implement equivalents in `internal/`.

## Conventions

- Conventional Commits: `fix(dpkms/<area>): <subject>`. No tracker IDs in commit subjects, bodies, branch names, or code comments.
- Never mention co-authors, Claude, AI, or the tools used.
- File length under ~500 LOC; split when growing.
- Standards-first: cobra/viper/POSIX defaults before custom flags. Avoid short aliases when kit reserves the letter (notably `-o`).

## CLI conventions

- Full guide: internal notes
- Side-effect annotations on mutating commands (`kit/side-effect`) are validated by kit when `EnforceValidate` flips on. Annotate new mutating subcommands when you add them.
