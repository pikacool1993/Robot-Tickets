CREATE TABLE IF NOT EXISTS admin_users (
    id CHAR(36) PRIMARY KEY,
    username VARCHAR(64) NOT NULL UNIQUE,
    password_hash VARCHAR(255) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'enabled',
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS admin_sessions (
    id CHAR(36) PRIMARY KEY,
    admin_user_id CHAR(36) NOT NULL,
    token_hash BINARY(32) NOT NULL UNIQUE,
    expires_at DATETIME(6) NOT NULL,
    revoked_at DATETIME(6) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    login_ip VARCHAR(45) NULL,
    CONSTRAINT fk_admin_sessions_admin FOREIGN KEY (admin_user_id) REFERENCES admin_users(id) ON DELETE CASCADE
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS agents (
    id CHAR(36) PRIMARY KEY,
    name VARCHAR(200) NOT NULL,
    username VARCHAR(64) NOT NULL UNIQUE,
    password_ciphertext VARBINARY(128) NOT NULL,
    password_nonce BINARY(12) NOT NULL,
    password_key_version VARCHAR(32) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'enabled',
    max_devices INT NOT NULL,
    last_login_at DATETIME(6) NULL,
    last_login_ip VARCHAR(45) NULL,
    created_by CHAR(36) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    CONSTRAINT fk_agents_creator FOREIGN KEY (created_by) REFERENCES admin_users(id) ON DELETE SET NULL
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS client_registrations (
    client_number VARCHAR(128) PRIMARY KEY,
    agent_id CHAR(36) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    CONSTRAINT fk_client_registrations_agent FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS agent_sessions (
    id CHAR(36) PRIMARY KEY,
    agent_id CHAR(36) NOT NULL,
    client_instance_id CHAR(36) NOT NULL,
    client_number VARCHAR(128) NOT NULL,
    token_hash BINARY(32) NOT NULL UNIQUE,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    expires_at DATETIME(6) NOT NULL,
    revoked_at DATETIME(6) NULL,
    login_ip VARCHAR(45) NOT NULL,
    user_agent VARCHAR(512) NULL,
    active_instance_id CHAR(36) GENERATED ALWAYS AS (IF(revoked_at IS NULL, client_instance_id, NULL)) STORED,
    UNIQUE KEY uq_agent_active_instance (agent_id, active_instance_id),
    KEY idx_agent_sessions_expiry (agent_id, expires_at),
    CONSTRAINT fk_agent_sessions_agent FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE,
    CONSTRAINT fk_agent_sessions_client FOREIGN KEY (client_number) REFERENCES client_registrations(client_number)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS audit_events (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    actor_type VARCHAR(16) NOT NULL,
    actor_admin_id CHAR(36) NULL,
    agent_id CHAR(36) NULL,
    event_type VARCHAR(64) NOT NULL,
    occurred_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    source_ip VARCHAR(45) NULL,
    client_number VARCHAR(128) NULL,
    client_instance_id CHAR(36) NULL,
    result VARCHAR(16) NOT NULL,
    details JSON NOT NULL,
    KEY idx_audit_agent_time (agent_id, occurred_at),
    CONSTRAINT fk_audit_admin FOREIGN KEY (actor_admin_id) REFERENCES admin_users(id) ON DELETE SET NULL,
    CONSTRAINT fk_audit_agent FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE
) ENGINE=InnoDB;
