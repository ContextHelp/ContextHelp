# Pre-Alpha Publish Gate

Operator-facing gate order for publishing an early-access pre-alpha build.

`make` is the runner in this repo; `make help` lists every target.

## Gate Order

1. Validate local tooling and optional provider dependencies.

   ```bash
   make preflight
   ```

2. Build the local binaries.

   ```bash
   make build
   ```

3. Run the primary local quality gate.

   ```bash
   make check
   ```

4. Run binary smoke coverage against the built artifacts.

   ```bash
   make test-smoke
   ```

5. Start backing services needed for integration coverage.

   ```bash
   make services-up
   ```

6. Run the broader integration suite.

   ```bash
   make test-integration
   ```

7. Run the focused end-to-end integration scenarios.

   ```bash
   make test-e2e
   ```

8. Build release artifacts locally with the GoReleaser config the release
   workflow uses (snapshot: no tag, no publish, no signing).

   ```bash
   make goreleaser-check
   scripts/ci/tools.sh goreleaser -- goreleaser release --snapshot --clean --skip=sign
   ```

9. If you are publishing the container path, verify the production Docker flow.

   ```bash
   # docker/.env.prod, docker/dpkms-auth.yaml, docker/dpkms-local.yaml: docs/deployment/docker.md
   docker compose --env-file docker/.env.prod build dpkms
   docker compose --env-file docker/.env.prod --profile prod up -d
   docker compose --env-file docker/.env.prod ps
   curl http://127.0.0.1:8080/health
   docker compose --env-file docker/.env.prod --profile prod down
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

- `make check` is the primary local gate. It is not a substitute for the full pre-alpha publish gate above.
- Run `make services-down` after integration testing if you started backing services.
- `make docker-build`, `make docker-prod` and `make docker-down` wrap the production Docker commands in step 9.
