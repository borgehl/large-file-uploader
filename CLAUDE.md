# Project rules

- NEVER publish design docs, plans, or write-ups to claude.ai (Artifact tool or Claude Docs connector). Both are denied in `.claude/settings.json`.
- ALWAYS write design docs as local markdown files in `docs/` (e.g. `docs/design.md`) and keep them in git.

# Commands

- Backend: `cd backend && go vet ./... && go test -race ./...`
- Frontend: `cd frontend && pnpm test` (tsc -b, eslint, vitest) and `pnpm build`
- Live stack: `docker compose up --build -d`; e2e suites in `frontend/e2e` need `E2E_BASE`, `E2E_ROOT_PASSWORD`, `E2E_UPLOAD_DIR`, `E2E_COMPOSE_DIR` (see README). Run them against a throwaway stack only.
- Never delete files in `data/` or other untracked directories without listing them first.
