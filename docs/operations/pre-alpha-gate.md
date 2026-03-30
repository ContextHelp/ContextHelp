# Pre-Alpha Publish Gate

Operator-facing gate order for publishing an early-access pre-alpha build.

`task` is the preferred runner in this repo. If `task` is not on your shell `PATH`,
run the same commands via `mise exec -- task ...`.

## Gate Order

1. Validate local tooling and optional provider dependencies.

   ```bash
   task preflight
   ```

2. Build the local binaries.

   ```bash
   task build
   ```

3. Run the primary local quality gate.

   ```bash
   task check
   ```

4. Run binary smoke coverage against the built artifacts.

   ```bash
   task test:smoke
   ```

5. Start backing services needed for integration coverage.

   ```bash
   task services:up
   ```

6. Run the broader integration suite.

   ```bash
   task test:integration
   ```

7. Run the focused end-to-end integration scenarios.

   ```bash
   task test:e2e
   ```

8. Build release-grade binaries and package release artifacts.

   ```bash
   task build:prod
   task release:build
   ```

9. If you are publishing the container path, verify the production Docker flow.

   ```bash
   docker compose build dpkms
   docker compose --profile prod up -d
   docker compose ps
   curl http://127.0.0.1:8080/health
   docker compose --profile prod down
   ```

10. Run a manual product sanity pass before tagging or announcing the build.

## Manual Verification

- `GET /health` returns `{"status":"ok"}`.
- Analyze content, wait for the job to complete, and open the resulting object.
- Run a search and confirm the new object is retrievable.
- Verify entity/backlink flows on a mention-rich object.
- Verify registry, pipeline, and inbox flows if they are in scope for the release.
- Verify `/ui/` loads if the web UI is part of the publish surface.
- Verify Raycast or other extension surfaces only if they are part of the announced pre-alpha.

## Notes

- `task check` is the primary local gate. It is not a substitute for the full pre-alpha publish gate above.
- Run `task services:down` after integration testing if you started backing services.
- Production Docker uses direct `docker compose` commands because there is no production Task target today.
