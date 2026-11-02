CREATE TABLE users (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    username             TEXT NOT NULL UNIQUE COLLATE NOCASE,
    password_hash        TEXT NOT NULL,
    role                 TEXT NOT NULL CHECK (role IN ('admin','user')),
    is_root              INTEGER NOT NULL DEFAULT 0,
    disabled             INTEGER NOT NULL DEFAULT 0,
    must_change_password INTEGER NOT NULL DEFAULT 0,
    created_at           INTEGER NOT NULL,
    last_login_at        INTEGER
);

CREATE TABLE sessions (
    token_hash   TEXT PRIMARY KEY,
    user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at   INTEGER NOT NULL,
    last_seen_at INTEGER NOT NULL,
    expires_at   INTEGER NOT NULL,
    ip           TEXT NOT NULL DEFAULT '',
    user_agent   TEXT NOT NULL DEFAULT ''
);
CREATE INDEX sessions_user ON sessions(user_id);

CREATE TABLE uploads (
    id             TEXT PRIMARY KEY,
    user_id        INTEGER NOT NULL,
    owner_name     TEXT NOT NULL,
    original_name  TEXT NOT NULL,
    size_bytes     INTEGER NOT NULL,
    bytes_received INTEGER NOT NULL DEFAULT 0,
    client_sha256  TEXT NOT NULL DEFAULT '',
    server_sha256  TEXT NOT NULL DEFAULT '',
    state          TEXT NOT NULL,
    error          TEXT NOT NULL DEFAULT '',
    final_path     TEXT NOT NULL DEFAULT '',
    created_at     INTEGER NOT NULL,
    updated_at     INTEGER NOT NULL,
    completed_at   INTEGER
);
CREATE INDEX uploads_user ON uploads(user_id);
CREATE INDEX uploads_state ON uploads(state);
