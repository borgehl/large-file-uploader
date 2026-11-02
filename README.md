# Large File Uploader

Upload very large files (designed for up to just under **100 GiB**) from a browser, behind a login, with
integrity checking end to end. No SFTP knowledge needed.

> **Status:** feature complete and tested with files up to ~300 MiB (unit, integration, live-stack and real-Chrome
> e2e). The 20 GB and ~100 GiB soak runs described in the design doc have **not** been run yet; do that on your
> hardware before relying on it for the largest files. See "Implementation status" in the design doc.

- **Resumable**: built on the [tus](https://tus.io) protocol (`tusd` embedded in the Go backend). Pause, resume,
  survive network drops, tab reloads and server restarts.
- **Verified**: every 32 MiB chunk carries its SHA-256 (a bad chunk is rejected and resent), and the browser
  computes a SHA-256 of the whole original file. The server re-hashes what it stored and only then moves the file
  into place. A mismatch quarantines the file instead.
- **Accounts**: argon2id passwords, cookie sessions, a root admin created from the environment on first start, and
  an admin page for creating users.
- **Stack**: Go + SQLite backend, React 19 / TypeScript / Vite / Tailwind 4 / zustand frontend, nginx, docker compose.

Design and rationale: [docs/design.md](docs/design.md).

## Run it

```bash
cp .env.example .env            # set ROOT_PASSWORD (12+ chars) and review the other values
mkdir -p data/uploads
sudo chown 10001:10001 data/uploads   # must be writable by LFU_UID:LFU_GID from .env
docker compose up --build -d
```

The stack listens on `127.0.0.1:${PORT}` (default 8081) only. TLS is expected from the host nginx with certbot:
use [docs/host-nginx.conf](docs/host-nginx.conf) as the server block. For a quick look on the same machine, open
`http://127.0.0.1:8081`.

1. Log in as `root` with `ROOT_PASSWORD`; you are forced to choose a new password. Then delete `ROOT_PASSWORD`
   from `.env`: it is only used when the user table is empty.
2. **Users** page: create accounts. A temporary password is generated and shown once; the user must change it at
   first login.
3. **Upload** page: drop files in. Finished files are verified before they appear in `complete/`.

### Keeping the uploading machine awake

While a file is hashing or sending, the page requests a browser Screen Wake Lock so the screen and machine stay
awake. This is best effort: it needs HTTPS (or localhost), isn't available in every browser, and is dropped
while the tab is hidden (it is re-taken when the tab is shown again). It can't stop a manual sleep or a closed laptop
lid, so for multi-hour uploads also turn off sleep in the OS power settings. If the machine does sleep, the upload
stalls and resumes by itself on wake, with nothing lost.

### Where the files go

Everything is under `UPLOAD_HOST_DIR` (default `./data/uploads`), a bind mount you read directly on the host:

```
incoming/<id>                         partial uploads (tusd)
complete/<year>/<id>/<file name>      verified files, plus manifest.json (sha256, uploader, timestamps)
quarantine/<id>/data                  failed the checksum; an admin deletes it from the "All uploads" page
```

Files are written `0640` (directories `0750`) with group `FILE_GID`; set `FILE_GID` in `.env` to a host group your
admins belong to. Completed files are never deleted automatically; abandoned partial uploads are removed after
`PARTIAL_EXPIRY_DAYS`.

## Development

```bash
cd backend  && go test ./...                 # unit + integration (real tusd, in-memory SQLite)
cd frontend && pnpm install && pnpm test     # tsc, eslint, vitest
cd frontend && pnpm dev                      # Vite dev server, proxies /api and /files to :8080
```

End-to-end tests run against a live stack (`docker compose up -d`) and are skipped otherwise:

```bash
cd frontend
E2E_BASE=http://127.0.0.1:8081 E2E_ROOT_PASSWORD=<current root password> \
E2E_UPLOAD_DIR=../data/uploads E2E_COMPOSE_DIR=.. pnpm vitest run
```

They cover the real tus client through nginx (corrupted chunk, resume, hash mismatch), a real Chrome session
(login, pause/resume, hashing worker under the CSP), and backend restarts mid-upload. `E2E_CHROME` overrides the
Chrome path. Run them against a throwaway stack: they create users and upload a few hundred MiB.

## Configuration

See [.env.example](.env.example). The ones that matter: `MAX_UPLOAD_BYTES` (files must be *smaller* than this,
default 100 GiB), `CHUNK_SIZE_MIB`, `RESERVE_BYTES` (disk kept free beyond declared upload sizes),
`PARTIAL_EXPIRY_DAYS`, `VERIFY_WORKERS`.
