-- Manual, idempotent MySQL migration for installations that ran the earlier
-- OpenCode defaults migration before npm 12 began blocking lifecycle scripts.
-- The pinned OpenCode package is installed, but its postinstall doesn't run, so
-- `opencode` is a stub that exits 1. Add the explicit postinstall (and a
-- version check) to the built-in generic-backend Dockerfile.
--
-- New installations get the same lines from migrations/seed/001_public_catalog.sql;
-- this script upgrades the stored row produced by 035.
SET NAMES utf8mb4;

UPDATE dockerfiles
SET payload = CAST(
  JSON_SET(
    payload,
    '$.content',
    REPLACE(
      JSON_UNQUOTE(JSON_EXTRACT(payload, '$.content')),
      '@opencode/cli@2.3.0',
      CONCAT('@opencode/cli@2.3.0', CHAR(10), 'RUN node /usr/local/lib/node_modules/@opencode/cli/postinstall.mjs && opencode --version')
    )
  ) AS JSON
)
WHERE id = 'generic-backend'
  AND JSON_UNQUOTE(JSON_EXTRACT(payload, '$.content')) LIKE '%@opencode/cli@2.3.0%'
  AND JSON_UNQUOTE(JSON_EXTRACT(payload, '$.content')) NOT LIKE '%postinstall.mjs%';
