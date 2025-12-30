-- 000001_init_schema.down.sql
-- Rollback initial schema

DROP TABLE IF EXISTS provisioning_results;
DROP TABLE IF EXISTS provisioning_requests;
DROP TABLE IF EXISTS data_provider_stats;
DROP TABLE IF EXISTS pipeline_hourly_stats;
DROP TABLE IF EXISTS pipeline_execution_stats;
DROP TABLE IF EXISTS connections;
DROP TABLE IF EXISTS delete_strategy_presets;
DROP TABLE IF EXISTS data_type_preworks;
DROP TABLE IF EXISTS data_types;
DROP TABLE IF EXISTS resource_permissions;
DROP TABLE IF EXISTS pipeline_group_executions;
DROP TABLE IF EXISTS audit_logs;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS agents;
DROP TABLE IF EXISTS schedules;
DROP TABLE IF EXISTS pipeline_runs;
DROP TABLE IF EXISTS pipelines;
DROP TABLE IF EXISTS pipeline_groups;
DROP TABLE IF EXISTS data_providers;
DROP TABLE IF EXISTS projects;
DROP TABLE IF EXISTS users;
