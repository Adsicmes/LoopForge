-- Manual, idempotent MySQL migration. The platform also creates this table
-- on startup; the script documents it for operators who manage DDL by hand.
-- Holds at most one row (id = 1): the OpenCode API Key, Base URL, protocol and
-- models (a JSON array, default first) a platform administrator shares with
-- users who have no key of their own.
CREATE TABLE IF NOT EXISTS platform_opencode_keys (
  id BIGINT NOT NULL PRIMARY KEY,
  api_key TEXT NOT NULL,
  base_url VARCHAR(2048) NOT NULL DEFAULT '',
  protocol VARCHAR(32) NOT NULL DEFAULT 'chat',
  models TEXT NOT NULL,
  updated_by BIGINT NOT NULL DEFAULT 0,
  updated_at DATETIME(3) NULL
);
