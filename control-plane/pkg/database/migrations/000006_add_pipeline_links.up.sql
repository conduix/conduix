-- Pipeline Links: Parent-Child Pipeline Connection (Kafka-based)
-- Note: kafka_brokers is NOT stored here - fetched dynamically from environment/config
CREATE TABLE IF NOT EXISTS pipeline_links (
    id VARCHAR(36) PRIMARY KEY,
    workflow_id VARCHAR(36) NOT NULL,
    parent_pipeline_id VARCHAR(36) NOT NULL,
    child_pipeline_id VARCHAR(36) NOT NULL,
    kafka_topic VARCHAR(255) NOT NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'active',
    metadata TEXT,
    created_by VARCHAR(36),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    INDEX idx_workflow_id (workflow_id),
    UNIQUE INDEX idx_pipeline_link (parent_pipeline_id, child_pipeline_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
