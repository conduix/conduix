-- Rename pipeline_groups to workflows
RENAME TABLE pipeline_groups TO workflows;

-- Rename pipeline_group_executions to workflow_executions
RENAME TABLE pipeline_group_executions TO workflow_executions;

-- Update column names in workflow_executions
ALTER TABLE workflow_executions
    CHANGE COLUMN group_id workflow_id VARCHAR(36) NOT NULL;

-- Update column names in pipeline_execution_stats
ALTER TABLE pipeline_execution_stats
    CHANGE COLUMN group_id workflow_id VARCHAR(36) NOT NULL;

-- Update column names in pipeline_hourly_stats
ALTER TABLE pipeline_hourly_stats
    CHANGE COLUMN group_id workflow_id VARCHAR(36) NOT NULL;

-- Update column names in data_provider_stats
ALTER TABLE data_provider_stats
    CHANGE COLUMN total_groups total_workflows INT DEFAULT 0,
    CHANGE COLUMN realtime_groups realtime_workflows INT DEFAULT 0,
    CHANGE COLUMN batch_groups batch_workflows INT DEFAULT 0;

-- Rename indexes on workflow_executions
ALTER TABLE workflow_executions
    DROP INDEX IF EXISTS idx_pipeline_group_executions_group_id,
    ADD INDEX idx_workflow_executions_workflow_id (workflow_id);

-- Rename indexes on pipeline_execution_stats
ALTER TABLE pipeline_execution_stats
    DROP INDEX IF EXISTS idx_pipeline_execution_stats_group_id,
    ADD INDEX idx_pipeline_execution_stats_workflow_id (workflow_id);

-- Rename indexes on pipeline_hourly_stats
ALTER TABLE pipeline_hourly_stats
    DROP INDEX IF EXISTS idx_pipeline_hourly_stats_group_id,
    ADD INDEX idx_pipeline_hourly_stats_workflow_id (workflow_id);

-- Update resource_permissions where resource_type = 'group' to 'workflow'
UPDATE resource_permissions SET resource_type = 'workflow' WHERE resource_type = 'group';
