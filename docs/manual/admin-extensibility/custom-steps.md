# Add a custom step (PII masking)

## Goal

Run your own program on every captured object before dpkms indexes, embeds, stores, or sends it to any provider. The worked example masks email addresses and phone numbers.

## How a step works

A step is a program dpkms runs once per object while a pipeline processes it. dpkms writes the knowledge object as JSON to the program's stdin. The program writes one JSON object to stdout: either `{"object": {...}}` holding only the fields it changed, or `{"error": {"code": "...", "message": "..."}}`. Any field the output omits keeps its original value.

A step may change only these fields: `type`, `subtype`, `raw_content`, `text_content`, `content_type`, `metadata`, `summaries`, `sections`, `tags`, `mentions`, `decisions`, `tasks`, `graph`, `inbox_note`. It may repeat read-only fields (`id`, `source`, `pipeline`, `created_at`, and others) unchanged. Changing a read-only field, or sending an unknown field, fails the object.

The program gets these environment variables: `DPKMS_STEP_NAME`, `DPKMS_PIPELINE_NAME`, `DPKMS_STEP_CONFIG_PATH` (a JSON file holding the step's `config` from `dpkms.yaml`; `{}` when none is set), `SANDBOX_ENABLED`, `SANDBOX_ISOLATION_LEVEL`, `SANDBOX_TIMEOUT`, `SANDBOX_NETWORK`. When `SKILL.md` sets resource limits it also gets `SANDBOX_MAX_MEMORY` and `SANDBOX_MAX_CPU`. Under process isolation it additionally gets `PATH=/usr/bin:/bin`, `LC_ALL=C`, and `HOME` and `TMPDIR` set to a private working directory that is also its current directory and is deleted after the run. Nothing from dpkms's own environment reaches the step, so API keys are not available.

A step is a directory containing a `SKILL.md` file with YAML frontmatter, plus the executable it names. The frontmatter from `examples/steps/pii-mask/SKILL.md` is:

```yaml
---
name: pii-mask
description: Masks email addresses and phone numbers in captured text
version: 1.0.0
execution:
  type: external
  executable: pii-mask
  minimum_isolation: process
  config_schema:
    type: object
    additionalProperties: false
    properties:
      email_mask: {type: string}
      phone_mask: {type: string}
  resource_requirements:
    max_memory: 256MB
    timeout: 10s
    max_output: 64MB
---
```

The full protocol is [ADR-058](https://github.com/ContextHelp/ContextHelp/blob/next/docs/decisions/ADR-058-external-step-execution-protocol.md).

## 1. Build the example step

The example is a Go program in `examples/steps/pii-mask`. It masks email addresses and phone numbers in `raw_content`, `text_content`, `inbox_note`, `summaries`, and `sections`. Build it with:

```bash
go build -o examples/steps/pii-mask/pii-mask ./examples/steps/pii-mask
```

Any language works. The executable must have its executable bit set. The patterns in the example are illustrative, not a complete PII detector.

## 2. Install it

```bash
dpkms pipeline step install pii-mask --path examples/steps/pii-mask
```

The CLI packs the directory and sends it to the server, which copies it into the steps directory (`steps.path`, default `~/.config/contexthelp/steps`) as `pii-mask/` and records the SHA-256 of the executable. Every run checks that digest. A changed executable never runs; reinstall it to update.

Install is refused in these cases:

- The name matches a built-in step.
- A step with that name is already installed.
- The `name` field in `SKILL.md` differs from the name you passed on the command line.
- The directory holds anything other than regular files and directories (a symlink, for example).

Confirm the step appears with:

```bash
dpkms pipeline step list
```

## 3. Put it in your pipelines

In `dpkms.yaml`, add the step under `pipelines.overrides.<pipeline>.insert_steps`. Each entry takes these fields:

- `step`: the step name (required).
- `before` or `after`: the name of a step already in that pipeline. Omitting both appends at the end.
- `on_error`: `fail` (default) or `continue`. Never use `continue` for a masking step.
- `timeout`: overrides the `timeout` in `SKILL.md`.
- `isolation`: `container` only, raises the isolation level.
- `config`: a map written to the config file and checked against `config_schema` at start.

`on_error`, `timeout`, `isolation`, and `config` apply to installed steps only.

Example configuration:

```yaml
pipelines:
  overrides:
    text.short:
      insert_steps:
        - step: pii-mask
          before: typedetector
          config:
            email_mask: "[email]"
            phone_mask: "[phone]"
    text.long:
      insert_steps:
        - step: pii-mask
          before: typedetector
    url.generic:
      insert_steps:
        - step: pii-mask
          after: html_cleaner
```

Place the step before the first step that sends content to a provider: `tagger`, `sectioner`, `entity_extractor`, `graph_extractor`, `structured_metadata` (when an LLM is configured), or `embedding`. Also place it before `externalize_content`, which moves large content to the blob store. Storage and the full-text index happen after the last step, so any position before them is covered.

For text pipelines, inserting first is simplest. In URL pipelines the content only exists after `url_fetcher` and `html_cleaner`, so insert after `html_cleaner`.

Overrides are per pipeline. Add the step to every pipeline your captures go through, including pipelines that receive objects handed off by another pipeline (for example, after `content_type_router`). Inserts apply in order after `skip_steps`, so an earlier insert can serve as an anchor for a later one. Anchors use the step names from the pipeline definition; if you use a name that does not exist in that pipeline, the error message lists the valid names.

## 4. Check the config and restart

Validate your changes before restarting:

```bash
dpkms config validate
```

This checks the overrides against the installed steps and each pipeline's step list. It reports errors by field, for example:

```text
pipelines.overrides.text.short.insert_steps[0].step: unknown step "pii-mask": not a built-in step and not installed (dpkms pipeline step list)
```

The same checks run at startup: `dpkms serve` refuses to start on the same errors. Install the step before adding it to the config, then restart `dpkms serve`.

## 5. Verify

Capture a sample object:

```bash
ctxt analyze "Call jane@example.com or +1 (415) 555-0123"
```

Check the stored object's content. You should see `[email]` and `[phone]` where the address and number appeared. Confirm a full-text search for the address finds nothing:

```bash
ctxt find jane --fts
```

## When a step fails

With the default `on_error: fail`, the object fails closed: it is not stored and its job fails with an error that names the step.

- A crash (non-zero exit) or a timeout is retried like any other failed job.
- A step-reported error, invalid output, output over `max_output`, a changed read-only field, or a changed executable fail the job at once, without retrying.
- A step can request a retry by setting `"retryable": true` in its error.

Check the job error with:

```bash
dpkms job status <job-id>
```

Step stderr is not stored. It appears in the dpkms log at debug level only. dpkms keeps the object's content out of its own error messages; keep it out of your step's `message` too.

With `on_error: continue`, dpkms logs the failure and passes the object on unchanged. Do not use this for a masking step.

## What masking cannot cover

A few things a step cannot reach:

- **Job records.** The job record keeps the content you submitted (the job payload), unmasked, after the job completes.
- **Raw ingestion.** `ctxt analyze --raw` stores the object without running any pipeline.
- **Media originals.** Media pipelines send the original image, audio, or video to OCR, transcription, or vision providers. A step can only mask the text they produce.
- **The `source` field.** A captured URL or other source identifier is read-only for steps.

## Isolation

The default is process isolation. What it enforces: the minimal environment described above, a private working directory, the process group killed on timeout, a CPU-time limit derived from the timeout, open-file and file-size limits, a memory limit (Linux only; macOS cannot set it), and the stdout cap.

What process isolation does not enforce: filesystem access (the step can read and write whatever the dpkms user can), network access (`SANDBOX_NETWORK=false` is advisory), and descriptor inheritance (Go opens its own files close-on-exec so the step gets only stdin, stdout, and stderr, but a descriptor a C library opened without close-on-exec would be inherited).

Container isolation runs the executable with the docker CLI on the dpkms host. It enforces: no network unless `SKILL.md` sets `network: true`, a read-only root filesystem and step directory, a 64 MB `/tmp` tmpfs, all capabilities dropped, no new privileges, user 65534, 128 processes, and the step's `max_memory` and `max_cpu`. Container isolation requires `container_image` in `SKILL.md` and docker on the dpkms host. Without docker, the object fails.

A step runs in a container when:

- `SKILL.md` sets `minimum_isolation: container`.
- Its registry has `trust_level: sandboxed` (and then never with network).
- The insert in `dpkms.yaml` sets `isolation: container`.

`max_cpu` applies in containers only.

## Install from a registry

```bash
dpkms pipeline step install <name> --registry <url>
```

The registry must be listed under `registries` in `dpkms.yaml` with one of these trust levels:

- `trust_level: trusted`: the step runs as installed.
- `trust_level: sandboxed`: the step always runs in a container with no network.

An untrusted registry (the default) is refused. A step is code, so trusting a registry to run it is a config decision, not an install-time default.

The registry manifest lists each step with `name`, `version`, `archive` (path of a `.tar.gz` of the step directory with `SKILL.md` at its root, relative to the registry URL), and `sha256` (the archive's SHA-256). Install is refused when the digest does not match, before anything is unpacked.

## Uninstall

Remove the step from all `insert_steps` entries in `dpkms.yaml` first, then run:

```bash
dpkms pipeline step uninstall pii-mask
```

This deletes the step's directory and its record. Uninstall is refused while any `insert_steps` still names the step.

## Reference

- [ADR-058: external step execution protocol](https://github.com/ContextHelp/ContextHelp/blob/next/docs/decisions/ADR-058-external-step-execution-protocol.md)
- [Pipeline management](pipeline-management.md)
- [Config reference: pipeline overrides](../reference/config-and-permissions.md#pipeline-overrides)
