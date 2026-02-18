# Config and Permissions

## Goal

Run predictable, secure deployments by combining validated configuration, explicit runtime controls, and constrained extension permissions.

## Configuration controls

### Validate and inspect config

```bash
ctxt config path
ctxt config show
ctxt config validate
```

Edit config in default editor:

```bash
ctxt config edit
```

### Layering model

Configuration behavior is layered (defaults, system, user, env, flags). For full details:

- [`../../configuration-structure.md`](../../configuration-structure.md)
- [`../../environment-variables/README.md`](../../environment-variables/README.md)

## Runtime controls

### Server settings

```bash
dpkms serve --port 8080 --grpc-port 9090 --workers 4
dpkms serve --public
```

### Profile controls

```bash
ctxt profile list
ctxt profile show <name>
ctxt profile set-default <name>
```

## Pipeline and extension permissions

### Pipeline governance controls

```bash
dpkms pipeline list --include-archived
dpkms pipeline show <name> --raw
dpkms pipeline step list
dpkms pipeline step registry list
```

### Registry update policy

```bash
dpkms pipeline step registry autoupdate <registry-url> --enable
dpkms pipeline step registry autoupdate <registry-url> --disable
```

## Permission and isolation policy references

- Plugin isolation: [`../../plugins/plugin-isolation.md`](../../plugins/plugin-isolation.md)
- Plugin API: [`../../plugins/plugins-api.md`](../../plugins/plugins-api.md)

## Operational safeguards

1. Always run `ctxt config validate` before rollout.
2. Use explicit registry auto-update policies.
3. Prefer staged pipeline changes plus test enqueue runs.
4. Track runtime flags used in production for reproducibility.

## Story alignment

- Config/admin: `US-0027`, `US-0030`, `US-0031`
- Plugin/pipeline permissions: `US-0029`, `US-0042` to `US-0045`, `US-0112`
