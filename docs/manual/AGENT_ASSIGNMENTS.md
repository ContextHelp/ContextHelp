# Manual Generation Agent Assignments

This ledger records the five parallel content tracks requested for manual generation.

## Agent A: Troubleshooting

- Scope: `docs/manual/troubleshooting/*`
- Outputs:
  - `README.md`
  - `faq.md`

## Agent B: Reference

- Scope: `docs/manual/reference/*`
- Outputs:
  - `README.md`
  - `api-cli-reference.md`
  - `query-language-and-ranking.md`
  - `config-and-permissions.md`

## Agent C: Operations

- Scope: `docs/manual/operations/*`
- Outputs:
  - `README.md`
  - `runbook.md`

## Agent D: Appendix

- Scope: `docs/manual/appendix/*`
- Outputs:
  - `README.md`
  - `story-to-chapter-index.md`
  - `persona-to-chapter-index.md`
  - `feature-maturity.md`

## Agent E: Admin and Extensibility

- Scope: `docs/manual/admin-extensibility/*`
- Outputs:
  - `README.md`
  - `admin-configuration.md`
  - `pipeline-management.md`
  - `plugin-development.md`

## Notes

- Command examples were aligned to current runtime surfaces exposed by `ctxt --help` and `dpkms --help`.
- Story-target contracts that may be forward-looking are explicitly labeled in reference/persona docs.
