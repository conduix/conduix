-- Revert resource_permissions
UPDATE resource_permissions SET resource_type = 'group' WHERE resource_type = 'workflow';

-- Revert indexes on pipeline_hourly_stats
ALTER TABLE pipeline_hourly_stats
    DROP INDEX IF EXISTS idx_pipeline_hourly_stats_workflow_id,
    ADD INDEX idx_pipeline_hourly_stats_group_id (workflow_id);

-- Revert indexes on pipeline_execution_stats
ALTER TABLE pipeline_execution_stats
    DROP INDEX IF EXISTS idx_pipeline_execution_stats_workflow_id,
    ADD INDEX idx_pipeline_execution_stats_group_id (workflow_id);

-- Revert indexes on workflow_executions
ALTER TABLE workflow_executions
    DROP INDEX IF EXISTS idx_workflow_executions_workflow_id,
    ADD INDEX idx_pipeline_group_executions_group_id (workflow_id);

-- Revert column names in data_provider_stats
ALTER TABLE data_provider_stats
    CHANGE COLUMN total_workflows total_groups INT DEFAULT 0,
    CHANGE COLUMN realtime_workflows realtime_groups INT DEFAULT 0,
    CHANGE COLUMN batch_workflows batch_groups INT DEFAULT 0;

-- Revert column names in pipeline_hourly_stats
ALTER TABLE pipeline_hourly_stats
    CHANGE COLUMN workflow_id group_id VARCHAR(36) NOT NULL;

-- Revert column names in pipeline_execution_stats
ALTER TABLE pipeline_execution_stats
    CHANGE COLUMN workflow_id group_id VARCHAR(36) NOT NULL;

-- Revert column names in workflow_executions
ALTER TABLE workflow_executions
    CHANGE COLUMN workflow_id group_id VARCHAR(36) NOT NULL;

-- Rename workflow_executions back to pipeline_group_executions
RENAME TABLE workflow_executions TO pipeline_group_executions;

-- Rename workflows back to pipeline_groups
RENAME TABLE workflows TO pipeline_groups;
