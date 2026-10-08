CREATE TABLE IF NOT EXISTS chats (
    jid         TEXT PRIMARY KEY,
    name        TEXT NOT NULL DEFAULT '',
    is_group    INTEGER NOT NULL DEFAULT 0,
    archived    INTEGER NOT NULL DEFAULT 0,
    muted       INTEGER NOT NULL DEFAULT 0,
    unread      INTEGER NOT NULL DEFAULT 0,
    last_ts     INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS chats_last_ts ON chats (archived, last_ts DESC);

CREATE TABLE IF NOT EXISTS messages (
    chat_jid    TEXT NOT NULL,
    id          TEXT NOT NULL,
    sender_jid  TEXT NOT NULL DEFAULT '',
    sender_name TEXT NOT NULL DEFAULT '',
    from_me     INTEGER NOT NULL DEFAULT 0,
    ts          INTEGER NOT NULL,
    kind        TEXT NOT NULL DEFAULT 'text',
    text        TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL DEFAULT 'sent',
    seen        INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (chat_jid, id)
);
CREATE INDEX IF NOT EXISTS messages_chat_ts ON messages (chat_jid, ts, id);

CREATE TABLE IF NOT EXISTS sent_client_ids (
    client_id   TEXT PRIMARY KEY,
    chat_jid    TEXT NOT NULL,
    message_id  TEXT NOT NULL,
    ts          INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS replies (
    pos         INTEGER PRIMARY KEY,
    text        TEXT NOT NULL
);

-- Downloadable media for messages that can be shown as an image on the watch.
CREATE TABLE IF NOT EXISTS media (
    chat_jid    TEXT NOT NULL,
    id          TEXT NOT NULL,
    kind        TEXT NOT NULL,          -- image, video, sticker
    proto       BLOB,                   -- marshalled waE2E media message, for downloading
    thumbnail   BLOB,                   -- embedded JPEG/PNG thumbnail, used as a fallback
    PRIMARY KEY (chat_jid, id)
);
