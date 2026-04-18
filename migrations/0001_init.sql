CREATE TABLE releases_seen (
    package       TEXT    NOT NULL,
    version       TEXT    NOT NULL,
    posted_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    tg_message_id INTEGER,
    release_url   TEXT,
    PRIMARY KEY (package, version)
);

CREATE TABLE articles_seen (
    url_hash    TEXT PRIMARY KEY,
    url         TEXT NOT NULL,
    title       TEXT,
    source      TEXT,
    seen_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    posted_at   DATETIME
);

CREATE INDEX idx_articles_seen_at ON articles_seen(seen_at);

CREATE TABLE posts_log (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    kind          TEXT NOT NULL,
    payload_json  TEXT NOT NULL,
    tg_message_id INTEGER,
    posted_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
