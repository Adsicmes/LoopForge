-- Codex ChatGPT subscription credentials and platform switches.
-- Run manually once per deployment. AutoMigrate also creates these objects
-- for development and tests; this script keeps production DDL explicit.

ALTER TABLE user_configs ADD COLUMN codex_auth_mode VARCHAR(32) NULL;

CREATE TABLE IF NOT EXISTS user_codex_subscriptions (
  user_id BIGINT NOT NULL,
  auth_json_ciphertext MEDIUMTEXT NOT NULL,
  generation BIGINT NOT NULL DEFAULT 1,
  account_id VARCHAR(128) NULL,
  email VARCHAR(255) NULL,
  plan_type VARCHAR(64) NULL,
  connected_at DATETIME(6) NOT NULL,
  last_refresh_at DATETIME(6) NULL,
  needs_reauth TINYINT(1) NOT NULL DEFAULT 0,
  last_error TEXT NULL,
  run_lease_owner VARCHAR(64) NULL,
  run_lease_until DATETIME(6) NULL,
  refresh_lease_owner VARCHAR(64) NULL,
  refresh_lease_until DATETIME(6) NULL,
  updated_at DATETIME(6) NULL,
  PRIMARY KEY (user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS platform_settings (
  name VARCHAR(64) NOT NULL,
  value TEXT NOT NULL,
  updated_at DATETIME(6) NULL,
  PRIMARY KEY (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
