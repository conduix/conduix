-- Revert soft-delete unique constraint fix

-- 1. Drop generated column indexes and columns from projects
DROP INDEX `idx_projects_unique_name` ON projects;
DROP INDEX `idx_projects_unique_alias` ON projects;
ALTER TABLE projects DROP COLUMN `unique_name`;
ALTER TABLE projects DROP COLUMN `unique_alias`;

-- 2. Restore original unique constraints on projects
CREATE UNIQUE INDEX `name` ON projects (`name`);
CREATE UNIQUE INDEX `idx_projects_name` ON projects (`name`);
CREATE UNIQUE INDEX `idx_projects_alias` ON projects (`alias`);

-- 3. Drop generated column indexes and columns from data_types
DROP INDEX `idx_data_types_unique_name` ON data_types;
ALTER TABLE data_types DROP COLUMN `unique_name`;

-- 4. Restore original unique constraints on data_types
CREATE UNIQUE INDEX `idx_data_types_name` ON data_types (`name`);
CREATE UNIQUE INDEX `idx_datatype_project_name` ON data_types (`project_id`, `name`);
