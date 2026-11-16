-- Yunxiao personal access token and optional Yunxiao user id.
-- Run manually once per deployment. AutoMigrate also adds these columns for
-- development and tests; this script keeps production DDL explicit.

ALTER TABLE user_configs ADD COLUMN yunxiao_token TEXT NULL;
ALTER TABLE user_configs ADD COLUMN yunxiao_user_id VARCHAR(64) NULL;
