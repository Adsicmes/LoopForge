-- Administrator-confirmed exact SSH host keys; never automatically imported.
CREATE TABLE IF NOT EXISTS ssh_trusted_hosts (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  host VARCHAR(253) NOT NULL,
  port BIGINT NOT NULL,
  marker VARCHAR(32) NOT NULL,
  fingerprint VARCHAR(64) NOT NULL,
  key_digest VARCHAR(64) NOT NULL,
  key_type VARCHAR(128) NOT NULL,
  public_key TEXT NOT NULL,
  UNIQUE KEY idx_ssh_host_key (host, port, marker, key_digest)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
