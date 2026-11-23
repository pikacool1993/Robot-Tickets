CREATE TABLE IF NOT EXISTS agent_terminal_tasks (
    agent_id CHAR(36) NOT NULL,
    client_number VARCHAR(128) NOT NULL,
    client_instance_id CHAR(36) NOT NULL,
    task_id BIGINT NOT NULL,
    authorization_status VARCHAR(32) NOT NULL,
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (agent_id, client_instance_id, task_id),
    KEY idx_terminal_task_status (agent_id, client_instance_id, authorization_status),
    CONSTRAINT fk_terminal_tasks_agent FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS agent_task_sync_state (
    agent_id CHAR(36) NOT NULL,
    client_number VARCHAR(128) NOT NULL,
    client_instance_id CHAR(36) NOT NULL,
    revision BIGINT NOT NULL DEFAULT 0,
    tasks_synced_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (agent_id, client_instance_id),
    CONSTRAINT fk_task_sync_agent FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS agent_orders (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    agent_id CHAR(36) NOT NULL,
    agent_name VARCHAR(200) NOT NULL,
    client_number VARCHAR(128) NOT NULL,
    client_instance_id CHAR(36) NOT NULL,
    task_id BIGINT NOT NULL,
    order_id VARCHAR(128) NOT NULL,
    match_id VARCHAR(128) NOT NULL,
    match_name VARCHAR(255) NOT NULL,
    ticket_count INT NOT NULL,
    tiers JSON NOT NULL,
    seats JSON NOT NULL,
    created_at DATETIME(6) NOT NULL,
    payment_deadline DATETIME(6) NULL,
    detail_status VARCHAR(16) NOT NULL,
    simulated BOOLEAN NOT NULL DEFAULT FALSE,
    viewer_ciphertext MEDIUMBLOB NOT NULL,
    viewer_nonce BINARY(12) NOT NULL,
    viewer_key_version VARCHAR(32) NOT NULL,
    received_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    UNIQUE KEY uq_agent_order_terminal (client_instance_id, order_id),
    KEY idx_agent_orders_owner_time (agent_id, created_at, id),
    CONSTRAINT fk_agent_orders_agent FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE
) ENGINE=InnoDB;
