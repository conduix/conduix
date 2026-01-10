-- Source Checkpoints: Realtime 파이프라인 소스의 오프셋 추적
CREATE TABLE IF NOT EXISTS source_checkpoints (
    id VARCHAR(36) PRIMARY KEY,
    workflow_id VARCHAR(36) NOT NULL,
    pipeline_id VARCHAR(36) NOT NULL,
    pipeline_name VARCHAR(255),
    source_type VARCHAR(50) NOT NULL COMMENT 'kubernetes, kafka, cdc, sql_event',
    partition_key VARCHAR(255) NOT NULL COMMENT 'ns/pod/container for k8s, topic/partition for kafka',
    offset_value VARCHAR(255) NOT NULL COMMENT 'timestamp or offset number',
    offset_type VARCHAR(50) NOT NULL COMMENT 'timestamp, numeric',
    record_count BIGINT DEFAULT 0 COMMENT 'cumulative processed record count',
    metadata TEXT COMMENT 'JSON - additional info',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    INDEX idx_checkpoint_workflow (workflow_id),
    INDEX idx_checkpoint_pipeline (pipeline_id),
    UNIQUE INDEX idx_checkpoint_unique (workflow_id, pipeline_id, partition_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
