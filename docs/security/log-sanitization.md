# Log Sanitization

ctxt and dpkms have no log sanitization layer. Log output goes through
`log/slog` with the kit console logger (`internal/logger`) straight to
stderr; nothing scans or redacts it, and there is no config key or env var
that turns redaction on. Debug output (`-V` / `--verbose`) is not filtered
either.

What does keep secrets out of output, and what does not, is listed in
[Secrets Validation and Log Sanitization](secrets-validation-and-log-sanitization.md#what-output-shows-secrets).
