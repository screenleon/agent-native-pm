## Summary

<!-- 1-3 bullet points describing what this PR changes and why -->

## Test plan

- [ ] `make lint-governance` passes (rule-lint, doc-lint, prompt-budget validator)
- [ ] `cd frontend && npm ci && npm test && npm run lint && npm run build`
- [ ] `cd backend && go vet ./...`
- [ ] `bash scripts/test-with-sqlite.sh` (no Docker / PostgreSQL needed)
- [ ] `make test` (PostgreSQL path via scripts/test-with-postgres.sh; skip if your change is frontend-only and doesn't touch SQL)
- [ ] Manual verification against the flow that changed (local mode `anpm serve`, server mode `docker compose up`, or UI interaction as relevant)

## Decisions / architecture changes

<!-- If this PR introduces a durable constraint, add a dated entry to DECISIONS.md and
     reference it here. Run `make decisions-conflict-check TEXT="..."` first to surface
     possible conflicts with historical decisions. -->

## Out of scope

<!-- What related work is explicitly deferred, and why -->
