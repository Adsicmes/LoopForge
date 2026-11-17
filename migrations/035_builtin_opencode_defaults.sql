-- Manual, idempotent MySQL migration for a database created before OpenCode
-- became the platform's first executor.
--
-- 1) Built-in templates and built-in roles stored in generic_templates switch
--    their role executors from CodeBuddy to OpenCode. Only the literal
--    '"executorId": "codebuddy"' value is rewritten; roles an operator already
--    changed are left alone.
-- 2) The generic-backend Dockerfile gains the pinned OpenCode CLI. The platform
--    image already bundles it; a custom Dockerfile needs the same package.
--
-- User-created agents, SOPs, workflow snapshots, preview contracts, bots and
-- scheduled tasks are intentionally not rewritten.
SET NAMES utf8mb4;

UPDATE generic_templates
SET payload = REPLACE(payload, '"executorId": "codebuddy"', '"executorId": "opencode"')
WHERE payload LIKE '%"executorId": "codebuddy"%';

-- The built-in baseline role lives in agents, not inside a generic template.
UPDATE agents
SET payload = REPLACE(payload, '"executorId": "codebuddy"', '"executorId": "opencode"')
WHERE id = 'baseline-align-resolver'
  AND payload LIKE '%"executorId": "codebuddy"%';

UPDATE dockerfiles
SET payload = CAST(
  JSON_SET(
    payload,
    '$.content',
    REPLACE(
      JSON_UNQUOTE(JSON_EXTRACT(payload, '$.content')),
      '@tencent-ai/codebuddy-code',
      '@tencent-ai/codebuddy-code @opencode/cli@2.3.0'
    )
  ) AS JSON
)
WHERE id = 'generic-backend'
  AND JSON_UNQUOTE(JSON_EXTRACT(payload, '$.content')) LIKE '%@tencent-ai/codebuddy-code%'
  AND JSON_UNQUOTE(JSON_EXTRACT(payload, '$.content')) NOT LIKE '%@opencode/cli%';
